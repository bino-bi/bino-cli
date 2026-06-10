package engine

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createMockVersion creates a mock version directory with the entry point file.
func createMockVersion(t *testing.T, cacheDir, version string) {
	t.Helper()
	versionDir := filepath.Join(cacheDir, version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("Failed to create version dir: %v", err)
	}
	entryPath := filepath.Join(versionDir, EntryPoint)
	if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
		t.Fatalf("Failed to create entry point: %v", err)
	}
}

func TestNewManager(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if mgr.cacheDir == "" {
		t.Error("NewManager() created manager with empty cacheDir")
	}
}

func TestListLocalVersions_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithClient(tmpDir, nil)

	versions, err := mgr.ListLocalVersions()
	if err != nil {
		t.Fatalf("ListLocalVersions() error = %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("ListLocalVersions() = %v, want empty slice", versions)
	}
}

func TestListLocalVersions_WithVersions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock version directories with entry points
	for _, v := range []string{"v1.0.0", "v1.2.3", "v2.0.0"} {
		createMockVersion(t, tmpDir, v)
	}

	// Create an incomplete version (no entry point)
	incompleteDir := filepath.Join(tmpDir, "v0.0.1")
	if err := os.MkdirAll(incompleteDir, 0o755); err != nil {
		t.Fatalf("Failed to create incomplete dir: %v", err)
	}

	mgr := NewManagerWithClient(tmpDir, nil)
	result, err := mgr.ListLocalVersions()
	if err != nil {
		t.Fatalf("ListLocalVersions() error = %v", err)
	}

	// Should have 3 valid versions, sorted newest first
	if len(result) != 3 {
		t.Errorf("ListLocalVersions() returned %d versions, want 3", len(result))
	}

	// Check semver sorting (newest first)
	if len(result) >= 1 && result[0].Version != "v2.0.0" {
		t.Errorf("ListLocalVersions()[0].Version = %q, want v2.0.0", result[0].Version)
	}
	if len(result) >= 2 && result[1].Version != "v1.2.3" {
		t.Errorf("ListLocalVersions()[1].Version = %q, want v1.2.3", result[1].Version)
	}
	if len(result) >= 3 && result[2].Version != "v1.0.0" {
		t.Errorf("ListLocalVersions()[2].Version = %q, want v1.0.0", result[2].Version)
	}
}

func TestLatestLocalVersion_NoVersions(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithClient(tmpDir, nil)

	_, err := mgr.LatestLocalVersion()
	if err == nil {
		t.Error("LatestLocalVersion() expected error for empty cache, got nil")
	}
}

func TestLatestLocalVersion_WithVersions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock version directories
	for _, v := range []string{"v1.0.0", "v2.0.0"} {
		createMockVersion(t, tmpDir, v)
	}

	mgr := NewManagerWithClient(tmpDir, nil)
	info, err := mgr.LatestLocalVersion()
	if err != nil {
		t.Fatalf("LatestLocalVersion() error = %v", err)
	}

	if info.Version != "v2.0.0" {
		t.Errorf("LatestLocalVersion().Version = %q, want v2.0.0", info.Version)
	}
}

func TestResolveVersion_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	// Create one version
	createMockVersion(t, tmpDir, "v1.0.0")

	mgr := NewManagerWithClient(tmpDir, nil)

	// Empty version should resolve to latest
	info, err := mgr.ResolveVersion("")
	if err != nil {
		t.Fatalf("ResolveVersion('') error = %v", err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("ResolveVersion('').Version = %q, want v1.0.0", info.Version)
	}
}

func TestResolveVersion_Specific(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two versions
	for _, v := range []string{"v1.0.0", "v2.0.0"} {
		createMockVersion(t, tmpDir, v)
	}

	mgr := NewManagerWithClient(tmpDir, nil)

	// Resolve specific version
	info, err := mgr.ResolveVersion("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion('v1.0.0') error = %v", err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("ResolveVersion('v1.0.0').Version = %q, want v1.0.0", info.Version)
	}
}

func TestResolveVersion_WithoutPrefix(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a version
	createMockVersion(t, tmpDir, "v1.0.0")

	mgr := NewManagerWithClient(tmpDir, nil)

	// Version without v prefix should be normalized
	info, err := mgr.ResolveVersion("1.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion('1.0.0') error = %v", err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("ResolveVersion('1.0.0').Version = %q, want v1.0.0", info.Version)
	}
}

func TestResolveVersion_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithClient(tmpDir, nil)

	_, err := mgr.ResolveVersion("v9.9.9")
	if err == nil {
		t.Error("ResolveVersion('v9.9.9') expected error for missing version, got nil")
	}
}

