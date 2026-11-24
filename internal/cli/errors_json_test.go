package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

const twoDocs = `# leading comment
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: first
---

apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: second
`

func errorRecords(t *testing.T, err error) []map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	LogErrorJSON(logx.NewJSON(&stdout, &stderr, false), err)
	if stdout.Len() != 0 {
		t.Fatalf("error records went to stdout: %q", stdout.String())
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var rec map[string]any
		if jerr := json.Unmarshal([]byte(line), &rec); jerr != nil {
			t.Fatalf("not a JSON line: %q: %v", line, jerr)
		}
		records = append(records, rec)
	}
	return records
}

func TestDocumentStartLine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(file, []byte(twoDocs), 0o600); err != nil {
		t.Fatal(err)
	}
	for pos, want := range map[int]int{1: 3, 2: 9, 3: 0, 0: 0} {
		if got := documentStartLine(file, pos); got != want {
			t.Errorf("documentStartLine(%d) = %d, want %d", pos, got, want)
		}
	}
}

// "{}" and comment-only documents are not loader positions.
func TestDocumentStartLineSkipsEmptyDocuments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.yaml")
	content := "{}\n---\n# only a comment\n---\n" + twoDocs
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for pos, want := range map[int]int{1: 7, 2: 13, 3: 0} {
		if got := documentStartLine(file, pos); got != want {
			t.Errorf("documentStartLine(%d) = %d, want %d", pos, got, want)
		}
	}
}

func TestLogErrorJSONPointsAtInnermostDocument(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(file, []byte(twoDocs), 0o600); err != nil {
		t.Fatal(err)
	}
	dataset := config.WithSource(config.Document{File: file, Position: 2}, errors.New("dataset second: query: boom"))
	artefact := config.WithSource(config.Document{File: file, Position: 1}, fmt.Errorf("artefact r: %w", dataset))

	records := errorRecords(t, RuntimeError(artefact))
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	rec := records[0]
	if rec["level"] != "error" || rec["file"] != file || rec["line"] != float64(9) || rec["kind"] != "runtime" {
		t.Errorf("record = %v, want the second document at line 9", rec)
	}
	if rec["msg"] != "artefact r: dataset second: query: boom" {
		t.Errorf("msg = %q", rec["msg"])
	}
}

func TestLogErrorJSONSchemaErrors(t *testing.T) {
	err := ConfigError(&spec.SchemaValidationError{
		File:        "/p/report.yaml",
		DocPosition: 1,
		Errors: []spec.SchemaError{
			{Field: "spec.bogus", Description: "additional property 'bogus' not allowed", Line: 22, Column: 3},
			{Field: "spec.other", Description: "is required", Line: 24, Column: 5},
		},
	})
	records := errorRecords(t, err)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0]["msg"] != "spec.bogus: additional property 'bogus' not allowed" ||
		records[0]["line"] != float64(22) || records[0]["column"] != float64(3) || records[0]["file"] != "/p/report.yaml" {
		t.Errorf("first record = %v", records[0])
	}
	if records[1]["line"] != float64(24) {
		t.Errorf("second record = %v", records[1])
	}
}

func TestLogErrorJSONWithoutSource(t *testing.T) {
	rec := errorRecords(t, RuntimeErrorf("no artefacts"))[0]
	if _, ok := rec["file"]; ok {
		t.Errorf("record has a file without a source: %v", rec)
	}
	if rec["msg"] != "no artefacts" {
		t.Errorf("msg = %q", rec["msg"])
	}
}

// The wrapper must not change the text a terminal user sees.
func TestSourceErrorKeepsMessage(t *testing.T) {
	err := config.WithSource(config.Document{File: "x.yaml", Position: 1}, errors.New("dataset ds: boom"))
	if err.Error() != "dataset ds: boom" {
		t.Errorf("Error() = %q", err.Error())
	}
	if config.WithSource(config.Document{}, nil) != nil {
		t.Error("WithSource(nil) is not nil")
	}
}
