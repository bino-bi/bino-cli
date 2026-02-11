package spec

import (
	"strings"
	"testing"
)

func TestParseYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int // number of nodes
		wantErr bool
	}{
		{
			name:  "single document",
			input: "kind: DataSource\nmetadata:\n  name: test\n",
			want:  1,
		},
		{
			name:  "multi document",
			input: "kind: DataSource\n---\nkind: DataSet\n---\nkind: LayoutPage\n",
			want:  3,
		},
		{
			name:  "empty documents become nodes",
			input: "kind: DataSource\n---\n---\nkind: DataSet\n",
			want:  3,
		},
		{
			name:  "empty input",
			input: "",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := ParseYAMLNodes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(nodes) != tt.want {
				t.Errorf("ParseYAMLNodes() got %d nodes, want %d", len(nodes), tt.want)
			}
		})
	}
}

func TestResolvePathPosition(t *testing.T) {
	content := `kind: DataSource
apiVersion: bino.bi/v1
metadata:
  name: test
spec:
  type: csv
  path: data.csv
  columns:
    - name: id
    - name: value`

	nodes, err := ParseYAMLNodes(content)
	if err != nil {
		t.Fatalf("ParseYAMLNodes() error = %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	root := nodes[0]

	tests := []struct {
		name     string
		path     string
		wantLine int
		wantOK   bool
	}{
		{
			name:     "root path",
			path:     "(root)",
			wantLine: 1,
			wantOK:   true,
		},
		{
			name:     "empty path",
			path:     "",
			wantLine: 1,
			wantOK:   true,
		},
		{
			name:     "top-level field",
			path:     "kind",
			wantLine: 1,
			wantOK:   true,
		},
		{
			name:     "nested field",
			path:     "metadata.name",
			wantLine: 4,
			wantOK:   true,
		},
		{
			name:     "spec type",
			path:     "spec.type",
			wantLine: 6,
			wantOK:   true,
		},
		{
			name:     "missing field resolves to parent key",
			path:     "spec.cells",
			wantLine: 5, // resolves to the "spec" key node where "cells" should be added
			wantOK:   true,
		},
		{
			name:     "array index",
			path:     "spec.columns.1",
			wantLine: 10,
			wantOK:   true,
		},
		{
			name:     "deeply missing path",
			path:     "nonexistent.deep.path",
			wantLine: 1, // resolves to root
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, _, ok := ResolvePathPosition(root, tt.path)
			if ok != tt.wantOK {
				t.Errorf("ResolvePathPosition() ok = %v, want %v", ok, tt.wantOK)
			}
			if line != tt.wantLine {
				t.Errorf("ResolvePathPosition() line = %d, want %d", line, tt.wantLine)
			}
		})
	}

	t.Run("nil node", func(t *testing.T) {
		_, _, ok := ResolvePathPosition(nil, "spec")
		if ok {
			t.Error("ResolvePathPosition(nil, ...) should return ok=false")
		}
	})
}

func TestExtractSourceSnippet(t *testing.T) {
	source := "line1\nline2\nline3\nline4\nline5\nline6\nline7"

	tests := []struct {
		name         string
		line         int
		contextLines int
		wantLines    int // number of output lines
		wantContains []string
	}{
		{
			name:         "middle line with context",
			line:         4,
			contextLines: 2,
			wantLines:    5,
			wantContains: []string{"line2", "line3", "line4", "line5", "line6"},
		},
		{
			name:         "first line",
			line:         1,
			contextLines: 2,
			wantLines:    3,
			wantContains: []string{"line1", "line2", "line3"},
		},
		{
			name:         "last line",
			line:         7,
			contextLines: 2,
			wantLines:    3,
			wantContains: []string{"line5", "line6", "line7"},
		},
		{
			name:         "zero context",
			line:         4,
			contextLines: 0,
			wantLines:    1,
			wantContains: []string{"line4"},
		},
		{
			name:         "empty source",
			line:         1,
			contextLines: 2,
			wantLines:    0,
		},
		{
			name:         "line out of range",
			line:         100,
			contextLines: 2,
			wantLines:    0,
		},
		{
			name:         "zero line",
			line:         0,
			contextLines: 2,
			wantLines:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := source
			if tt.name == "empty source" {
				src = ""
			}

			result := ExtractSourceSnippet(src, tt.line, tt.contextLines)

			if tt.wantLines == 0 {
				if result != "" {
					t.Errorf("expected empty result, got %q", result)
				}
				return
			}

			resultLines := strings.Split(strings.TrimRight(result, "\n"), "\n")
			if len(resultLines) != tt.wantLines {
				t.Errorf("got %d lines, want %d\nresult:\n%s", len(resultLines), tt.wantLines, result)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("result does not contain %q\nresult:\n%s", want, result)
				}
			}

			// Verify line numbers are present
			if !strings.Contains(result, "│") {
				t.Error("result does not contain line number separator '│'")
			}
		})
	}
}
