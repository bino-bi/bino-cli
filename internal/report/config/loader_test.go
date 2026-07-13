package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/spec"
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

func TestLoadDirCollectErrorsContinuesAcrossFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// One schema-invalid manifest (unknown property), one syntactically broken
	// file, and one valid manifest.
	invalid := minimalManifest("broken") + "  bogusExtraField: true\n"
	if err := os.WriteFile(filepath.Join(root, "a_invalid.yaml"), []byte(invalid), 0o600); err != nil {
		t.Fatalf("write a_invalid.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b_syntax.yaml"), []byte("foo: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("write b_syntax.yaml: %v", err)
	}
	writeManifest(t, filepath.Join(root, "c_valid.yaml"), "valid")

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	var collected []error
	docs, err := LoadDirWithOptions(ctx, root, LoadOptions{CollectErrors: &collected})
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(docs) != 1 || docs[0].Name != "valid" {
		t.Fatalf("expected only the valid document, got %+v", docs)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 collected errors, got %d: %v", len(collected), collected)
	}
	var schemaErr *spec.SchemaValidationError
	if !errors.As(collected[0], &schemaErr) {
		t.Fatalf("expected first error to be SchemaValidationError, got %T: %v", collected[0], collected[0])
	}
	if !strings.HasSuffix(schemaErr.File, "a_invalid.yaml") {
		t.Fatalf("expected error file a_invalid.yaml, got %q", schemaErr.File)
	}
	if !strings.Contains(collected[1].Error(), "b_syntax.yaml") {
		t.Fatalf("expected second error to reference b_syntax.yaml, got %v", collected[1])
	}
}

func TestLoadDirCollectErrorsContinuesWithinFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Multi-doc file: document #1 is schema-invalid, document #2 is valid.
	content := minimalManifest("first") + "  bogusExtraField: true\n---\n" + minimalManifest("second")
	if err := os.WriteFile(filepath.Join(root, "multi.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write multi.yaml: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	var collected []error
	docs, err := LoadDirWithOptions(ctx, root, LoadOptions{CollectErrors: &collected})
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(docs) != 1 || docs[0].Name != "second" {
		t.Fatalf("expected only the second document, got %+v", docs)
	}
	if docs[0].Position != 2 {
		t.Fatalf("expected position 2 for surviving document, got %d", docs[0].Position)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected error, got %d: %v", len(collected), collected)
	}
	var schemaErr *spec.SchemaValidationError
	if !errors.As(collected[0], &schemaErr) {
		t.Fatalf("expected SchemaValidationError, got %T: %v", collected[0], collected[0])
	}
	if schemaErr.DocPosition != 1 {
		t.Fatalf("expected error for document #1, got #%d", schemaErr.DocPosition)
	}
}

func TestLoadDirWithoutCollectErrorsFailsFast(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	invalid := minimalManifest("broken") + "  bogusExtraField: true\n"
	if err := os.WriteFile(filepath.Join(root, "invalid.yaml"), []byte(invalid), 0o600); err != nil {
		t.Fatalf("write invalid.yaml: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	if _, err := LoadDir(ctx, root); err == nil {
		t.Fatal("expected error without CollectErrors")
	}
}

// TestLoadDirOverlayUsesBufferContent proves the buffer-driven embedding seam:
// when LoadOptions.Overlay holds replacement content for a file, the loader
// parses the overlay instead of reading disk, and a non-overlaid sibling is
// still read from disk. The overlay key and the on-disk path differ in form
// (relative vs absolute) to exercise key normalization.
func TestLoadDirOverlayUsesBufferContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	diskPath := filepath.Join(root, "a.yaml")
	writeManifest(t, diskPath, "disk_name") // on-disk name
	writeManifest(t, filepath.Join(root, "b.yaml"), "sibling")

	// Overlay replaces a.yaml's content with a document that has a different
	// metadata.name, so a successful overlay is observable by name.
	overlayBody := minimalManifest("buffer_name")

	docs, err := LoadDirWithOptions(ctx, root, LoadOptions{
		Overlay: map[string]string{diskPath: overlayBody},
	})
	if err != nil {
		t.Fatalf("load dir with overlay: %v", err)
	}

	names := make(map[string]bool, len(docs))
	for _, d := range docs {
		names[d.Name] = true
	}
	if !names["buffer_name"] {
		t.Errorf("overlay content not parsed: want a document named %q, got %v", "buffer_name", names)
	}
	if names["disk_name"] {
		t.Errorf("disk content was read for an overlaid file: unexpected %q in %v", "disk_name", names)
	}
	if !names["sibling"] {
		t.Errorf("non-overlaid sibling not read from disk: want %q in %v", "sibling", names)
	}
}

// TestLoadDirOverlayKeyNormalization confirms the overlay matches regardless of
// whether the caller's key is relative, dot-prefixed, or already absolute.
func TestLoadDirOverlayKeyNormalization(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	diskPath := filepath.Join(root, "a.yaml")
	writeManifest(t, diskPath, "disk_name")

	// A non-clean absolute key (…/./a.yaml) must still match the loaded path.
	messyKey := filepath.Join(root, ".", "a.yaml")
	docs, err := LoadDirWithOptions(ctx, root, LoadOptions{
		Overlay: map[string]string{messyKey: minimalManifest("buffer_name")},
	})
	if err != nil {
		t.Fatalf("load dir with overlay: %v", err)
	}

	for _, d := range docs {
		if d.Name == "disk_name" {
			t.Fatalf("overlay key did not normalize: disk content was read despite overlay key %q", messyKey)
		}
	}
}

func overrideConfig(t *testing.T, mutate func(runtimecfg.Config) runtimecfg.Config) {
	t.Helper()
	cfg := runtimecfg.Current()
	cfg = mutate(cfg)
	restore := runtimecfg.SetForTests(cfg)
	t.Cleanup(restore)
}

func TestLoadDirAcceptsScopedNames(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	multiDoc := "apiVersion: bino.bi/v1alpha1\n" +
		"kind: Table\n" +
		"metadata:\n" +
		"  name: \"@acme/revenue-table\"\n" +
		"spec:\n" +
		"  dataset: revenue\n" +
		"---\n" +
		minimalManifest("revenue")
	if err := os.WriteFile(filepath.Join(root, "predefs.yaml"), []byte(multiDoc), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
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
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	names := map[string]bool{}
	for _, d := range docs {
		names[d.Name] = true
	}
	if !names["@acme/revenue-table"] {
		t.Fatalf("scoped-name document not loaded, got names: %v", names)
	}
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

func TestLoadDirIncludesRegistryDocs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "main.yaml"), "main")

	// Installed registry package: must be loaded despite living under .bino.
	regScope := filepath.Join(root, ".bino", "registry", "acme")
	if err := os.MkdirAll(regScope, 0o755); err != nil {
		t.Fatal(err)
	}
	scoped := "apiVersion: bino.bi/v1alpha1\n" +
		"kind: Table\n" +
		"metadata:\n" +
		"  name: \"@acme/revenue-table\"\n" +
		"spec:\n" +
		"  dataset: revenue\n"
	if err := os.WriteFile(filepath.Join(regScope, "revenue-table.yml"), []byte(scoped), 0o644); err != nil {
		t.Fatal(err)
	}

	// Anything else under .bino stays invisible.
	cacheDir := filepath.Join(root, ".bino", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(cacheDir, "skip.yaml"), "cached")

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
	names := map[string]bool{}
	for _, d := range docs {
		names[d.Name] = true
	}
	if !names["main"] || !names["@acme/revenue-table"] {
		t.Fatalf("registry doc not loaded, got names: %v", names)
	}
	if names["cached"] {
		t.Fatalf(".bino/cache doc must stay skipped, got names: %v", names)
	}
}

