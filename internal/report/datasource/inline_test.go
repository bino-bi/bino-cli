package datasource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlinePayloadHash(t *testing.T) {
	tests := []struct {
		name     string
		payload1 string
		payload2 string
		sameHash bool
	}{
		{
			name:     "same content same hash",
			payload1: `[{"a": 1}]`,
			payload2: `[{"a": 1}]`,
			sameHash: true,
		},
		{
			name:     "different content different hash",
			payload1: `[{"a": 1}]`,
			payload2: `[{"a": 2}]`,
			sameHash: false,
		},
		{
			name:     "empty arrays same hash",
			payload1: `[]`,
			payload2: `[]`,
			sameHash: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h1 := inlinePayloadHash(json.RawMessage(tc.payload1))
			h2 := inlinePayloadHash(json.RawMessage(tc.payload2))

			if tc.sameHash && h1 != h2 {
				t.Errorf("expected same hash, got %q vs %q", h1, h2)
			}
			if !tc.sameHash && h1 == h2 {
				t.Errorf("expected different hashes, got same: %q", h1)
			}

			// Hash should be 16 hex chars (8 bytes)
			if len(h1) != 16 {
				t.Errorf("expected 16-char hash, got %d chars: %q", len(h1), h1)
			}
		})
	}
}

func TestInlineToCSV(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantCols    []string
		wantRows    int
		wantErr     string
		wantContain string
	}{
		{
			name:     "single row single column",
			payload:  `[{"name": "Alice"}]`,
			wantCols: []string{"name"},
			wantRows: 1,
		},
		{
			name:     "multiple rows multiple columns",
			payload:  `[{"a": 1, "b": 2}, {"a": 3, "b": 4}]`,
			wantCols: []string{"a", "b"},
			wantRows: 2,
		},
		{
			name:     "columns are sorted",
			payload:  `[{"z": 1, "a": 2, "m": 3}]`,
			wantCols: []string{"a", "m", "z"},
			wantRows: 1,
		},
		{
			name:     "union of keys across rows",
			payload:  `[{"a": 1}, {"b": 2}]`,
			wantCols: []string{"a", "b"},
			wantRows: 2,
		},
		{
			name:     "empty array",
			payload:  `[]`,
			wantCols: nil,
			wantRows: 0,
		},
		{
			name:     "null values become empty",
			payload:  `[{"a": null}]`,
			wantCols: []string{"a"},
			wantRows: 1,
		},
		{
			name:     "boolean values",
			payload:  `[{"flag": true}, {"flag": false}]`,
			wantCols: []string{"flag"},
			wantRows: 2,
		},
		{
			name:    "nested object error",
			payload: `[{"a": {"b": 1}}]`,
			wantErr: "nested object",
		},
		{
			name:    "nested array error",
			payload: `[{"a": [1, 2, 3]}]`,
			wantErr: "nested array",
		},
		{
			name:    "non-object row error",
			payload: `["string"]`,
			wantErr: "not an object",
		},
		{
			name:    "not an array error",
			payload: `{"a": 1}`,
			wantErr: "JSON array",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			csvData, cols, err := inlineToCSV(json.RawMessage(tc.payload))

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check columns
			if len(cols) != len(tc.wantCols) {
				t.Fatalf("expected %d columns, got %d: %v", len(tc.wantCols), len(cols), cols)
			}
			for i, col := range tc.wantCols {
				if cols[i] != col {
					t.Errorf("column %d: expected %q, got %q", i, col, cols[i])
				}
			}

			// Check row count (header + data rows)
			if tc.wantRows == 0 && len(csvData) == 0 {
				// Empty case is fine
				return
			}

			// Count lines by splitting on newline, but handle trailing newline correctly
			csvStr := string(csvData)
			if len(csvStr) > 0 && csvStr[len(csvStr)-1] == '\n' {
				csvStr = csvStr[:len(csvStr)-1]
			}
			lines := strings.Split(csvStr, "\n")
			// First line is header
			if tc.wantRows > 0 && len(lines) != tc.wantRows+1 {
				t.Errorf("expected %d rows + header, got %d lines:\n%s", tc.wantRows, len(lines), string(csvData))
			}

			// Check for expected content if specified
			if tc.wantContain != "" && !strings.Contains(string(csvData), tc.wantContain) {
				t.Errorf("expected CSV to contain %q, got:\n%s", tc.wantContain, string(csvData))
			}
		})
	}
}

func TestBuildInlineViewSQL(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		dsName      string
		payload     string
		wantContain string
		wantErr     string
	}{
		{
			name:        "creates view with read_csv_auto",
			dsName:      "test_ds",
			payload:     `[{"a": 1, "b": 2}]`,
			wantContain: `CREATE OR REPLACE VIEW "test_ds" AS SELECT * FROM read_csv_auto`,
		},
		{
			name:        "empty array creates empty view",
			dsName:      "empty_ds",
			payload:     `[]`,
			wantContain: `CREATE OR REPLACE VIEW "empty_ds" AS SELECT 1 WHERE 1=0`,
		},
		{
			name:    "nested object error",
			dsName:  "bad_ds",
			payload: `[{"a": {"nested": true}}]`,
			wantErr: "nested object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := buildInlineViewSQL(tc.dsName, tempDir, json.RawMessage(tc.payload))

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(sql, tc.wantContain) {
				t.Errorf("expected SQL to contain %q, got: %s", tc.wantContain, sql)
			}
		})
	}
}

func TestWriteInlineCSV(t *testing.T) {
	tempDir := t.TempDir()

	payload := json.RawMessage(`[{"name": "Alice", "age": 30}]`)
	csvPath, err := writeInlineCSV(tempDir, "test_source", payload)
	if err != nil {
		t.Fatalf("writeInlineCSV: %v", err)
	}

	// Check file was created
	if _, err := os.Stat(csvPath); err != nil {
		t.Fatalf("CSV file not created: %v", err)
	}

	// Check filename contains name and hash
	filename := filepath.Base(csvPath)
	if !strings.HasPrefix(filename, "test_source-") {
		t.Errorf("expected filename to start with 'test_source-', got %q", filename)
	}
	if !strings.HasSuffix(filename, ".csv") {
		t.Errorf("expected filename to end with '.csv', got %q", filename)
	}

	// Check content
	content, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}

	// Should have header and one row
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d:\n%s", len(lines), string(content))
	}

	// Header should be sorted columns
	if lines[0] != "age,name" {
		t.Errorf("expected header 'age,name', got %q", lines[0])
	}
}
