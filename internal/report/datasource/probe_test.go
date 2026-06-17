package datasource

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/pkg/duckdb"
)

func openProbeSession(ctx context.Context, t *testing.T) *duckdb.Session {
	t.Helper()
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Fatalf("duckdb options: %v", err)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestProbeCSV(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("id,Full Name,amount\n1,Ada,42.5\n2,Nik,37.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := openProbeSession(ctx, t)
	res, err := Probe(ctx, session, ProbeRequest{
		SpecJSON: json.RawMessage(`{"type":"csv","path":"data.csv"}`),
		BaseDir:  dir,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	wantCols := []string{"id", "Full Name", "amount"}
	if len(res.Columns) != len(wantCols) {
		t.Fatalf("columns = %+v, want %v", res.Columns, wantCols)
	}
	for i, c := range res.Columns {
		if c.Name != wantCols[i] {
			t.Errorf("column[%d].Name = %q, want %q", i, c.Name, wantCols[i])
		}
		if c.Type == "" {
			t.Errorf("column[%d] %q has empty type", i, c.Name)
		}
	}

	if len(res.SampleRows) != 1 {
		t.Errorf("sample rows = %d, want 1 (limit)", len(res.SampleRows))
	}
	if !res.Truncated {
		t.Error("expected truncated = true with limit 1 over 2 rows")
	}
	if res.DetectedCSV == nil || res.DetectedCSV.Delimiter != "," {
		t.Errorf("detectedCsv = %+v, want delimiter ','", res.DetectedCSV)
	}
	if res.DetectedCSV.HasHeader == nil || !*res.DetectedCSV.HasHeader {
		t.Errorf("detectedCsv.HasHeader = %v, want true", res.DetectedCSV.HasHeader)
	}
}

func TestProbeParquet(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	parquetPath := filepath.Join(dir, "data.parquet")
	if err := os.WriteFile(csvPath, []byte("id,city\n1,Berlin\n2,Oslo\n3,Zurich\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := openProbeSession(ctx, t)
	copySQL := fmt.Sprintf("COPY (SELECT * FROM read_csv_auto('%s')) TO '%s' (FORMAT parquet)",
		escapeSQLString(csvPath), escapeSQLString(parquetPath))
	if _, err := session.DB().ExecContext(ctx, copySQL); err != nil {
		t.Fatalf("write parquet: %v", err)
	}

	res, err := Probe(ctx, session, ProbeRequest{
		SpecJSON: json.RawMessage(`{"type":"parquet","path":"data.parquet"}`),
		BaseDir:  dir,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0].Name != "id" || res.Columns[1].Name != "city" {
		t.Fatalf("columns = %+v, want [id city]", res.Columns)
	}
	if len(res.SampleRows) != 3 {
		t.Errorf("sample rows = %d, want 3", len(res.SampleRows))
	}
	if res.Truncated {
		t.Error("expected truncated = false")
	}
}

func TestProbeInlineRejected(t *testing.T) {
	ctx := context.Background()
	session := openProbeSession(ctx, t)
	_, err := Probe(ctx, session, ProbeRequest{
		SpecJSON: json.RawMessage(`{"type":"inline","content":[]}`),
	})
	if err == nil {
		t.Fatal("expected error for inline source")
	}
}

func TestSheetNames(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "book.xlsx")
	writeMinimalXLSX(t, xlsxPath, []string{"Q1", "Q2", "Summary"})

	got, err := sheetNames(xlsxPath)
	if err != nil {
		t.Fatalf("sheetNames: %v", err)
	}
	want := []string{"Q1", "Q2", "Summary"}
	if len(got) != len(want) {
		t.Fatalf("sheets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sheet[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExcelSheetParamSQL(t *testing.T) {
	// URL paths bypass the file-existence check, so this verifies the sheet
	// parameter wiring without a real workbook.
	spec := sourceSpec{Type: sourceTypeExcel, Path: "https://example.com/book.xlsx", Sheet: "Q2"}
	got, err := buildViewSourceSQL(spec)
	if err != nil {
		t.Fatalf("buildViewSourceSQL: %v", err)
	}
	want := "SELECT * FROM read_xlsx('https://example.com/book.xlsx', sheet = 'Q2')"
	if got != want {
		t.Errorf("got:  %s\nwant: %s", got, want)
	}
}

// writeMinimalXLSX writes a zip with just xl/workbook.xml — enough for sheetNames.
func writeMinimalXLSX(t *testing.T, path string, sheets []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("xl/workbook.xml")
	if err != nil {
		t.Fatal(err)
	}
	xml := `<?xml version="1.0" encoding="UTF-8"?><workbook><sheets>`
	for i, name := range sheets {
		xml += fmt.Sprintf(`<sheet name="%s" sheetId="%d"/>`, name, i+1)
	}
	xml += `</sheets></workbook>`
	if _, err := w.Write([]byte(xml)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
