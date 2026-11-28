package datasource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func inspect(t *testing.T, body string, declared CSVOptions) CSVReport {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	session := openProbeSession(ctx, t)
	rep, err := InspectCSV(ctx, session.DB(), path, declared)
	if err != nil {
		t.Fatalf("InspectCSV: %v", err)
	}
	return rep
}

func issueOptions(rep CSVReport) []string {
	out := make([]string, 0, len(rep.Issues))
	for _, i := range rep.Issues {
		out = append(out, i.Option+": "+i.Message)
	}
	return out
}

func TestInspectCSVClean(t *testing.T) {
	rep := inspect(t, "id,name,amount,date\n1,Ada,42.5,2024-01-03\n2,Nik,37.0,2024-02-03\n", CSVOptions{Delimiter: ",", DateFormat: "%Y-%m-%d"})
	if len(rep.Issues) != 0 {
		t.Errorf("clean file has issues: %v", issueOptions(rep))
	}
	if rep.Delimiter != "," || !rep.HasHeader || rep.DateFormat != "%Y-%m-%d" || len(rep.Columns) != 4 {
		t.Errorf("unexpected report %+v", rep)
	}
}

func TestInspectCSVNoHeader(t *testing.T) {
	rep := inspect(t, "1,Ada,42.5\n2,Nik,37.0\n3,Bo,1.5\n", CSVOptions{})
	if len(rep.Issues) != 1 || rep.Issues[0].Option != "header" || !strings.Contains(rep.Issues[0].Message, "no header row") {
		t.Fatalf("issues = %v", issueOptions(rep))
	}
	// A declared header: false with columnNames is intended.
	f := false
	rep = inspect(t, "1,Ada,42.5\n2,Nik,37.0\n", CSVOptions{Header: &f, ColumnNames: []string{"id", "name", "amount"}})
	if len(rep.Issues) != 0 {
		t.Errorf("columnNames without a header row is fine, got %v", issueOptions(rep))
	}
}

func TestInspectCSVHeaderReadAsData(t *testing.T) {
	f := false
	rep := inspect(t, "id,amount\n1,42.5\n2,37.0\n", CSVOptions{Header: &f})
	if len(rep.Issues) != 1 || rep.Issues[0].Option != "header" || !strings.Contains(rep.Issues[0].Message, "read as data") {
		t.Fatalf("issues = %v", issueOptions(rep))
	}
}

func TestInspectCSVHeaderValueInData(t *testing.T) {
	tr := true
	rep := inspect(t, "EU,North\nEU,South\nUS,East\n", CSVOptions{Header: &tr})
	if len(rep.Issues) != 1 || !strings.Contains(rep.Issues[0].Message, `"EU"`) {
		t.Fatalf("issues = %v", issueOptions(rep))
	}
}

func TestInspectCSVDelimiter(t *testing.T) {
	rep := inspect(t, "id;name;amount\n1;Ada;42\n2;Nik;37\n", CSVOptions{Delimiter: ","})
	if len(rep.Issues) != 1 || rep.Issues[0].Option != "delimiter" || !strings.Contains(rep.Issues[0].Message, `";"`) {
		t.Fatalf("issues = %v", issueOptions(rep))
	}
}

func TestInspectCSVDateFormat(t *testing.T) {
	rep := inspect(t, "id,date\n1,2024-01-03\n2,2024-02-03\n", CSVOptions{DateFormat: "%d.%m.%Y"})
	if len(rep.Issues) != 1 || rep.Issues[0].Option != "dateFormat" || !strings.Contains(rep.Issues[0].Message, "2024-01-03") {
		t.Fatalf("issues = %v", issueOptions(rep))
	}
}

func TestLocalCSV(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte("x\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := config.Document{Kind: "DataSource", File: filepath.Join(dir, "ds.yaml"),
		Raw: json.RawMessage(`{"spec":{"type":"csv","path":"a.csv","delimiter":";","dateFormat":"%d.%m.%Y"}}`)}
	path, opts, ok := LocalCSV(doc)
	if !ok || path != filepath.Join(dir, "a.csv") || opts.Delimiter != ";" || opts.DateFormat != "%d.%m.%Y" {
		t.Errorf("LocalCSV = %q %+v %v", path, opts, ok)
	}
	doc.Raw = json.RawMessage(`{"spec":{"type":"csv","path":"missing.csv"}}`)
	if _, _, ok := LocalCSV(doc); ok {
		t.Error("a missing file is not a local CSV")
	}
}
