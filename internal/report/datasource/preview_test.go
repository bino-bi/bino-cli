package datasource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewDataSetCSV(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("id,amount\n1,42.5\n2,-37.0\n3,0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := openProbeSession(ctx, t)
	res, err := PreviewDataSet(ctx, session, PreviewRequest{
		SpecJSON:   json.RawMessage(`{"type":"csv","path":"data.csv"}`),
		SourceName: "sales_src",
		// References the registered view by name, exercises a cast and op().
		SQL:     `SELECT amount::DECIMAL(18,2) AS amount, op(amount) AS sign FROM sales_src ORDER BY id`,
		BaseDir: dir,
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	wantCols := []string{"amount", "sign"}
	if len(res.Columns) != len(wantCols) {
		t.Fatalf("columns = %+v, want %v", res.Columns, wantCols)
	}
	for i, c := range res.Columns {
		if c.Name != wantCols[i] {
			t.Errorf("column[%d].Name = %q, want %q", i, c.Name, wantCols[i])
		}
	}

	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (limit)", len(res.Rows))
	}
	if !res.Truncated {
		t.Error("expected truncated = true with limit 2 over 3 rows")
	}
	if got := res.Rows[0]["sign"]; got != "+" {
		t.Errorf("row[0].sign = %v, want %q", got, "+")
	}
	if got := res.Rows[1]["sign"]; got != "-" {
		t.Errorf("row[1].sign = %v, want %q", got, "-")
	}
}

func TestPreviewDataSetRejectsEmptySQL(t *testing.T) {
	ctx := context.Background()
	session := openProbeSession(ctx, t)
	_, err := PreviewDataSet(ctx, session, PreviewRequest{
		SpecJSON:   json.RawMessage(`{"type":"csv","path":"data.csv"}`),
		SourceName: "sales_src",
		SQL:        "   ;  ",
	})
	if err == nil {
		t.Fatal("expected error for empty SQL")
	}
}
