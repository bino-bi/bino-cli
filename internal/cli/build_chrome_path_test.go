package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeCachedChrome creates a fake chrome-headless-shell download under
// $HOME/.bino/chrome-headless-shell/<version>/ and returns its exec path.
func writeCachedChrome(t *testing.T, home, version string) string {
	t.Helper()

	name := "chrome-headless-shell"
	if runtime.GOOS == "windows" {
		name = "chrome-headless-shell.exe"
	}

	dir := filepath.Join(home, ".bino", "chrome-headless-shell", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	execPath := filepath.Join(dir, name)
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // G306: test fixture must be executable
		t.Fatalf("write fake chrome: %v", err)
	}

	return execPath
}

func TestResolveChromePath(t *testing.T) {
	tests := []struct {
		name string
		// setup returns the configured --chrome-path value and the expected result.
		setup func(t *testing.T, home string) (configured, want string)
	}{
		{
			name: "configured path wins over everything",
			setup: func(t *testing.T, home string) (string, string) {
				t.Helper()
				writeCachedChrome(t, home, "120.0.0.0")
				t.Setenv("CHROME_PATH", writeCachedChrome(t, home, "121.0.0.0"))
				return "/explicit/chrome", "/explicit/chrome"
			},
		},
		{
			name: "CHROME_PATH wins over the cached version",
			setup: func(t *testing.T, home string) (string, string) {
				t.Helper()
				writeCachedChrome(t, home, "120.0.0.0")
				envPath := filepath.Join(t.TempDir(), "system-chromium")
				if err := os.WriteFile(envPath, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // G306: test fixture must be executable
					t.Fatalf("write fake chrome: %v", err)
				}
				t.Setenv("CHROME_PATH", envPath)
				return "", envPath
			},
		},
		{
			name: "falls back to the cached version",
			setup: func(t *testing.T, home string) (string, string) {
				t.Helper()
				return "", writeCachedChrome(t, home, "120.0.0.0")
			},
		},
		{
			name: "empty when nothing is available",
			setup: func(t *testing.T, _ string) (string, string) {
				t.Helper()
				return "", ""
			},
		},
		{
			name: "empty when CHROME_PATH points at a missing file",
			setup: func(t *testing.T, _ string) (string, string) {
				t.Helper()
				t.Setenv("CHROME_PATH", filepath.Join(t.TempDir(), "does-not-exist"))
				return "", ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
			t.Setenv("CHROME_PATH", "")

			configured, want := tt.setup(t, home)

			if got := resolveChromePath(configured); got != want {
				t.Errorf("resolveChromePath(%q) = %q, want %q", configured, got, want)
			}
		})
	}
}
