package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoldenCases edit one document of a testdata file. The golden output
// pins that every byte outside the edited document is unchanged.
var writeGoldenCases = []struct {
	name  string
	input string
	write func(content string) (string, string, error)
}{
	{"blank-lines-add", "blank-lines", func(c string) (string, string, error) {
		return EditYAMLDocument(c, 2, map[string]any{"spec.subtitle": "New"})
	}},
	{"comments-add-first", "comments", func(c string) (string, string, error) {
		return EditYAMLDocument(c, 1, map[string]any{"spec.delimiter": ";"})
	}},
	{"comments-edit-second", "comments", func(c string) (string, string, error) {
		return EditYAMLDocument(c, 2, map[string]any{"metadata.name": "renamed"})
	}},
	{"indent4-append", "indent4", func(c string) (string, string, error) {
		return AppendYAMLSequence(c, 2, "spec.columns", map[string]any{"name": "margin"})
	}},
	{"multi-remove", "multi-remove", func(c string) (string, string, error) {
		return RemoveYAMLPaths(c, 2, []string{"spec.title"})
	}},
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "yamlwrite", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteKeepsFileFormatGolden(t *testing.T) {
	for _, tc := range writeGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			full, _, err := tc.write(readTestdata(t, tc.input+".in.yaml"))
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			if want := readTestdata(t, tc.name+".want.yaml"); full != want {
				t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", full, want)
			}
		})
	}
}

// TestWriteKeepsCRLF runs the golden cases on CRLF copies of the inputs: the
// output must be the CRLF form of the LF golden.
func TestWriteKeepsCRLF(t *testing.T) {
	for _, tc := range writeGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.ReplaceAll(readTestdata(t, tc.input+".in.yaml"), "\n", "\r\n")
			full, _, err := tc.write(in)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			want := strings.ReplaceAll(readTestdata(t, tc.name+".want.yaml"), "\n", "\r\n")
			if full != want {
				t.Errorf("output mismatch\n--- got ---\n%q\n--- want ---\n%q", full, want)
			}
		})
	}
}

// TestWriteNoTrailingNewline keeps a missing final line break missing.
func TestWriteNoTrailingNewline(t *testing.T) {
	in := "kind: Text\nspec:\n  value: a\n---\nkind: Text\nspec:\n  value: b"
	full, edited, err := EditYAMLDocument(in, 2, map[string]any{"spec.value": "c"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "kind: Text\nspec:\n  value: a\n---\nkind: Text\nspec:\n  value: c"; full != want {
		t.Errorf("full = %q, want %q", full, want)
	}
	if want := "kind: Text\nspec:\n  value: c\n"; edited != want {
		t.Errorf("edited = %q, want %q", edited, want)
	}
}

// TestWriteFallsBackOnUnsplittableShape keeps the old whole-file re-encode for
// shapes the range splitter rejects, such as content on the "---" line.
func TestWriteFallsBackOnUnsplittableShape(t *testing.T) {
	in := "--- {kind: Text}\n---\nkind: Text\nspec:\n  value: b\n"
	full, _, err := EditYAMLDocument(in, 2, map[string]any{"spec.value": "c"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{kind: Text}\n---\nkind: Text\nspec:\n  value: c\n"; full != want {
		t.Errorf("full = %q, want %q", full, want)
	}
}
