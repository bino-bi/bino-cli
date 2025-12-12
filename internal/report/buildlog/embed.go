// Package buildlog provides build log functionality including CSV embedding.
package buildlog

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"strings"
)

// sensitivePatterns defines column name patterns that should be redacted.
var sensitivePatterns = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"key",
	"api_key",
	"private",
	"credential",
	"auth",
}

// EmbedOptions configures CSV embedding behavior.
type EmbedOptions struct {
	Enable   bool // Whether CSV embedding is enabled
	MaxRows  int  // Maximum rows to embed (default: 10)
	MaxBytes int  // Maximum bytes per CSV (default: 65536)
	Base64   bool // Whether to base64 encode CSV (default: true)
	Redact   bool // Whether to redact sensitive columns (default: true)
}

// DefaultEmbedOptions returns sensible default embed options.
func DefaultEmbedOptions() EmbedOptions {
	return EmbedOptions{
		Enable:   true,
		MaxRows:  10,
		MaxBytes: 65536,
		Base64:   true,
		Redact:   true,
	}
}

// CSVResult contains the embedded CSV data and metadata.
type CSVResult struct {
	Encoding  string // "base64" or "raw"
	MimeType  string // "text/csv"
	Bytes     int    // Size in bytes
	Rows      int    // Number of rows included
	Truncated bool   // Whether data was truncated
	Data      string // CSV data (base64 or raw)
}

// IsSensitiveColumn returns true if the column name matches sensitive patterns.
// Matching is case-insensitive.
func IsSensitiveColumn(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// RedactValue returns "[REDACTED]" if the column name matches sensitive patterns,
// otherwise returns the original value.
func RedactValue(columnName, value string) string {
	if IsSensitiveColumn(columnName) {
		return "[REDACTED]"
	}
	return value
}

// BuildCSV builds a CSV result from columns and rows with the given options.
// Returns nil if opts.Enable is false or if there is no data.
func BuildCSV(columns []string, rows [][]string, opts EmbedOptions) *CSVResult {
	if !opts.Enable {
		return nil
	}

	if len(columns) == 0 {
		return nil
	}

	// Determine which rows to include
	rowCount := len(rows)
	truncated := false

	if opts.MaxRows > 0 && rowCount > opts.MaxRows {
		rowCount = opts.MaxRows
		truncated = true
	}

	// Build sensitive column index for redaction
	sensitiveColumns := make([]bool, len(columns))
	if opts.Redact {
		for i, col := range columns {
			sensitiveColumns[i] = IsSensitiveColumn(col)
		}
	}

	// Write CSV to buffer
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header
	if err := writer.Write(columns); err != nil {
		return nil
	}

	// Write rows with potential redaction
	for i := 0; i < rowCount; i++ {
		row := rows[i]
		if row == nil {
			continue
		}

		// Apply redaction if enabled
		outputRow := make([]string, len(row))
		for j, val := range row {
			if opts.Redact && j < len(sensitiveColumns) && sensitiveColumns[j] {
				outputRow[j] = "[REDACTED]"
			} else {
				outputRow[j] = val
			}
		}

		if err := writer.Write(outputRow); err != nil {
			return nil
		}

		// Check byte limit after each row
		writer.Flush()
		if opts.MaxBytes > 0 && buf.Len() > opts.MaxBytes {
			truncated = true
			break
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil
	}

	csvData := buf.String()
	csvBytes := len(csvData)

	// Truncate to MaxBytes if exceeded
	if opts.MaxBytes > 0 && csvBytes > opts.MaxBytes {
		csvData = csvData[:opts.MaxBytes]
		csvBytes = opts.MaxBytes
		truncated = true
	}

	result := &CSVResult{
		MimeType:  "text/csv",
		Bytes:     csvBytes,
		Rows:      rowCount,
		Truncated: truncated,
	}

	if opts.Base64 {
		result.Encoding = "base64"
		result.Data = base64.StdEncoding.EncodeToString([]byte(csvData))
	} else {
		result.Encoding = "raw"
		result.Data = csvData
	}

	return result
}