func TestBnignoreExcludesRegistryDocs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "main.yaml"), "main")

	regScope := filepath.Join(root, ".bino", "registry", "acme")
	if err := os.MkdirAll(regScope, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(regScope, "excluded.yml"), "excluded")

	if err := os.WriteFile(filepath.Join(root, ".bnignore"), []byte(".bino/registry/acme/\n"), 0o644); err != nil {
		t.Fatal(err)
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
	if len(docs) != 1 || docs[0].Name != "main" {
		t.Fatalf(".bnignore should exclude the registry scope, got %d docs", len(docs))
	}
}

func TestLoadDirPreservesComponentParamPlaceholders(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	predef := "apiVersion: bino.bi/v1alpha1\n" +
		"kind: Text\n" +
		"metadata:\n" +
		"  name: \"@acme/commentary\"\n" +
		"  params:\n" +
		"    - name: REGION\n" +
		"spec:\n" +
		"  value: \"Report for ${REGION}\"\n"
	if err := os.WriteFile(filepath.Join(root, "predef.yaml"), []byte(predef), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	overrideConfig(t, func(cfg runtimecfg.Config) runtimecfg.Config {
		cfg.MaxManifestFiles = 10
		cfg.MaxManifestDocs = 5
		cfg.MaxManifestBytes = 1_000_000
		return cfg
	})

	load := func(t *testing.T) {
		t.Helper()
		docs, err := LoadDir(ctx, root)
		if err != nil {
			t.Fatalf("load dir: %v", err)
		}
		if len(docs) != 1 {
			t.Fatalf("expected 1 document, got %d", len(docs))
		}
		doc := docs[0]
		if !strings.Contains(string(doc.Raw), "${REGION}") {
			t.Fatalf("param placeholder not preserved, raw: %s", doc.Raw)
		}
		if len(doc.Params) != 1 || doc.Params[0].Name != "REGION" {
			t.Fatalf("metadata.params not captured, got %+v", doc.Params)
		}
		if len(doc.MissingEnvVars) != 0 {
			t.Fatalf("declared param must not count as missing env var, got %v", doc.MissingEnvVars)
		}
	}

	// The placeholder must survive load even when REGION is set in the
	// environment, so it can be expanded later with ref params.
	t.Run("env set", func(t *testing.T) {
		t.Setenv("REGION", "from-env")
		load(t)
	})

	// With REGION unset, the declared param must not be reported missing.
	t.Run("env unset", func(t *testing.T) {
		load(t)
	})
}
