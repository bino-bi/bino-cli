package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The lsp-helper tests used to depend on an examples/ directory that does not
// exist in the repo, so every test skipped unconditionally on every machine.
// They now build their bundles in t.TempDir().

const lspFixtureDataSource = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: people
spec:
  type: inline
  content:
    - region: "EMEA"
      amount: 12
    - region: "APAC"
      amount: 7
`

const lspFixtureDataSet = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: people_by_region
spec:
  query: "SELECT region, amount FROM people"
`

const lspFixtureLayoutPage = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: main_page
spec:
  children: []
`

// writeLSPFixtureBundle creates a minimal valid project: bino.toml plus the
// given named manifests.
func writeLSPFixtureBundle(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestLSPIndexCommand(t *testing.T) {
	dir := writeLSPFixtureBundle(t, map[string]string{
		"data.yaml": lspFixtureDataSource + "---\n" + lspFixtureDataSet,
		"page.yaml": lspFixtureLayoutPage,
	})

	var buf bytes.Buffer
	if err := runLSPIndex(context.Background(), dir, &buf); err != nil {
		t.Fatalf("runLSPIndex failed: %v", err)
	}

	var result LSPIndexResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	kinds := make(map[string]int)
	for _, doc := range result.Documents {
		kinds[doc.Kind]++
	}
	for _, want := range []string{"DataSource", "DataSet", "LayoutPage"} {
		if kinds[want] != 1 {
			t.Errorf("index lists %d %s documents, want 1 (index: %v)", kinds[want], want, kinds)
		}
	}
}

func TestLSPColumnsCommand(t *testing.T) {
	dir := writeLSPFixtureBundle(t, map[string]string{
		"data.yaml": lspFixtureDataSource + "---\n" + lspFixtureDataSet,
	})

	var buf bytes.Buffer
	if err := runLSPColumns(context.Background(), dir, "people_by_region", &buf); err != nil {
		t.Fatalf("runLSPColumns failed: %v", err)
	}

	var result LSPColumnsResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(result.Columns) == 0 {
		t.Fatal("expected at least one column")
	}
	joined := strings.Join(result.Columns, ",")
	if !strings.Contains(joined, "region") || !strings.Contains(joined, "amount") {
		t.Errorf("columns = %v, want region and amount", result.Columns)
	}
}

func TestLSPColumnsCommandWithPrefix(t *testing.T) {
	dir := writeLSPFixtureBundle(t, map[string]string{
		"data.yaml": lspFixtureDataSource,
	})

	// $name addresses the DataSource directly.
	var buf bytes.Buffer
	if err := runLSPColumns(context.Background(), dir, "$people", &buf); err != nil {
		t.Fatalf("runLSPColumns failed: %v", err)
	}

	var result LSPColumnsResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(result.Columns) == 0 {
		t.Fatal("expected at least one column for $people")
	}
}

// schemaDiagnosticCodes are the diagnostic codes validateDirectory emits for
// schema/parse problems (as opposed to lint or engine-compat findings, which
// depend on the machine's engine cache and must not affect these assertions).
func schemaDiagnostics(diags []LSPDiagnostic) []LSPDiagnostic {
	var out []LSPDiagnostic
	for _, d := range diags {
		if d.Code == "schema-validation" || d.Code == "validation-error" || d.Code == "yaml-syntax" {
			out = append(out, d)
		}
	}
	return out
}

func TestLSPValidateCommand(t *testing.T) {
	t.Run("valid bundle has no schema diagnostics", func(t *testing.T) {
		dir := writeLSPFixtureBundle(t, map[string]string{
			"data.yaml": lspFixtureDataSource + "---\n" + lspFixtureDataSet,
		})

		var buf bytes.Buffer
		if err := runLSPValidate(context.Background(), dir, false, &buf); err != nil {
			t.Fatalf("runLSPValidate failed: %v", err)
		}
		var result LSPValidateResult
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		if bad := schemaDiagnostics(result.Diagnostics); len(bad) != 0 {
			t.Errorf("valid bundle produced schema diagnostics: %+v", bad)
		}
	})

	t.Run("schema-invalid bundle is reported", func(t *testing.T) {
		dir := writeLSPFixtureBundle(t, map[string]string{
			"broken.yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSource\nmetadata:\n  name: broken\nspec:\n  type: inline\n  bogus_field: true\n",
		})

		var buf bytes.Buffer
		if err := runLSPValidate(context.Background(), dir, false, &buf); err != nil {
			t.Fatalf("runLSPValidate failed: %v", err)
		}
		var result LSPValidateResult
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.Valid {
			t.Error("Valid = true for a schema-invalid bundle")
		}
		if bad := schemaDiagnostics(result.Diagnostics); len(bad) == 0 {
			t.Errorf("no schema diagnostics reported for the invalid manifest; got: %+v", result.Diagnostics)
		}
	})
}
