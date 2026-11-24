package spec

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestBuildNameIndex(t *testing.T) {
	files := map[string]string{
		"data.yaml": `kind: DataSource
metadata:
  name: sales_src
spec:
  type: csv
  path: sales.csv
---
kind: DataSet
metadata:
  name: sales
spec:
  source: sales_src
  dependencies:
    - sales_src
`,
		"report.yaml": `kind: Table
metadata:
  name: rev_table
spec:
  dataset: sales
---
kind: LayoutPage
metadata:
  name: page1
spec:
  children:
    - kind: Table
      ref: rev_table
---
kind: ReportArtefact
metadata:
  name: report
spec:
  layoutPages:
    - page1
`,
	}

	idx, err := BuildNameIndex(files)
	if err != nil {
		t.Fatalf("BuildNameIndex: %v", err)
	}

	// Definitions
	for _, tc := range []struct{ kind, name, file string }{
		{"DataSource", "sales_src", "data.yaml"},
		{"DataSet", "sales", "data.yaml"},
		{"Table", "rev_table", "report.yaml"},
		{"LayoutPage", "page1", "report.yaml"},
		{"ReportArtefact", "report", "report.yaml"},
	} {
		def, ok := idx.Definition(tc.kind, tc.name)
		if !ok {
			t.Errorf("Definition(%s, %s) not found", tc.kind, tc.name)
			continue
		}
		if def.File != tc.file {
			t.Errorf("Definition(%s, %s).File = %q, want %q", tc.kind, tc.name, def.File, tc.file)
		}
		if def.NameRange.StartLine == 0 {
			t.Errorf("Definition(%s, %s) has zero NameRange", tc.kind, tc.name)
		}
	}

	// References to sales_src: source + dependencies[0] = 2 sites.
	srcRefs := idx.References("DataSource", "sales_src")
	if len(srcRefs) != 2 {
		t.Errorf("References(DataSource, sales_src) = %d, want 2 (%+v)", len(srcRefs), srcRefs)
	}

	// References to sales (the DataSet) from rev_table's dataset field.
	dsRefs := idx.References("DataSet", "sales")
	if len(dsRefs) != 1 {
		t.Errorf("References(DataSet, sales) = %d, want 1", len(dsRefs))
	}

	// Layout child `ref: rev_table` resolves its target kind from the sibling kind.
	tblRefs := idx.References("Table", "rev_table")
	if len(tblRefs) != 1 {
		t.Fatalf("References(Table, rev_table) = %d, want 1", len(tblRefs))
	}
	if tblRefs[0].Field != "ref" || tblRefs[0].TargetKind != "Table" {
		t.Errorf("ref site = %+v, want Field=ref TargetKind=Table", tblRefs[0])
	}

	// ReportArtefact string-form layoutPages references page1.
	pageRefs := idx.References("LayoutPage", "page1")
	if len(pageRefs) != 1 {
		t.Errorf("References(LayoutPage, page1) = %d, want 1", len(pageRefs))
	}

	// documentSymbol surface: defs grouped by file.
	if got := len(idx.DefsInFile("report.yaml")); got != 3 {
		t.Errorf("DefsInFile(report.yaml) = %d, want 3", got)
	}
}

func TestBuildNameIndex_SelectedStyleRef(t *testing.T) {
	files := map[string]string{
		"style.yaml": `kind: ComponentStyle
metadata:
  name: highlighted
spec:
  backgroundColor: yellow
`,
		"report.yaml": `kind: Table
metadata:
  name: rev_table
spec:
  dataset: sales
  selectedStyle: highlighted
`,
	}
	idx, err := BuildNameIndex(files)
	if err != nil {
		t.Fatalf("BuildNameIndex: %v", err)
	}

	def, ok := idx.Definition("ComponentStyle", "highlighted")
	if !ok {
		t.Fatal("Definition(ComponentStyle, highlighted) not found")
	}
	if def.File != "style.yaml" {
		t.Errorf("Definition(ComponentStyle, highlighted).File = %q, want style.yaml", def.File)
	}

	refs := idx.References("ComponentStyle", "highlighted")
	if len(refs) != 1 {
		t.Fatalf("References(ComponentStyle, highlighted) = %d, want 1", len(refs))
	}
	if refs[0].Field != "selectedStyle" || refs[0].File != "report.yaml" {
		t.Errorf("ref site = %+v, want Field=selectedStyle File=report.yaml", refs[0])
	}
}

func TestBuildNameIndex_DollarPrefixDataSource(t *testing.T) {
	files := map[string]string{
		"r.yaml": `kind: ChartTime
metadata:
  name: c
spec:
  dataset: $sales_src
`,
	}
	idx, _ := BuildNameIndex(files)
	// A $-prefixed dataset reference targets a DataSource; the $ is stripped.
	refs := idx.References("DataSource", "sales_src")
	if len(refs) != 1 {
		t.Fatalf("References(DataSource, sales_src) = %d, want 1", len(refs))
	}
	if refs[0].TargetName != "sales_src" {
		t.Errorf("TargetName = %q, want sales_src (stripped $)", refs[0].TargetName)
	}
}

// canonical renders an index in a map-order independent form.
func canonical(idx *NameIndex) string {
	var lines []string
	for k, d := range idx.defs {
		lines = append(lines, fmt.Sprintf("def %q %+v", k, d))
	}
	for name, defs := range idx.byName {
		for _, d := range defs {
			lines = append(lines, fmt.Sprintf("byName %s %+v", name, d))
		}
	}
	for _, r := range idx.refs {
		lines = append(lines, fmt.Sprintf("ref %+v", r))
	}
	for _, q := range idx.queries {
		lines = append(lines, fmt.Sprintf("query %s %s %d:%d", q.file, q.field, q.node.Line, q.node.Column))
	}
	for f, c := range idx.files {
		lines = append(lines, fmt.Sprintf("file %s %q", f, c))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestNameIndexWithFileMatchesRebuild(t *testing.T) {
	files := map[string]string{
		"a.yaml": "kind: DataSet\nmetadata:\n  name: sales\nspec:\n  query: SELECT 1\n  dependencies: [src]\n",
		"b.yaml": "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n",
		"c.yaml": "kind: DataSource\nmetadata:\n  name: src\nspec:\n  type: csv\n",
	}
	full, _ := BuildNameIndex(files)
	edited := "kind: DataSet\nmetadata:\n  name: revenue\nspec:\n  prql: from src\n---\nkind: Table\nmetadata:\n  name: t2\nspec:\n  dataset: revenue\n"
	incremental := full.WithFile("a.yaml", edited, true)

	next := map[string]string{"a.yaml": edited, "b.yaml": files["b.yaml"], "c.yaml": files["c.yaml"]}
	rebuilt, _ := BuildNameIndex(next)
	if got, want := canonical(incremental), canonical(rebuilt); got != want {
		t.Fatalf("incremental index differs from a rebuild\n got:\n%s\nwant:\n%s", got, want)
	}
	if _, ok := full.Definition("DataSet", "sales"); !ok {
		t.Error("WithFile must not change the receiver")
	}

	dropped := incremental.WithFile("c.yaml", "", false)
	delete(next, "c.yaml")
	rebuilt, _ = BuildNameIndex(next)
	if got, want := canonical(dropped), canonical(rebuilt); got != want {
		t.Fatalf("dropping a file differs from a rebuild\n got:\n%s\nwant:\n%s", got, want)
	}
}
