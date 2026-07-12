package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func writeToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readToml(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "bino.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func depsOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	var parsed struct {
		Dependencies map[string]string `toml:"dependencies"`
	}
	if err := toml.Unmarshal([]byte(readToml(t, dir)), &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	return parsed.Dependencies
}

func TestSetDependencyCreatesTable(t *testing.T) {
	dir := writeToml(t, "report-id = \"r1\"\n")
	if err := SetDependency(dir, "@acme/tbl", "latest"); err != nil {
		t.Fatal(err)
	}
	if got := depsOf(t, dir); got["@acme/tbl"] != "latest" {
		t.Errorf("deps = %v", got)
	}
	if !strings.Contains(readToml(t, dir), "report-id = \"r1\"") {
		t.Error("existing content lost")
	}
}

func TestSetDependencyInsertsIntoExistingTable(t *testing.T) {
	dir := writeToml(t, `# my project
report-id = "r1"

[dependencies]
"@bino/style_a" = "1.4.0"

[registry]
url = "http://localhost"
`)
	if err := SetDependency(dir, "@acme/tbl", "latest"); err != nil {
		t.Fatal(err)
	}
	got := depsOf(t, dir)
	if got["@acme/tbl"] != "latest" || got["@bino/style_a"] != "1.4.0" {
		t.Errorf("deps = %v", got)
	}
	content := readToml(t, dir)
	if !strings.Contains(content, "# my project") || !strings.Contains(content, "[registry]") {
		t.Errorf("comments or other tables damaged:\n%s", content)
	}
}

func TestSetDependencyReplacesExistingKey(t *testing.T) {
	before := `report-id = "r1"

[dependencies]
"@acme/tbl" = "1.0.0" # pinned for reasons
"@bino/style_a" = "1.4.0"
`
	dir := writeToml(t, before)
	if err := SetDependency(dir, "@acme/tbl", "latest"); err != nil {
		t.Fatal(err)
	}
	got := depsOf(t, dir)
	if got["@acme/tbl"] != "latest" {
		t.Errorf("deps = %v", got)
	}
	// Only the one key line changed; sibling untouched.
	if !strings.Contains(readToml(t, dir), `"@bino/style_a" = "1.4.0"`) {
		t.Error("sibling key damaged")
	}
}

func TestSetDependencyPreservesUnrelatedContentByteForByte(t *testing.T) {
	prefix := `# heading comment
report-id = "r1" # trailing comment

[plugins.foo]
version = ">=1.0.0"

`
	dir := writeToml(t, prefix+"[dependencies]\n\"@a/x\" = \"1.0.0\"\n")
	if err := SetDependency(dir, "@a/y", "latest"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(readToml(t, dir), prefix) {
		t.Errorf("prefix not preserved byte-for-byte:\n%s", readToml(t, dir))
	}
}

func TestRemoveDependency(t *testing.T) {
	dir := writeToml(t, `report-id = "r1"

[dependencies]
"@acme/tbl" = "latest"
"@bino/style_a" = "1.4.0"
`)
	if err := RemoveDependency(dir, "@acme/tbl"); err != nil {
		t.Fatal(err)
	}
	got := depsOf(t, dir)
	if _, ok := got["@acme/tbl"]; ok {
		t.Errorf("key not removed: %v", got)
	}
	if got["@bino/style_a"] != "1.4.0" {
		t.Errorf("sibling lost: %v", got)
	}
	// Removing a missing key is a no-op.
	if err := RemoveDependency(dir, "@acme/tbl"); err != nil {
		t.Errorf("no-op remove: %v", err)
	}
}

func TestSetDependencyRejectsInlineTable(t *testing.T) {
	dir := writeToml(t, "report-id = \"r1\"\ndependencies = { \"@a/x\" = \"1.0.0\" }\n")
	if err := SetDependency(dir, "@a/y", "latest"); err == nil {
		t.Fatal("expected error for inline dependencies table")
	}
	// Nothing corrupted.
	if !strings.Contains(readToml(t, dir), `dependencies = { "@a/x" = "1.0.0" }`) {
		t.Error("file was modified despite the error")
	}
}
