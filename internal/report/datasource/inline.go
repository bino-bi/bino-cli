package datasource

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// inlinePayloadHash computes a SHA256 hash of the canonical inline payload.
// The hash is returned as a 16-character hex string (first 64 bits).
func inlinePayloadHash(payload json.RawMessage) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:8])
}

// inlineToCSV converts an inline payload (JSON array of objects) to CSV bytes.
// Returns the CSV data and the column names in sorted order.
//
// Errors if:
//   - Payload is not an array
//   - Any array element is not an object
//   - Any value is a nested object or array
//
// Empty arrays return CSV with no rows (and no columns).
func inlineToCSV(payload json.RawMessage) (csvData []byte, columns []string, err error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil, nil, fmt.Errorf("inline content must be a JSON array: %w", err)
	}

	// Handle empty array: return empty CSV with no columns
	if len(rows) == 0 {
		return nil, nil, nil
	}

	// Parse all rows as objects and collect column names
	parsedRows := make([]map[string]any, 0, len(rows))
	columnSet := make(map[string]struct{})

	for i, raw := range rows {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, nil, fmt.Errorf("row %d is not an object: %w", i, err)
		}

		// Validate all values are scalars (not nested objects/arrays)
		for key, val := range obj {
			if err := validateScalar(key, val); err != nil {
				return nil, nil, fmt.Errorf("row %d: %w", i, err)
			}
			columnSet[key] = struct{}{}
		}

		parsedRows = append(parsedRows, obj)
	}

	// Sort columns for deterministic output
	columns = make([]string, 0, len(columnSet))
	for col := range columnSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	// Write CSV
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Write header
	if err := w.Write(columns); err != nil {
		return nil, nil, fmt.Errorf("write CSV header: %w", err)
	}

	// Write rows
	record := make([]string, len(columns))
	for _, row := range parsedRows {
		for i, col := range columns {
			val, ok := row[col]
			if !ok || val == nil {
				record[i] = ""
				continue
			}
			record[i] = formatScalar(val)
		}
		if err := w.Write(record); err != nil {
			return nil, nil, fmt.Errorf("write CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, fmt.Errorf("flush CSV: %w", err)
	}

	return buf.Bytes(), columns, nil
}

// validateScalar checks that a value is a scalar (not a nested object or array).
func validateScalar(key string, val any) error {
	if val == nil {
		return nil
	}
	switch val.(type) {
	case string, float64, bool, json.Number:
		return nil
	case map[string]any:
		return fmt.Errorf("field %q contains a nested object", key)
	case []any:
		return fmt.Errorf("field %q contains a nested array", key)
	default:
		// For other numeric types from JSON unmarshal
		return nil
	}
}

// formatScalar converts a scalar value to a CSV cell string.
func formatScalar(val any) string {
	switch v := val.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		// Use %g for compact representation without unnecessary trailing zeros
		return fmt.Sprintf("%g", v)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// writeInlineCSV writes the inline payload to a CSV file in the temp directory.
// The filename is "<name>-<hash>.csv" where hash is the first 16 hex chars of the payload hash.
// Returns the full path to the written file.
func writeInlineCSV(tempDir, name string, payload json.RawMessage) (string, error) {
	csvData, _, err := inlineToCSV(payload)
	if err != nil {
		return "", err
	}

	hash := inlinePayloadHash(payload)
	filename := fmt.Sprintf("%s-%s.csv", name, hash)
	csvPath := filepath.Join(tempDir, filename)

	if err := os.WriteFile(csvPath, csvData, 0o600); err != nil {
		return "", fmt.Errorf("write inline CSV: %w", err)
	}

	return csvPath, nil
}

// buildInlineViewSQL creates the CREATE VIEW SQL for an inline datasource.
// For empty arrays, it creates an empty view with no columns.
// For non-empty arrays, it creates a view over read_csv_auto().
func buildInlineViewSQL(name, tempDir string, payload json.RawMessage) (string, error) {
	csvData, columns, err := inlineToCSV(payload)
	if err != nil {
		return "", err
	}

	// Handle empty inline array: create an empty view
	if len(columns) == 0 {
		return fmt.Sprintf("CREATE OR REPLACE VIEW %q AS SELECT 1 WHERE 1=0", name), nil
	}

	// Write CSV to temp file
	hash := inlinePayloadHash(payload)
	filename := fmt.Sprintf("%s-%s.csv", name, hash)
	csvPath := filepath.Join(tempDir, filename)

	if err := os.WriteFile(csvPath, csvData, 0o600); err != nil {
		return "", fmt.Errorf("write inline CSV: %w", err)
	}

	// Build CREATE VIEW statement
	return fmt.Sprintf(
		"CREATE OR REPLACE VIEW %q AS SELECT * FROM read_csv_auto('%s', header=true)",
		name, escapeSQLString(csvPath),
	), nil
}