func TestResolveVersion_Prerelease(t *testing.T) {
	tmpDir := t.TempDir()

	createMockVersion(t, tmpDir, "v1.0.0-alpha.2")

	mgr := NewManagerWithClient(tmpDir, nil)

	info, err := mgr.ResolveVersion("v1.0.0-alpha.2")
	if err != nil {
		t.Fatalf("ResolveVersion('v1.0.0-alpha.2') error = %v", err)
	}
	if info.Version != "v1.0.0-alpha.2" {
		t.Errorf("ResolveVersion('v1.0.0-alpha.2').Version = %q, want v1.0.0-alpha.2", info.Version)
	}
}

func TestListLocalVersions_WithPrerelease(t *testing.T) {
	tmpDir := t.TempDir()

	for _, v := range []string{"v1.0.0", "v1.0.0-alpha.2", "v1.0.0-beta.1"} {
		createMockVersion(t, tmpDir, v)
	}

	mgr := NewManagerWithClient(tmpDir, nil)
	result, err := mgr.ListLocalVersions()
	if err != nil {
		t.Fatalf("ListLocalVersions() error = %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("ListLocalVersions() returned %d versions, want 3", len(result))
	}

	// v1.0.0 should sort before pre-release versions (semver: release > pre-release)
	if result[0].Version != "v1.0.0" {
		t.Errorf("ListLocalVersions()[0].Version = %q, want v1.0.0", result[0].Version)
	}
}

func TestResolveVersion_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithClient(tmpDir, nil)

	_, err := mgr.ResolveVersion("invalid")
	if err == nil {
		t.Error("ResolveVersion('invalid') expected error for invalid format, got nil")
	}
}

func TestEnsureVersion_AlreadyCached(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a cached version
	createMockVersion(t, tmpDir, "v1.0.0")

	mgr := NewManagerWithClient(tmpDir, nil)

	// Should resolve from cache without downloading
	info, err := mgr.EnsureVersion(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("EnsureVersion('v1.0.0') error = %v", err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("EnsureVersion('v1.0.0').Version = %q, want v1.0.0", info.Version)
	}
}

func TestVersionPattern(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{"v1.0.0", true},
		{"v1.2.3", true},
		{"v10.20.30", true},
		{"v0.0.0", true},
		{"v1.0.0-alpha", true},
		{"v1.0.0-alpha.1", true},
		{"v1.0.0-alpha.2", true},
		{"v1.0.0-beta", true},
		{"v1.0.0-beta.1", true},
		{"v1.0.0-rc.1", true},
		{"v1.0.0-0.3.7", true},
		{"1.0.0", false},  // missing v prefix
		{"v1.0", false},   // missing patch
		{"v1", false},     // missing minor and patch
		{"vX.Y.Z", false}, // not numbers
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			got := versionPattern.MatchString(tc.version)
			if got != tc.valid {
				t.Errorf("versionPattern.MatchString(%q) = %v, want %v", tc.version, got, tc.valid)
			}
		})
	}
}

func TestCacheDir(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cacheDir := mgr.CacheDir()
	if cacheDir == "" {
		t.Error("CacheDir() returned empty string")
	}

	// Should contain the expected path structure
	if !filepath.IsAbs(cacheDir) {
		t.Errorf("CacheDir() = %q, want absolute path", cacheDir)
	}
}

// writeTestZip creates a zip file at path with the given name → content entries.
func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestExtractZip_ValidArchive(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManagerWithClient(tmp, nil)
	zipPath := filepath.Join(tmp, "ok.zip")
	writeTestZip(t, zipPath, map[string]string{
		"bn-template-engine/main.js": "// engine",
	})

	destDir := filepath.Join(tmp, "dest")
	if err := mgr.extractZip(zipPath, destDir); err != nil {
		t.Fatalf("extractZip() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, "main.js"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "// engine" {
		t.Errorf("extracted content = %q, want %q", data, "// engine")
	}
}

func TestExtractZip_RejectsSiblingDirEscape(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManagerWithClient(tmp, nil)
	zipPath := filepath.Join(tmp, "bad.zip")
	// Resolves to a sibling of destDir that shares it as a string prefix.
	writeTestZip(t, zipPath, map[string]string{
		"bn-template-engine/../dest-evil/evil.txt": "pwned",
	})

	destDir := filepath.Join(tmp, "dest")
	err := mgr.extractZip(zipPath, destDir)
	if err == nil {
		t.Fatal("extractZip() should reject path traversal entry")
	}
	if !strings.Contains(err.Error(), "invalid file path in zip") {
		t.Errorf("error = %v, want invalid file path in zip", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "dest-evil", "evil.txt")); !os.IsNotExist(statErr) {
		t.Error("zip entry escaped the destination directory")
	}
}
