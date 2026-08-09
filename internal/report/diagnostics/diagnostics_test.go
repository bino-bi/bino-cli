package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// TestFromLoadError_TypedDocumentError: the typed *config.DocumentError path
// must position diagnostics from the struct fields alone. The read and
// decode cases are undecodable by the legacy string parsers (no " document "
// or ": yaml: " marker), so a File on the result proves the typed path ran.
func TestFromLoadError_TypedDocumentError(t *testing.T) {
	cases := []struct {
		name         string
		err          *config.DocumentError
		wantFile     string
		wantPosition int
	}{
		{
			name:         "header failure carries file and document position",
			err:          &config.DocumentError{Op: "header", File: "x.yaml", Position: 3, Err: errors.New("boom")},
			wantFile:     "x.yaml",
			wantPosition: 3,
		},
		{
			name:     "read failure is positioned (legacy parser could not)",
			err:      &config.DocumentError{Op: "read", File: "/p/report.yaml", Err: errors.New("permission denied")},
			wantFile: "/p/report.yaml",
		},
		{
			name:     "non-yaml decode failure is positioned (legacy parser could not)",
			err:      &config.DocumentError{Op: "decode", File: "/p/report.yaml", Err: errors.New("stream too large")},
			wantFile: "/p/report.yaml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := FromLoadError(tc.err)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
			}
			d := diags[0]
			if d.File != tc.wantFile {
				t.Errorf("File = %q, want %q", d.File, tc.wantFile)
			}
			if d.Position != tc.wantPosition {
				t.Errorf("Position = %d, want %d", d.Position, tc.wantPosition)
			}
			if d.Code != "validation-error" {
				t.Errorf("Code = %q, want validation-error", d.Code)
			}
			if d.Severity != "error" {
				t.Errorf("Severity = %q, want error", d.Severity)
			}
		})
	}
}

// TestFromLoadError_TypedDecodeYAMLSyntax: a typed decode failure wrapping a
// yaml.v3 error becomes a positioned yaml-syntax diagnostic; only the line
// number is parsed out of yaml's own message, the file comes from the struct.
func TestFromLoadError_TypedDecodeYAMLSyntax(t *testing.T) {
	cases := []struct {
		name     string
		inner    string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "single error with line",
			inner:    "yaml: line 7: mapping values are not allowed in this context",
			wantLine: 7,
			wantMsg:  "YAML syntax error: mapping values are not allowed in this context",
		},
		{
			name:     "no line stays unpositioned",
			inner:    "yaml: did not find expected node content",
			wantLine: 0,
			wantMsg:  "YAML syntax error: did not find expected node content",
		},
		{
			name:     "type error keeps full message, anchors first line",
			inner:    "yaml: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
			wantLine: 3,
			wantMsg:  "YAML syntax error: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &config.DocumentError{Op: "decode", File: "/p/f.yaml", Err: errors.New(tc.inner)}
			diags := FromLoadError(err)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
			}
			d := diags[0]
			if d.Code != "yaml-syntax" {
				t.Fatalf("Code = %q, want yaml-syntax", d.Code)
			}
			if d.File != "/p/f.yaml" {
				t.Errorf("File = %q, want /p/f.yaml", d.File)
			}
			if d.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", d.Line, tc.wantLine)
			}
			if d.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", d.Message, tc.wantMsg)
			}
		})
	}
}

