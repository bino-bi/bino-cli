package spec

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// mapGoldenSource is a two-document file whose first document carries head, line,
// and foot comments on its spec mapping. The second document is never touched.
const mapGoldenSource = `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t # the name
spec:
  # head on a
  a: 1 # line on a
  # head on b
  b: 2
  c: 3 # line on c
  # foot floating
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: keep
`

// seqGoldenSource is a two-document file whose first document carries head and
// line comments on a sequence. The second document is never touched.
const seqGoldenSource = `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - first # line first
    - second
    # head third
    - third # line third
---
kind: Text
spec:
  value: keep
`

// seqFootGoldenSource mirrors seqGoldenSource but adds a trailing block comment
// after the last element, which yaml.v3 attaches as that element's FootComment.
// It exercises the foot-hoist branch of removeSequenceElement (idx == last).
const seqFootGoldenSource = `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - first # line first
    - second
    # head third
    - third # line third
    # foot after third
---
kind: Text
spec:
  value: keep
`

func TestRemoveYAMLPathsGolden(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		paths   []string
		want    string
		wantErr bool
	}{
		{
			name:   "delete middle mapping key hoists its head comment to the next sibling",
			source: mapGoldenSource,
			paths:  []string{"spec.b"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t # the name
spec:
  # head on a
  a: 1 # line on a
  # head on b
  c: 3 # line on c
  # foot floating
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: keep
`,
		},
		{
			name:   "delete last mapping key hoists the trailing foot comment to the prior sibling",
			source: mapGoldenSource,
			paths:  []string{"spec.c"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t # the name
spec:
  # head on a
  a: 1 # line on a
  # head on b
  b: 2
  # foot floating
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: keep
`,
		},
		{
			name:   "delete middle sequence element keeps the survivors and their comments",
			source: seqGoldenSource,
			paths:  []string{"spec.columns[1]"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - first # line first
    # head third
    - third # line third
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name:   "delete last sequence element drops its own comments",
			source: seqGoldenSource,
			paths:  []string{"spec.columns[2]"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - first # line first
    - second
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name:   "delete last sequence element hoists its trailing foot comment to the prior element",
			source: seqFootGoldenSource,
			paths:  []string{"spec.columns[2]"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - first # line first
    - second
    # foot after third
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name:   "delete several elements of the same sequence removes the highest index first",
			source: seqGoldenSource,
			// [0] then [2] in a 3-element sequence: removing [0] first would
			// shift [2] to [1]; orderRemovals must splice the higher index first.
			// [2] (third) is last so its own head comment drops; [0] (first) is
			// then not last, so its head comment is hoisted onto the survivor.
			paths: []string{"spec.columns[0]", "spec.columns[2]"},
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head first
    - second
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name:    "missing key errors",
			source:  mapGoldenSource,
			paths:   []string{"spec.nope"},
			wantErr: true,
		},
		{
			name:    "sequence index out of range errors",
			source:  seqGoldenSource,
			paths:   []string{"spec.columns[9]"},
			wantErr: true,
		},
		{
			name:    "position out of range errors",
			source:  mapGoldenSource,
			paths:   []string{"spec.a"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := 1
			if tt.name == "position out of range errors" {
				pos = 9
			}
			full, edited, err := RemoveYAMLPaths(tt.source, pos, tt.paths)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got full:\n%s", full)
				}
				return
			}
			if err != nil {
				t.Fatalf("RemoveYAMLPaths: %v", err)
			}
			if full != tt.want {
				t.Errorf("full mismatch:\n--- got ---\n%s\n--- want ---\n%s", full, tt.want)
			}
			assertEvenMappings(t, edited)
		})
	}
}

func TestReorderYAMLSequenceGolden(t *testing.T) {
	tests := []struct {
		name             string
		from, to         int
		want             string
		wantErr          bool
		wantByteIdentity bool
	}{
		{
			name: "move the first element to the end carries its comments",
			from: 0,
			to:   2,
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    - second
    # head third
    - third # line third
    # head first
    - first # line first
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name: "move the last element to the front carries its comments",
			from: 2,
			to:   0,
			want: `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  columns:
    # head third
    - third # line third
    # head first
    - first # line first
    - second
---
kind: Text
spec:
  value: keep
`,
		},
		{
			name:             "no-op reorder re-encodes to byte-identical content",
			from:             1,
			to:               1,
			wantByteIdentity: true,
		},
		{
			name:    "from index out of range errors",
			from:    5,
			to:      0,
			wantErr: true,
		},
		{
			name:    "to index out of range errors",
			from:    0,
			to:      5,
			wantErr: true,
		},
	}

	// The canonical re-encoding of the source (a no-op reorder must equal it).
	canonical, _, err := ReorderYAMLSequence(seqGoldenSource, 1, "spec.columns", 0, 0)
	if err != nil {
		t.Fatalf("canonical re-encode: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			full, edited, err := ReorderYAMLSequence(seqGoldenSource, 1, "spec.columns", tt.from, tt.to)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got full:\n%s", full)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReorderYAMLSequence: %v", err)
			}
			want := tt.want
			if tt.wantByteIdentity {
				want = canonical
			}
			if full != want {
				t.Errorf("full mismatch:\n--- got ---\n%s\n--- want ---\n%s", full, want)
			}
			assertEvenMappings(t, edited)
		})
	}
}

func TestReorderYAMLSequenceNotASequence(t *testing.T) {
	if _, _, err := ReorderYAMLSequence(mapGoldenSource, 1, "spec", 0, 0); err == nil {
		t.Error("expected error reordering a mapping path")
	}
}

// assertEvenMappings fails if any mapping node in the document has an odd number
// of children, which would break the alternating key/value invariant that
// setMapValue and ResolvePathPosition rely on.
func assertEvenMappings(t *testing.T, content string) {
	t.Helper()
	nodes, err := ParseYAMLNodes(content)
	if err != nil {
		t.Fatalf("re-parse edited doc: %v", err)
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode && len(n.Content)%2 != 0 {
			t.Errorf("mapping node has odd Content length %d", len(n.Content))
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
}
