package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/runtimecfg"
)

func TestLoadDirSkipsIgnoredDirectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "main.yaml"), "main")

	ignoredDir := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	writeManifest(t, filepath.Join(ignoredDir, "ignored.yaml"), "ignored")

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	docs, err := LoadDir(ctx, root)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Name != "main" {
		t.Fatalf("unexpected document name: %s", docs[0].Name)
	}
}

func TestLoadDirEnforcesFileLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "a.yaml"), "a")
	writeManifest(t, filepath.Join(root, "b.yaml"), "b")

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 1
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	if _, err := LoadDir(ctx, root); err == nil {
		t.Fatal("expected error due to file limit")
	}
}

func TestLoadDirEnforcesDocumentLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "multi.yaml")
	content := minimalManifest("first") + "\n---\n" + minimalManifest("second")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write multi.yaml: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 1
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	if _, err := LoadDir(ctx, root); err == nil {
		t.Fatal("expected error due to document limit")
	}
}

func TestLoadDirEnforcesByteLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	large := filepath.Join(root, "large.yaml")
	if err := os.WriteFile(large, []byte(minimalManifest("large")+"\n"+strings.Repeat("#", 2048)), 0o600); err != nil {
		t.Fatalf("write large.yaml: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1024
		return cfg
	})

	if _, err := LoadDir(ctx, root); err == nil {
		t.Fatal("expected error due to byte limit")
	}
}

func TestLoadDirRespectsBnignore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "main.yaml"), "main")

	// Create an "issues" directory with a manifest that should be ignored.
	issuesDir := filepath.Join(root, "issues")
	if err := os.MkdirAll(issuesDir, 0o755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	writeManifest(t, filepath.Join(issuesDir, "draft.yaml"), "draft")

	// Write .bnignore with "issues/" pattern.
	bnignore := filepath.Join(root, ".bnignore")
	if err := os.WriteFile(bnignore, []byte("issues/\n"), 0o600); err != nil {
		t.Fatalf("write .bnignore: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	docs, err := LoadDir(ctx, root)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Name != "main" {
		t.Fatalf("expected document 'main', got %q", docs[0].Name)
	}
}

func TestLoadDirBnignoreFilePattern(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "main.yaml"), "main")
	writeManifest(t, filepath.Join(root, "scratch.yaml"), "scratch")

	// Ignore a specific file pattern.
	bnignore := filepath.Join(root, ".bnignore")
	if err := os.WriteFile(bnignore, []byte("scratch.yaml\n"), 0o600); err != nil {
		t.Fatalf("write .bnignore: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	docs, err := LoadDir(ctx, root)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Name != "main" {
		t.Fatalf("expected document 'main', got %q", docs[0].Name)
	}
}

func overrideConfig(t *testing.T, mutate func(runtimecfg.Config) runtimecfg.Config) {
	t.Helper()
	cfg := runtimecfg.Current()
	cfg = mutate(cfg)
	restore := runtimecfg.SetForTests(cfg)
	t.Cleanup(restore)
}

func writeManifest(t *testing.T, path, name string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(minimalManifest(name)), 0o600); err != nil {
		t.Fatalf("write manifest %s: %v", path, err)
	}
}

func minimalManifest(name string) string {
	return "apiVersion: bino.bi/v1alpha1\n" +
		"kind: DataSource\n" +
		"metadata:\n" +
		"  name: " + name + "\n" +
		"spec:\n" +
		"  type: inline\n" +
		"  inline:\n" +
		"    content: []\n"
}
