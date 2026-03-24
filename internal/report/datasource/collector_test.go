package datasource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestCollectInlineAndPathSources(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "orders.csv")
	if err := os.WriteFile(csvPath, []byte("id,drink\n1,espresso\n2,latte\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	inlineRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type": "inline",
			"inline": map[string]any{
				"content": []map[string]any{{"bean": "Mokka", "score": 92}},
			},
		},
	})

	directRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type":    "inline",
			"content": []map[string]any{{"bean": "Guji", "score": 95}},
		},
	})

	stringRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type":    "inline",
			"content": "[\n  {\"bean\": \"Kochere\", \"score\": 91}\n]",
		},
	})

	fileRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type": "csv",
			"path": filepath.Base(csvPath),
		},
	})

	docs := []config.Document{
		{Kind: "DataSource", Name: "inline_notes", File: filepath.Join(tmpDir, "inline.yaml"), Raw: inlineRaw},
		{Kind: "DataSource", Name: "inline_direct", File: filepath.Join(tmpDir, "direct.yaml"), Raw: directRaw},
		{Kind: "DataSource", Name: "inline_string", File: filepath.Join(tmpDir, "string.yaml"), Raw: stringRaw},
		{Kind: "DataSource", Name: "orders", File: filepath.Join(tmpDir, "file.yaml"), Raw: fileRaw},
	}

	results, diags, err := Collect(ctx, docs, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %d", len(diags))
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	inlineRows := decodeRows(t, results[0].Data)
	inlineBean, ok := inlineRows[0]["bean"].(string)
	if len(inlineRows) != 1 || !ok || inlineBean != "Mokka" {
		t.Fatalf("unexpected inline rows: %#v", inlineRows)
	}

	directRows := decodeRows(t, results[1].Data)
	directBean, ok := directRows[0]["bean"].(string)
	if !ok || directBean != "Guji" {
		t.Fatalf("unexpected direct rows: %#v", directRows)
	}

	stringRows := decodeRows(t, results[2].Data)
	stringBean, ok := stringRows[0]["bean"].(string)
	if !ok || stringBean != "Kochere" {
		t.Fatalf("unexpected string rows: %#v", stringRows)
	}

	fileRows := decodeRows(t, results[3].Data)
	if len(fileRows) != 2 {
		t.Fatalf("unexpected file rows: %#v", fileRows)
	}
	fileDrink, ok := fileRows[0]["drink"].(string)
	if !ok || fileDrink != "espresso" {
		t.Fatalf("unexpected first drink: %#v", fileRows[0])
	}
}

func TestCollectContinuesOnMissingSources(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	inlineRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type": "inline",
			"inline": map[string]any{
				"content": []map[string]any{{"bean": "Sidamo"}},
			},
		},
	})

	missingRaw := marshalManifest(t, map[string]any{
		"spec": map[string]any{
			"type": "csv",
			"path": "absent",
		},
	})

	docs := []config.Document{
		{Kind: "DataSource", Name: "inline_ok", File: filepath.Join(tmpDir, "inline.yaml"), Raw: inlineRaw},
		{Kind: "DataSource", Name: "folder_missing", File: filepath.Join(tmpDir, "missing.yaml"), Raw: missingRaw},
	}

	results, diags, err := Collect(ctx, docs, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 successful datasource, got %d", len(results))
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Datasource != "folder_missing" {
		t.Fatalf("unexpected diagnostic datasource: %+v", diags[0])
	}
	if diags[0].Stage == "" {
		t.Fatalf("expected diagnostic stage to be set")
	}
}

func marshalManifest(t *testing.T, manifest map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return raw
}

func decodeRows(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	return rows
}
