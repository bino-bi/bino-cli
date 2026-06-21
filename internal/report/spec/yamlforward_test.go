package spec

import "testing"

// table is the fixture body shared by the resolver cases. Cursor coordinates are
// 1-based (matching yaml.v3 / the resolver contract).
func TestResolvePositionPath(t *testing.T) {
	const table = `kind: Table
apiVersion: bino.bi/v1
metadata:
  name: rev_table
spec:
  dataset: sales
  scenarios:
    - ac1
    - pp1
  variances:
    - dac1_pp1_pos
  title: Revenue
`
	tests := []struct {
		name          string
		content       string
		line, col     int
		wantKind      PositionKind
		wantPath      string
		wantEnclosing string
		wantRefKind   string
		wantPrefix    string
		wantBound     []string
	}{
		{
			name: "kind value", content: table, line: 1, col: 7,
			wantKind: PosKindValue, wantPath: "kind", wantEnclosing: "Table", wantPrefix: "Table",
		},
		{
			name: "dataset reference value", content: table, line: 6, col: 12,
			wantKind: PosDatasetRef, wantPath: "spec.dataset", wantEnclosing: "Table",
			wantRefKind: "DataSet", wantPrefix: "sales",
		},
		{
			name: "scenario item existing", content: table, line: 8, col: 8,
			wantKind: PosScenarioItem, wantEnclosing: "Table", wantPrefix: "ac1",
			wantBound: []string{"sales"},
		},
		{
			name: "variance item existing", content: table, line: 11, col: 9,
			wantKind: PosVarianceItem, wantEnclosing: "Table", wantPrefix: "dac1_pp1_pos",
			wantBound: []string{"sales"},
		},
		{
			name: "new key under spec (blank trailing line)", content: table, line: 13, col: 3,
			wantKind: PosKey, wantEnclosing: "Table",
		},
		{
			name: "free value (title)", content: table, line: 12, col: 12,
			wantKind: PosFreeValue, wantPath: "spec.title", wantEnclosing: "Table", wantPrefix: "Revenue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, ok := ResolvePositionPath(tt.content, tt.line, tt.col)
			if !ok {
				t.Fatalf("ResolvePositionPath() ok=false, want a context")
			}
			if ctx.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", ctx.Kind, tt.wantKind)
			}
			if tt.wantPath != "" && ctx.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", ctx.Path, tt.wantPath)
			}
			if tt.wantEnclosing != "" && ctx.EnclosingKind != tt.wantEnclosing {
				t.Errorf("EnclosingKind = %q, want %q", ctx.EnclosingKind, tt.wantEnclosing)
			}
			if tt.wantRefKind != "" && ctx.RefKind != tt.wantRefKind {
				t.Errorf("RefKind = %q, want %q", ctx.RefKind, tt.wantRefKind)
			}
			if tt.wantPrefix != "" && ctx.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %q, want %q", ctx.Prefix, tt.wantPrefix)
			}
			if tt.wantBound != nil {
				if len(ctx.BoundDatasets) != len(tt.wantBound) || (len(tt.wantBound) > 0 && ctx.BoundDatasets[0] != tt.wantBound[0]) {
					t.Errorf("BoundDatasets = %v, want %v", ctx.BoundDatasets, tt.wantBound)
				}
			}
		})
	}
}

func TestResolvePositionPath_NewScenarioItem(t *testing.T) {
	// A fresh "- " slot below the last scenario should classify as a scenario item
	// with an empty prefix (the most valuable completion case).
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  scenarios:
    - ac1
    -
`
	ctx, ok := ResolvePositionPath(doc, 8, 7)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosScenarioItem {
		t.Fatalf("Kind = %v, want PosScenarioItem", ctx.Kind)
	}
	if len(ctx.BoundDatasets) != 1 || ctx.BoundDatasets[0] != "sales" {
		t.Errorf("BoundDatasets = %v, want [sales]", ctx.BoundDatasets)
	}
}

func TestResolvePositionPath_MultiDoc(t *testing.T) {
	const content = `kind: DataSource
metadata:
  name: src
spec:
  type: csv
---
kind: DataSet
metadata:
  name: ds
spec:
  source: src
`
	// Cursor on the `source:` value in the SECOND document.
	ctx, ok := ResolvePositionPath(content, 11, 11)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.DocIndex != 1 {
		t.Errorf("DocIndex = %d, want 1", ctx.DocIndex)
	}
	if ctx.EnclosingKind != "DataSet" {
		t.Errorf("EnclosingKind = %q, want DataSet", ctx.EnclosingKind)
	}
	if ctx.Kind != PosDatasetRef || ctx.RefKind != "DataSource" {
		t.Errorf("Kind/RefKind = %v/%q, want PosDatasetRef/DataSource", ctx.Kind, ctx.RefKind)
	}
}

func TestResolvePositionPath_EnvVarLine(t *testing.T) {
	// The resolver parses the RAW buffer; a ${VAR} line must not crash and offsets
	// stay raw-relative.
	const doc = `kind: DataSource
metadata:
  name: src
spec:
  type: csv
  path: ${DATA_DIR}/sales.csv
`
	ctx, ok := ResolvePositionPath(doc, 6, 9)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Path != "spec.path" {
		t.Errorf("Path = %q, want spec.path", ctx.Path)
	}
	if ctx.Prefix != "${DATA_DIR}/sales.csv" {
		t.Errorf("Prefix = %q, want the raw ${VAR} value", ctx.Prefix)
	}
}

func TestResolvePositionPath_BlockScalar(t *testing.T) {
	const doc = `kind: DataSet
metadata:
  name: ds
spec:
  query: |
    SELECT region, amount
    FROM sales
`
	ctx, ok := ResolvePositionPath(doc, 6, 12)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosQueryScalar {
		t.Errorf("Kind = %v, want PosQueryScalar", ctx.Kind)
	}
	if ctx.FieldName != "query" {
		t.Errorf("FieldName = %q, want query", ctx.FieldName)
	}
}

func TestResolvePositionPath_OutOfBounds(t *testing.T) {
	const doc = "kind: Table\nmetadata:\n  name: t\n"
	if _, ok := ResolvePositionPath("", 1, 1); ok {
		t.Error("empty content should yield ok=false")
	}
	if _, ok := ResolvePositionPath("\t bad: : :\n", 1, 1); ok {
		// malformed YAML → no nodes → ok=false
		t.Error("unparseable content should yield ok=false")
	}
	// A cursor far past EOF still resolves into the single document (clamped),
	// which is acceptable; assert it does not panic.
	_, _ = ResolvePositionPath(doc, 99, 1)
}
