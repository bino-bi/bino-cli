package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/engine"
)

var dockerfileEnginePin = regexp.MustCompile(`(?m)^ARG ENGINE_VERSION=(\S+)`)

// defaultI18nTokens is copied by hand from the engine. The engine pin moved to
// next.27, which dropped the "==" markers from the no-data label, and nobody
// re-synced the copy, so `bino add i18n --defaults` kept writing "==No Data==".
// This test reads the bundles of the pinned engine and compares them.
//
// It needs node and the pinned engine in the local cache
// (`bino setup --template-engine --engine-version <pin>`); CI installs both.
func TestDefaultI18nTokensMatchPinnedEngine(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	m := dockerfileEnginePin.FindSubmatch(dockerfile)
	if m == nil {
		t.Fatal("no ARG ENGINE_VERSION pin in Dockerfile")
	}
	pin := string(m[1])

	mgr, err := engine.NewManager()
	if err != nil {
		t.Fatalf("engine manager: %v", err)
	}
	info, err := mgr.ResolveVersion(pin)
	if err != nil {
		t.Skipf("pinned engine not installed (%v); run `bino setup --template-engine --engine-version %s`", err, pin)
	}

	// The engine ships its stores as ES modules with extension-less relative
	// imports, which node only loads from a "type": "module" package with the
	// ".js" spelled out.
	dir := t.TempDir()
	store := readEngineFile(t, info.Path, "collection/stores/internationalization.js")
	store = strings.ReplaceAll(store, `"../utils/flattenObject"`, `"../utils/flattenObject.js"`)
	writeTempFile(t, dir, "package.json", `{"type": "module"}`)
	writeTempFile(t, dir, "stores/internationalization.js", store)
	writeTempFile(t, dir, "utils/flattenObject.js", readEngineFile(t, info.Path, "collection/utils/flattenObject.js"))
	writeTempFile(t, dir, "dump.js", `import { SYSTEM_I18N_DEFAULTS } from "./stores/internationalization.js";
process.stdout.write(JSON.stringify(SYSTEM_I18N_DEFAULTS._system));
`)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), node, "dump.js")
	cmd.Dir = dir
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("load engine %s i18n bundles: %v\n%s", pin, err, stderr.String())
	}
	var engineTokens map[string]map[string]string
	if err := json.Unmarshal(out, &engineTokens); err != nil {
		t.Fatalf("decode engine %s i18n bundles: %v", pin, err)
	}

	for locale, want := range engineTokens {
		got, ok := defaultI18nTokens[locale]
		if !ok {
			t.Errorf("engine %s ships locale %q, which defaultI18nTokens lacks", pin, locale)
			continue
		}
		for key, value := range want {
			if gotValue, ok := got[key]; !ok {
				t.Errorf("%s: %q is in engine %s but not in defaultI18nTokens", locale, key, pin)
			} else if gotValue != value {
				t.Errorf("%s: %q = %q, engine %s has %q", locale, key, gotValue, pin, value)
			}
		}
		for key := range got {
			if _, ok := want[key]; !ok {
				t.Errorf("%s: %q is in defaultI18nTokens but not in engine %s", locale, key, pin)
			}
		}
	}
	for locale := range defaultI18nTokens {
		if _, ok := engineTokens[locale]; !ok {
			t.Errorf("defaultI18nTokens has locale %q, which engine %s does not ship", locale, pin)
		}
	}
}

func readEngineFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read engine file: %v", err)
	}
	return string(b)
}

func writeTempFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
