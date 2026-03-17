package hooks

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// testLogger collects log messages for assertions.
type testLogger struct {
	messages []string
}

func (l *testLogger) Infof(format string, args ...any)    { l.log("info", format, args...) }
func (l *testLogger) Successf(format string, args ...any) { l.log("success", format, args...) }
func (l *testLogger) Warnf(format string, args ...any)    { l.log("warn", format, args...) }
func (l *testLogger) Errorf(format string, args ...any)   { l.log("error", format, args...) }
func (l *testLogger) Debugf(format string, args ...any)   { l.log("debug", format, args...) }
func (l *testLogger) Channel(_ string) logx.Logger        { return l }

func (l *testLogger) log(level, format string, args ...any) {
	msg := level + ": " + format
	if len(args) > 0 {
		msg = level + ": " + strings.ReplaceAll(format, "%s", "X")
	}
	l.messages = append(l.messages, msg)
}

func (l *testLogger) contains(substr string) bool {
	for _, m := range l.messages {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses sh -c commands; skipping on Windows")
	}
}

func TestRun(t *testing.T) {
	skipOnWindows(t)

	tests := []struct {
		name       string
		hooks      pathutil.HooksConfig
		checkpoint string
		wantErr    bool
		errSubstr  string
	}{
		{
			name:       "success",
			hooks:      pathutil.HooksConfig{"pre-build": {"echo hello"}},
			checkpoint: "pre-build",
			wantErr:    false,
		},
		{
			name:       "no commands for checkpoint",
			hooks:      pathutil.HooksConfig{"pre-build": {"echo hello"}},
			checkpoint: "post-build",
			wantErr:    false,
		},
		{
			name:       "empty hooks",
			hooks:      nil,
			checkpoint: "pre-build",
			wantErr:    false,
		},
		{
			name:       "failure propagation",
			hooks:      pathutil.HooksConfig{"pre-build": {"exit 1"}},
			checkpoint: "pre-build",
			wantErr:    true,
			errSubstr:  "exit code 1",
		},
		{
			name:       "skip exit code 78",
			hooks:      pathutil.HooksConfig{"pre-build": {"exit 78"}},
			checkpoint: "pre-build",
			wantErr:    false,
		},
		{
			name:       "multiple commands sequential success",
			hooks:      pathutil.HooksConfig{"pre-build": {"echo one", "echo two", "echo three"}},
			checkpoint: "pre-build",
			wantErr:    false,
		},
		{
			name:       "multiple commands stops on failure",
			hooks:      pathutil.HooksConfig{"pre-build": {"echo one", "exit 1", "echo three"}},
			checkpoint: "pre-build",
			wantErr:    true,
			errSubstr:  "exit code 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testLogger{}
			cfg := &Config{Hooks: tt.hooks}
			runner := NewRunner(cfg, logger, t.TempDir())

			err := runner.Run(context.Background(), tt.checkpoint, HookEnv{
				Mode:    "build",
				Workdir: t.TempDir(),
			})

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.errSubstr != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
			}
		})
	}
}

func TestRunNilRunner(t *testing.T) {
	var runner *Runner
	err := runner.Run(context.Background(), "pre-build", HookEnv{})
	if err != nil {
		t.Fatalf("nil runner should return nil error, got: %v", err)
	}
}

func TestRunNilConfig(t *testing.T) {
	runner := NewRunner(nil, nil, ".")
	err := runner.Run(context.Background(), "pre-build", HookEnv{})
	if err != nil {
		t.Fatalf("nil config runner should return nil error, got: %v", err)
	}
}

