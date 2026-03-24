package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func createExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverBinary_ExplicitPath(t *testing.T) {
	tmp := t.TempDir()
	bin := createExecutable(t, tmp, "my-plugin")

	got, err := discoverBinary("test", bin, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestDiscoverBinary_ExplicitPath_NotFound(t *testing.T) {
	_, err := discoverBinary("test", "/nonexistent/path/plugin", "", nil)
	if err == nil {
		t.Fatal("expected error for non-existent explicit path")
	}
}

func TestDiscoverBinary_ExplicitPath_NotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not have Unix file permissions")
	}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "my-plugin")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := discoverBinary("test", path, "", nil)
	if err == nil {
		t.Fatal("expected error for non-executable file")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected 'not executable' in error, got: %v", err)
	}
}

func TestDiscoverBinary_ProjectLocal(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, ".bino", "plugins")
	createExecutable(t, pluginDir, "bino-plugin-test")

	got, err := discoverBinary("test", "", tmp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, filepath.Join(".bino", "plugins", "bino-plugin-test")) {
		t.Fatalf("expected project-local path, got %q", got)
	}
}

func TestDiscoverBinary_UserGlobal(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, ".bino", "plugins")
	createExecutable(t, pluginDir, "bino-plugin-test")

	fakeHome := func() (string, error) { return tmp, nil }

	// No project root, so it skips project-local.
	got, err := discoverBinary("test", "", "", fakeHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, filepath.Join(".bino", "plugins", "bino-plugin-test")) {
		t.Fatalf("expected user-global path, got %q", got)
	}
}

func TestDiscoverBinary_PATH(t *testing.T) {
	tmp := t.TempDir()
	createExecutable(t, tmp, "bino-plugin-test")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := discoverBinary("test", "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "bino-plugin-test") {
		t.Fatalf("expected PATH discovery, got %q", got)
	}
}

func TestDiscoverBinary_NotFound(t *testing.T) {
	// Use a unique name that won't be on PATH.
	_, err := discoverBinary("nonexistent-xyzzy-42", "", "", nil)
	if err == nil {
		t.Fatal("expected error when binary not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestDiscoverBinary_ProjectLocalBeforeUserGlobal(t *testing.T) {
	// Project-local should take priority over user-global.
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	createExecutable(t, filepath.Join(projectDir, ".bino", "plugins"), "bino-plugin-test")
	createExecutable(t, filepath.Join(homeDir, ".bino", "plugins"), "bino-plugin-test")

	fakeHome := func() (string, error) { return homeDir, nil }

	got, err := discoverBinary("test", "", projectDir, fakeHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, projectDir) {
		t.Fatalf("expected project-local path (prefix %q), got %q", projectDir, got)
	}
}

func TestDiscoverBinary_ExplicitPath_WindowsExeAppend(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	// On Windows, an explicit path without .exe should resolve to the .exe variant.
	tmp := t.TempDir()
	createExecutable(t, tmp, "my-plugin.exe")

	basePath := filepath.Join(tmp, "my-plugin") // no .exe
	got, err := discoverBinary("test", basePath, "", nil)
	if err != nil {
		t.Fatalf("expected Windows to find .exe variant, got error: %v", err)
	}
	if !strings.HasSuffix(got, ".exe") {
		t.Fatalf("expected path ending in .exe, got %q", got)
	}
}

func TestDiscoverBinary_ExplicitPathIsDirectory(t *testing.T) {
	tmp := t.TempDir()
	_, err := discoverBinary("test", tmp, "", nil)
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected 'directory' in error, got: %v", err)
	}
}
