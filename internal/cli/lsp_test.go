package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLSPIndexCommand(t *testing.T) {
	// Use the examples/minimal directory for testing
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	// Navigate to project root
	projectRoot := filepath.Join(wd, "..", "..")
	minimalDir := filepath.Join(projectRoot, "examples", "minimal")

	if _, err := os.Stat(minimalDir); os.IsNotExist(err) {
		t.Skipf("examples/minimal directory not found at %s", minimalDir)
	}

	var buf bytes.Buffer
	err = runLSPIndex(context.Background(), minimalDir, &buf)
	if err != nil {
		t.Fatalf("runLSPIndex failed: %v", err)
	}

	var result LSPIndexResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	if len(result.Documents) == 0 {
		t.Error("expected at least one document")
	}

	// Verify we found expected document kinds
	kindCounts := make(map[string]int)
	for _, doc := range result.Documents {
		kindCounts[doc.Kind]++
	}

	t.Logf("Found %d documents: %v", len(result.Documents), kindCounts)
}

func TestLSPColumnsCommand(t *testing.T) {
	// Use the examples/coffee-report directory for testing
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	// Navigate to project root
	projectRoot := filepath.Join(wd, "..", "..")
	coffeeDir := filepath.Join(projectRoot, "examples", "coffee-report")

	if _, err := os.Stat(coffeeDir); os.IsNotExist(err) {
		t.Skipf("examples/coffee-report directory not found at %s", coffeeDir)
	}

	var buf bytes.Buffer
	err = runLSPColumns(context.Background(), coffeeDir, "local_drop", &buf)
	if err != nil {
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
		t.Error("expected at least one column")
	}

	t.Logf("Found columns for %s: %v", result.Name, result.Columns)
}

func TestLSPColumnsCommandWithPrefix(t *testing.T) {
	// Test with $ prefix for DataSource
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	projectRoot := filepath.Join(wd, "..", "..")
	coffeeDir := filepath.Join(projectRoot, "examples", "coffee-report")

	if _, err := os.Stat(coffeeDir); os.IsNotExist(err) {
		t.Skipf("examples/coffee-report directory not found at %s", coffeeDir)
	}

	var buf bytes.Buffer
	err = runLSPColumns(context.Background(), coffeeDir, "$local_drop", &buf)
	if err != nil {
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
		t.Error("expected at least one column")
	}

	t.Logf("Found columns for %s: %v", result.Name, result.Columns)
}

func TestLSPValidateCommand(t *testing.T) {
	// Use the examples/minimal directory for testing
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	// Navigate to project root
	projectRoot := filepath.Join(wd, "..", "..")
	minimalDir := filepath.Join(projectRoot, "examples", "minimal")

	if _, err := os.Stat(minimalDir); os.IsNotExist(err) {
		t.Skipf("examples/minimal directory not found at %s", minimalDir)
	}

	var buf bytes.Buffer
	err = runLSPValidate(context.Background(), minimalDir, false, &buf)
	if err != nil {
		t.Fatalf("runLSPValidate failed: %v", err)
	}

	var result LSPValidateResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	t.Logf("Validation valid: %v, diagnostics: %d", result.Valid, len(result.Diagnostics))
}