func TestRunContextCancellation(t *testing.T) {
	skipOnWindows(t)

	cfg := &Config{Hooks: pathutil.HooksConfig{
		"pre-build": {"sleep 10"},
	}}
	logger := &testLogger{}
	runner := NewRunner(cfg, logger, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := runner.Run(ctx, "pre-build", HookEnv{Mode: "build"})
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}

func TestRunEnvironmentVariables(t *testing.T) {
	skipOnWindows(t)

	// Use a command that writes env vars to a file we can check
	dir := t.TempDir()
	cfg := &Config{Hooks: pathutil.HooksConfig{
		"pre-build": {
			`test "$BINO_MODE" = "build" && test "$BINO_HOOK" = "pre-build" && test "$BINO_VERBOSE" = "1"`,
		},
	}}
	logger := &testLogger{}
	runner := NewRunner(cfg, logger, dir)

	err := runner.Run(context.Background(), "pre-build", HookEnv{
		Mode:     "build",
		Workdir:  dir,
		ReportID: "test-id",
		Verbose:  true,
	})
	if err != nil {
		t.Fatalf("env var check failed: %v", err)
	}
}

func TestRunArtefactEnvVars(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	cfg := &Config{Hooks: pathutil.HooksConfig{
		"pre-datasource": {
			`test "$BINO_ARTIFACT_NAME" = "monthly" && test "$BINO_ARTIFACT_KIND" = "report"`,
		},
	}}
	logger := &testLogger{}
	runner := NewRunner(cfg, logger, dir)

	err := runner.Run(context.Background(), "pre-datasource", HookEnv{
		Mode:         "build",
		Workdir:      dir,
		ArtefactName: "monthly",
		ArtefactKind: "report",
	})
	if err != nil {
		t.Fatalf("artifact env var check failed: %v", err)
	}
}

func TestResolve(t *testing.T) {
	t.Run("per-command overrides shared", func(t *testing.T) {
		shared := pathutil.HooksConfig{
			"pre-datasource": {"shared-cmd"},
			"pre-build":      {"shared-build"},
		}
		perCommand := pathutil.HooksConfig{
			"pre-datasource": {"override-cmd"},
		}

		cfg := Resolve(shared, perCommand, nil)

		// pre-datasource should be overridden
		cmds := cfg.Hooks["pre-datasource"]
		if len(cmds) != 1 || cmds[0] != "override-cmd" {
			t.Fatalf("expected override, got %v", cmds)
		}

		// pre-build should remain from shared
		cmds = cfg.Hooks["pre-build"]
		if len(cmds) != 1 || cmds[0] != "shared-build" {
			t.Fatalf("expected shared pre-build, got %v", cmds)
		}
	})

	t.Run("warns on unknown checkpoint", func(t *testing.T) {
		logger := &testLogger{}
		Resolve(pathutil.HooksConfig{"unknown-hook": {"cmd"}}, nil, logger)

		if !logger.contains("unknown hook checkpoint") {
			t.Fatal("expected warning about unknown checkpoint")
		}
	})

	t.Run("nil inputs", func(t *testing.T) {
		cfg := Resolve(nil, nil, nil)
		if len(cfg.Hooks) != 0 {
			t.Fatalf("expected empty hooks, got %v", cfg.Hooks)
		}
	})
}

func TestBuildEnvSlice(t *testing.T) {
	env := HookEnv{
		Mode:         "build",
		Workdir:      "/test",
		ReportID:     "abc",
		Hook:         "pre-build",
		Verbose:      false,
		OutputDir:    "/dist",
		ArtefactName: "report1",
		ArtefactKind: "report",
		Include:      "a,b",
		Exclude:      "c",
		PDFPath:      "/dist/report1.pdf",
	}

	pairs := buildEnvSlice(env)
	pairMap := make(map[string]string)
	for _, p := range pairs {
		parts := strings.SplitN(p, "=", 2)
		pairMap[parts[0]] = parts[1]
	}

	checks := map[string]string{
		"BINO_MODE":          "build",
		"BINO_WORKDIR":       "/test",
		"BINO_REPORT_ID":     "abc",
		"BINO_HOOK":          "pre-build",
		"BINO_VERBOSE":       "0",
		"BINO_OUTPUT_DIR":    "/dist",
		"BINO_ARTIFACT_NAME": "report1",
		"BINO_ARTIFACT_KIND": "report",
		"BINO_INCLUDE":       "a,b",
		"BINO_EXCLUDE":       "c",
		"BINO_PDF_PATH":      "/dist/report1.pdf",
	}

	for key, want := range checks {
		got, ok := pairMap[key]
		if !ok {
			t.Errorf("missing %s", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
