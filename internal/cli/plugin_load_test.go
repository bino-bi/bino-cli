package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

const (
	missingPluginToml        = "report-id = \"t\"\n\n[plugins.nonexistent-xyzzy]\n"
	missingPluginDisableToml = "report-id = \"t\"\n\n[lint]\ndisable = [\"plugin-load-failed\"]\n\n[plugins.nonexistent-xyzzy]\n"
)

func writeMissingPluginProject(t *testing.T, toml string) string {
	t.Helper()
	return writeLintProject(t, toml, map[string]string{
		"report.yaml": lintCfgReportA4,
		"page.yaml":   lintCfgPage,
	})
}

func assertPluginLoadError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error for a declared plugin that does not load")
	}
	if code := exitCode(errorKind(err)); code != 1 {
		t.Errorf("exit code = %d, want 1 (%v)", code, err)
	}
	if !strings.Contains(err.Error(), `plugin declared in bino.toml did not load: loading plugin "nonexistent-xyzzy"`) {
		t.Errorf("error does not name the plugin: %v", err)
	}
}

func findPluginLoadDiag(t *testing.T, diags []LSPDiagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Code != "plugin-load-failed" {
			continue
		}
		if filepath.Base(d.File) != "bino.toml" || d.Line != 1 || d.Column != 1 || d.Severity != "error" {
			t.Errorf("plugin-load-failed diagnostic has the wrong shape: %+v", d)
		}
		if !strings.Contains(d.Message, `"nonexistent-xyzzy"`) || !strings.HasSuffix(d.Message, "Reload the editor window after fixing it (in VS Code: Developer: Reload Window).") {
			t.Errorf("plugin-load-failed message = %q", d.Message)
		}
		return
	}
	t.Fatalf("no plugin-load-failed diagnostic in %+v", diags)
}

func TestLoadProjectPlugins(t *testing.T) {
	ctx := context.Background()
	mgr, err := loadProjectPlugins(ctx, logx.Nop(), &pathutil.ProjectConfig{}, t.TempDir())
	if mgr != nil || err != nil {
		t.Fatalf("no plugins declared: want (nil, nil), got (%v, %v)", mgr, err)
	}

	cfg := &pathutil.ProjectConfig{Plugins: map[string]pathutil.PluginDeclaration{"nonexistent-xyzzy": {}}}
	mgr, err = loadProjectPlugins(ctx, logx.Nop(), cfg, "/tmp/no-such-project")
	if mgr != nil {
		t.Error("want no manager on failure")
	}
	assertPluginLoadError(t, err)
}

func TestLintCommand_MissingPluginFails(t *testing.T) {
	dir := writeMissingPluginProject(t, missingPluginToml)
	out, err := runLintCommand(t, dir)
	assertPluginLoadError(t, err)
	if strings.Contains(out, "Validated") {
		t.Errorf("lint validated manifests without the plugin:\n%s", out)
	}
}

func TestGraphCommand_MissingPlugin(t *testing.T) {
	run := func(dir string) error {
		cmd := newGraphCommand()
		cmd.SetArgs([]string{"--work-dir", dir})
		cmd.SetContext(context.Background())
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		return cmd.Execute()
	}
	assertPluginLoadError(t, run(writeMissingPluginProject(t, missingPluginToml)))
	if err := run(writeMissingPluginProject(t, "report-id = \"t\"\n")); err != nil {
		t.Errorf("graph without [plugins]: %v", err)
	}
}

func TestSchemaCommand_MissingPluginFails(t *testing.T) {
	t.Chdir(writeMissingPluginProject(t, missingPluginToml))
	cmd := newSchemaCommand()
	cmd.SetArgs(nil)
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	assertPluginLoadError(t, cmd.Execute())
}

func TestLoadStandaloneState_MissingPluginDiagnostic(t *testing.T) {
	for _, toml := range []string{missingPluginToml, missingPluginDisableToml} {
		dir := writeMissingPluginProject(t, toml)
		ctx := context.Background()
		managed, reg, cleanup, err := loadStandaloneState(ctx, logx.Nop(), dir)
		if err != nil {
			t.Fatalf("loadStandaloneState: %v", err)
		}
		if reg != nil {
			t.Error("want no plugin registry after a failed load")
		}
		if err := managed.State.Refresh(ctx); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		findPluginLoadDiag(t, managed.State.Diagnostics())
		cleanup()
	}
}

func TestValidateDirectory_MissingPluginDiagnostic(t *testing.T) {
	for _, toml := range []string{missingPluginToml, missingPluginDisableToml} {
		findPluginLoadDiag(t, validateDirectory(context.Background(), writeMissingPluginProject(t, toml), false))
	}
	for _, d := range validateDirectory(context.Background(), writeMissingPluginProject(t, "report-id = \"t\"\n"), false) {
		if d.Code == "plugin-load-failed" {
			t.Errorf("project without [plugins] got %+v", d)
		}
	}
}

// fakeEngineHome points HOME at a temp dir holding a stand-in engine, so
// initCommandEnv runs without a download. The DuckDB extension cache is linked
// from the real HOME to avoid fetching extensions again.
func fakeEngineHome(t *testing.T) {
	t.Helper()
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	engineDir := filepath.Join(home, ".bino", engine.CacheSubdir, "v1.0.0-alpha.19")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engineDir, engine.EntryPoint), []byte("// stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(realHome, ".bino", "cache")); err == nil {
		if err := os.Symlink(filepath.Join(realHome, ".bino", "cache"), filepath.Join(home, ".bino", "cache")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDaemon_MissingPluginKeepsRunning(t *testing.T) {
	fakeEngineHome(t)
	dir := writeMissingPluginProject(t, missingPluginToml)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := newDaemonCommand()
	cmd.SetArgs([]string{"--work-dir", dir, "--mcp=false"})
	cmd.SetContext(ctx)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	var port int
	for deadline := time.Now().Add(30 * time.Second); port == 0; {
		select {
		case err := <-done:
			t.Fatalf("daemon exited early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not write its port file")
		}
		if pf, _ := daemon.ReadPortFile(dir); pf != nil { //nolint:errcheck // retried until the deadline
			port = pf.Port
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/validate", port), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result daemon.ValidateResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	findPluginLoadDiag(t, result.Diagnostics)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestInitCommandEnv_MissingPlugin(t *testing.T) {
	fakeEngineHome(t)
	dir := writeMissingPluginProject(t, missingPluginToml)
	ctx := context.Background()

	_, err := initCommandEnv(ctx, newBuildCommand(), dir, "build", logx.Nop())
	assertPluginLoadError(t, err)

	env, err := initEditorCommandEnv(ctx, newDaemonCommand(), dir, "preview", logx.Nop())
	if err != nil {
		t.Fatalf("initEditorCommandEnv: %v", err)
	}
	assertPluginLoadError(t, env.PluginLoadErr)
	if env.PluginManager != nil || env.PluginRegistry != nil {
		t.Error("want no plugins after a failed load")
	}
}
