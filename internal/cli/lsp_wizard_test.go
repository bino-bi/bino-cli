package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/schema"
)

func newWizardProject(t *testing.T) (dir, csv string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("name = \"test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	csv = filepath.Join(dir, "data", "sales.csv")
	if err := os.MkdirAll(filepath.Dir(csv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csv, []byte("id,Full Name,amount\n1,Ada,42.5\n2,Nik,37.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, csv
}

func TestRunLSPIntrospectDraftCSV(t *testing.T) {
	dir, csv := newWizardProject(t)
	spec := fmt.Sprintf(`{"type":"csv","path":%q}`, csv)

	var buf bytes.Buffer
	if err := runLSPIntrospectDraft(context.Background(), dir, "", []byte(spec), "", 50, &buf); err != nil {
		t.Fatalf("introspect: %v", err)
	}

	var res lspIntrospectResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if res.Error != "" {
		t.Fatalf("error: %s", res.Error)
	}
	if res.Version == "" {
		t.Error("missing version")
	}
	want := []string{"id", "Full Name", "amount"}
	if len(res.Columns) != len(want) {
		t.Fatalf("columns = %+v, want %v", res.Columns, want)
	}
	for i, c := range res.Columns {
		if c.Name != want[i] {
			t.Errorf("col[%d] = %q, want %q", i, c.Name, want[i])
		}
	}
	if res.DetectedCSV == nil || res.DetectedCSV.Delimiter != "," {
		t.Errorf("detectedCsv = %+v, want delimiter ','", res.DetectedCSV)
	}
}

func TestRunLSPScaffoldCreatesValidManifests(t *testing.T) {
	dir, csv := newWizardProject(t)
	payload := fmt.Sprintf(`{
		"dataSource": {"name":"sales_src","type":"csv","path":%q,"delimiter":";"},
		"dataSet": {"name":"sales","pretty":true,"columns":[
			{"name":"id","type":"BIGINT"},
			{"name":"Full Name","type":"VARCHAR"},
			{"name":"amount","type":"VARCHAR","targetType":"DECIMAL(18,2)"}
		]}
	}`, csv)

	var buf bytes.Buffer
	if err := runLSPScaffold(context.Background(), dir, []byte(payload), &buf); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	var res lspScaffoldResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if !res.OK || res.Error != "" {
		t.Fatalf("scaffold failed: ok=%v error=%s", res.OK, res.Error)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want 2 (datasource + dataset)", res.Files)
	}

	for _, f := range res.Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatalf("read %s: %v", f.Path, err)
		}
		if err := schema.Validate(b); err != nil {
			t.Fatalf("file %s failed schema.Validate:\n%s\nerror: %v", f.Path, b, err)
		}
	}

	// The DataSet manifest should carry the generated typed SELECT.
	dsetBytes, err := os.ReadFile(filepath.Join(dir, res.Files[1].Path))
	if err != nil {
		t.Fatal(err)
	}
	dset := string(dsetBytes)
	if !bytes.Contains(dsetBytes, []byte("AS full_name")) {
		t.Errorf("dataset missing typed SELECT alias:\n%s", dset)
	}

	// The DataSource path should be relative to its manifest, not absolute.
	dsBytes, err := os.ReadFile(filepath.Join(dir, res.Files[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dsBytes, []byte(csv)) {
		t.Errorf("datasource path should be relative, not absolute:\n%s", dsBytes)
	}
}

// When the wizard sends an edited SQL, the DataSet must carry it verbatim
// instead of regenerating a typed SELECT from the columns.
func TestRunLSPScaffoldUsesEditedSQL(t *testing.T) {
	dir, csv := newWizardProject(t)
	payload := fmt.Sprintf(`{
		"dataSource": {"name":"sales_src","type":"csv","path":%q,"delimiter":";"},
		"dataSet": {"name":"sales","pretty":true,"sql":"SELECT 42 AS sentinel_xyz FROM sales_src","columns":[
			{"name":"id","type":"BIGINT"}
		]}
	}`, csv)

	var buf bytes.Buffer
	if err := runLSPScaffold(context.Background(), dir, []byte(payload), &buf); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	var res lspScaffoldResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if !res.OK || res.Error != "" {
		t.Fatalf("scaffold failed: ok=%v error=%s", res.OK, res.Error)
	}

	dsetBytes, err := os.ReadFile(filepath.Join(dir, res.Files[1].Path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dsetBytes, []byte("sentinel_xyz")) {
		t.Errorf("dataset should use the edited SQL verbatim:\n%s", dsetBytes)
	}
	if bytes.Contains(dsetBytes, []byte(`"id"`)) {
		t.Errorf("dataset should not contain a regenerated typed SELECT:\n%s", dsetBytes)
	}
	if err := schema.Validate(dsetBytes); err != nil {
		t.Fatalf("edited-SQL dataset failed schema.Validate:\n%s\nerror: %v", dsetBytes, err)
	}
}