// TestParseYAMLSyntaxError covers the legacy string fallback (moved here from
// internal/daemon): stringified decode failures that lost their type must
// still be destructured into positioned yaml-syntax diagnostics.
func TestParseYAMLSyntaxError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantOK   bool
		wantFile string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "decode with line",
			in:       "decode /tmp/x/draft.yaml: yaml: line 4: mapping values are not allowed in this context",
			wantOK:   true,
			wantFile: "/tmp/x/draft.yaml",
			wantLine: 4,
			wantMsg:  "YAML syntax error: mapping values are not allowed in this context",
		},
		{
			name:     "decode without line",
			in:       "decode /tmp/x/draft.yaml: yaml: did not find expected node content",
			wantOK:   true,
			wantFile: "/tmp/x/draft.yaml",
			wantLine: 0,
			wantMsg:  "YAML syntax error: did not find expected node content",
		},
		{
			name:     "windows drive colon stays in the path",
			in:       `decode C:\proj\report.yaml: yaml: line 2: found character that cannot start any token`,
			wantOK:   true,
			wantFile: `C:\proj\report.yaml`,
			wantLine: 2,
			wantMsg:  "YAML syntax error: found character that cannot start any token",
		},
		{
			name:     "type error keeps full message, anchors first line",
			in:       "decode /p/f.yaml: yaml: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
			wantOK:   true,
			wantFile: "/p/f.yaml",
			wantLine: 3,
			wantMsg:  "YAML syntax error: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
		},
		{
			name:   "not a decode error",
			in:     "validate /p/f.yaml document 2: boom",
			wantOK: false,
		},
		{
			name:   "decode without yaml marker",
			in:     "decode /p/f.yaml: permission denied",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := parseYAMLSyntaxError(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if d.File != tc.wantFile {
				t.Errorf("File = %q, want %q", d.File, tc.wantFile)
			}
			if d.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", d.Line, tc.wantLine)
			}
			if d.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", d.Message, tc.wantMsg)
			}
			if d.Code != "yaml-syntax" {
				t.Errorf("Code = %q, want yaml-syntax", d.Code)
			}
		})
	}
}

// TestCollect_YAMLSyntaxPositioned: a syntax-broken manifest in a real bundle
// must surface as a yaml-syntax diagnostic pointing at the file and line.
func TestCollect_YAMLSyntaxPositioned(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(broken, []byte("kind: Table\nspec: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := Collect(context.Background(), dir, Options{SkipForeign: true})

	var found bool
	for _, d := range diags {
		if d.Code != "yaml-syntax" {
			continue
		}
		found = true
		if d.File != broken {
			t.Errorf("File = %q, want %q", d.File, broken)
		}
		if d.Line <= 0 {
			t.Errorf("Line = %d, want a positioned syntax error", d.Line)
		}
		if !strings.HasPrefix(d.Message, "YAML syntax error: ") {
			t.Errorf("Message = %q, want YAML syntax error prefix", d.Message)
		}
	}
	if !found {
		t.Fatalf("expected a yaml-syntax diagnostic, got %+v", diags)
	}
}

// TestCollect_NeverNil: a bundle with nothing to report must yield an empty,
// non-nil slice — callers serialize it directly and [] vs null is contract.
func TestCollect_NeverNil(t *testing.T) {
	dir := t.TempDir()
	report := `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  filename: out.pdf
  title: Sample
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
`
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := Collect(context.Background(), dir, Options{SkipForeign: true})
	if diags == nil {
		t.Fatal("Collect returned nil, want empty slice")
	}
	if len(diags) != 0 {
		t.Fatalf("Collect on a clean project returned %+v, want none", diags)
	}
}

// TestCollect_PreservesFindingSeverity: lint findings keep their per-rule
// severity (missing-required-reference is an error) instead of a hardcoded
// "warning".
func TestCollect_PreservesFindingSeverity(t *testing.T) {
	dir := t.TempDir()
	page := `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Report
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page_one
spec:
  children:
    - kind: Text
      spec:
        value: hello
    - kind: Text
      ref: ghost_text
`
	if err := os.WriteFile(filepath.Join(dir, "page.yaml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := Collect(context.Background(), dir, Options{SkipForeign: true})

	var found bool
	for _, d := range diags {
		if d.Code == "missing-required-reference" {
			found = true
			if d.Severity != "error" {
				t.Errorf("missing-required-reference severity = %q, want error", d.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected a missing-required-reference finding, got %+v", diags)
	}
}
