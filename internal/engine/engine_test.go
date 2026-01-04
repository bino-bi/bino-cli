package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
	versions := []string{"v1.0.0", "v1.2.3", "v2.0.0"}
	for _, v := range versions {
		versionDir := filepath.Join(tmpDir, v)
		if err := os.MkdirAll(versionDir, 0o755); err != nil {
			t.Fatalf("Failed to create version dir: %v", err)
		}
		entryPath := filepath.Join(versionDir, EntryPoint)
		if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
			t.Fatalf("Failed to create entry point: %v", err)
		}
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
		versionDir := filepath.Join(tmpDir, v)
		if err := os.MkdirAll(versionDir, 0o755); err != nil {
			t.Fatalf("Failed to create version dir: %v", err)
		}
		entryPath := filepath.Join(versionDir, EntryPoint)
		if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
			t.Fatalf("Failed to create entry point: %v", err)
		}
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
	versionDir := filepath.Join(tmpDir, "v1.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("Failed to create version dir: %v", err)
	}
	entryPath := filepath.Join(versionDir, EntryPoint)
	if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
		t.Fatalf("Failed to create entry point: %v", err)
	}

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
		versionDir := filepath.Join(tmpDir, v)
		if err := os.MkdirAll(versionDir, 0o755); err != nil {
			t.Fatalf("Failed to create version dir: %v", err)
		}
		entryPath := filepath.Join(versionDir, EntryPoint)
		if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
			t.Fatalf("Failed to create entry point: %v", err)
		}
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
	versionDir := filepath.Join(tmpDir, "v1.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("Failed to create version dir: %v", err)
	}
	entryPath := filepath.Join(versionDir, EntryPoint)
	if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
		t.Fatalf("Failed to create entry point: %v", err)
	}

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
	versionDir := filepath.Join(tmpDir, "v1.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("Failed to create version dir: %v", err)
	}
	entryPath := filepath.Join(versionDir, EntryPoint)
	if err := os.WriteFile(entryPath, []byte("// mock"), 0o644); err != nil {
		t.Fatalf("Failed to create entry point: %v", err)
	}

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
		{"1.0.0", false},  // missing v prefix
		{"v1.0", false},   // missing patch
		{"v1", false},     // missing minor and patch
		{"vX.Y.Z", false}, // not numbers
		{"", false},
		{"v1.0.0-beta", false}, // prerelease not supported
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
