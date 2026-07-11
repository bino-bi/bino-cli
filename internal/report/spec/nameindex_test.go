package spec

import "testing"

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
