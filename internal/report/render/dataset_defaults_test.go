package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// defaultsResult builds a dataset result whose rows carry the given `_spec_`
// cells on every row, folded the way the executor folds them.
func defaultsResult(t *testing.T, name string, cells map[string]any) dataset.Result {
	t.Helper()
	row := map[string]any{"category": "A", "categoryIndex": 1, "ac1": 10}
	for k, v := range cells {
		row[k] = v
	}
	data, err := json.Marshal([]map[string]any{row, row})
	if err != nil {
		t.Fatal(err)
	}
	defaults, warnings := dataset.FoldDefaults(name, data)
	if len(warnings) != 0 {
		t.Fatalf("fold warnings: %v", warnings)
	}
	return dataset.Result{Name: name, Data: data, Defaults: defaults}
}

// renderWithDefaults renders docs with dataset results and returns the HTML
// and the render warnings.
func renderWithDefaults(t *testing.T, docs []config.Document, results []dataset.Result, rootComponent string) (string, []string) {
	t.Helper()
	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, results, "en", "", "", ModePreview, nil, nil, "v1.0.0", nil, nil, rootComponent, "", "")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocumentsWithDatasets failed: %v", err)
	}
	return string(result.HTML), result.Warnings
}

func tablePage(tableSpec string) config.Document {
	return pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": ` + tableSpec + `}]}`)
}

func TestDatasetDefaults_TablePrecedence(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{"_spec_table_barColumns": "ac1,pl1"})

	tests := []struct {
		name string
		spec string
		want string // substring the <bn-table> tag must contain
		not  string // substring it must not contain
	}{
		{"unset field comes from the rows", `{"dataset": "sales"}`, `bar-columns='ac1,pl1'`, ""},
		{"YAML list wins", `{"dataset": "sales", "barColumns": ["pp1"]}`, `bar-columns='pp1'`, "ac1,pl1"},
		{"YAML string wins", `{"dataset": "sales", "barColumns": "pp1"}`, `bar-columns='pp1'`, "ac1,pl1"},
		{"empty list written by the author stays empty", `{"dataset": "sales", "barColumns": []}`, "", "bar-columns"},
		{"explicit null is unset", `{"dataset": "sales", "barColumns": null}`, `bar-columns='ac1,pl1'`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, warnings := renderWithDefaults(t, []config.Document{tablePage(tt.spec)}, []dataset.Result{sales}, "")
			tag := openTag(t, html, "bn-table")
			if tt.want != "" && !strings.Contains(tag, tt.want) {
				t.Errorf("want %q in %s", tt.want, tag)
			}
			if tt.not != "" && strings.Contains(tag, tt.not) {
				t.Errorf("do not want %q in %s", tt.not, tag)
			}
			if len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
		})
	}
}

func TestDatasetDefaults_InheritedStyleBeatsDataset(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{"_spec_table_selectedStyle": "fromdata"})
	page := pageDoc(`{"selectedStyle": "corp", "children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}}]}`)
	html, _ := renderWithDefaults(t, []config.Document{page}, []dataset.Result{sales}, "")
	if tag := openTag(t, html, "bn-table"); !strings.Contains(tag, `selected-style='corp'`) {
		t.Fatalf("inherited page style must beat the dataset default: %s", tag)
	}

	// Without an inherited style the dataset default fills the field.
	html, _ = renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "sales"}`)}, []dataset.Result{sales}, "")
	if tag := openTag(t, html, "bn-table"); !strings.Contains(tag, `selected-style='fromdata'`) {
		t.Fatalf("dataset default should fill the unset style: %s", tag)
	}
}

func TestDatasetDefaults_AnyToken(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{
		"_spec_any_scenarios":          "ac1,pl1",
		"_spec_table_scenarios":        "ac1",
		"_spec_any_barColumns":         "pl1",
		"_spec_any_measureUnit":        "kEUR",
		"_spec_charttime_dateInterval": "MONTH",
	})
	page := pageDoc(`{"children": [
		{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}},
		{"kind": "ChartStructure", "metadata": {"name": "c"}, "spec": {"dataset": "sales"}},
		{"kind": "Text", "metadata": {"name": "x"}, "spec": {"dataset": "sales", "value": "hi"}}
	]}`)
	html, warnings := renderWithDefaults(t, []config.Document{page}, []dataset.Result{sales}, "")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	table := openTag(t, html, "bn-table")
	if !strings.Contains(table, `scenarios='ac1'`) {
		t.Errorf("kind-specific must beat any: %s", table)
	}
	if !strings.Contains(table, `bar-columns='pl1'`) || !strings.Contains(table, `measure-unit='kEUR'`) {
		t.Errorf("any fields missing on table: %s", table)
	}
	chart := openTag(t, html, "bn-chart-structure")
	if !strings.Contains(chart, `scenarios='ac1,pl1'`) || !strings.Contains(chart, `measure-unit='kEUR'`) {
		t.Errorf("any fields missing on chart: %s", chart)
	}
	if strings.Contains(chart, "bar-columns") || strings.Contains(chart, "date-interval") {
		t.Errorf("chart must not receive fields it lacks or another kind's defaults: %s", chart)
	}
	if text := openTag(t, html, "bn-text"); strings.Contains(text, "scenarios") || strings.Contains(text, "kEUR") {
		t.Errorf("text has none of these fields: %s", text)
	}
}

// The Table scaling fields take "auto" and ScalingGroup names from data. An
// `any` scaling default is typed by the charts as a string, so the Table must
// accept a string there too.
func TestDatasetDefaults_TableScaling(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{
		"_spec_table_unitScaling":     "auto",
		"_spec_any_percentageScaling": "sg_pct",
	})
	html, warnings := renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "sales"}`)}, []dataset.Result{sales}, "")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	table := openTag(t, html, "bn-table")
	if !strings.Contains(table, `unit-scaling='auto'`) || !strings.Contains(table, `percentage-scaling='sg_pct'`) {
		t.Errorf("scaling defaults missing on table: %s", table)
	}
}

func TestDatasetDefaults_NonPrimaryDatasetWarns(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{"_spec_table_barColumns": "ac1"})
	other := defaultsResult(t, "other", map[string]any{"_spec_table_measureUnit": "kEUR"})
	page := tablePage(`{"dataset": ["sales", "other"]}`)
	html, warnings := renderWithDefaults(t, []config.Document{page}, []dataset.Result{sales, other}, "")
	tag := openTag(t, html, "bn-table")
	if !strings.Contains(tag, `bar-columns='ac1'`) || strings.Contains(tag, "measure-unit") {
		t.Errorf("only the primary dataset's defaults apply: %s", tag)
	}
	want := `Table "t": dataset "other" carries _spec_ columns but is not its primary dataset; ignored`
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %v, want [%s]", warnings, want)
	}
}

func TestDatasetDefaults_DatasourceBindingGetsNone(t *testing.T) {
	src := defaultsResult(t, "raw", map[string]any{"_spec_table_barColumns": "ac1"})
	html, warnings := renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "$raw"}`)}, []dataset.Result{src}, "")
	if tag := openTag(t, html, "bn-table"); strings.Contains(tag, "bar-columns") {
		t.Errorf("direct datasource binding must not get defaults: %s", tag)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestDatasetDefaults_AllSites(t *testing.T) {
	sales := defaultsResult(t, "sales", map[string]any{"_spec_table_barColumns": "ac1,pl1"})
	tableDoc := makeTestDoc("Table", "shared", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "Table", "metadata": {"name": "shared"},
		"spec": {"dataset": "sales", "scenarios": ["ac1"]}
	}`))

	tests := []struct {
		name  string
		docs  []config.Document
		root  string
		count int
	}{
		{
			name: "card child",
			docs: []config.Document{pageDoc(`{"children": [{"kind": "LayoutCard", "metadata": {"name": "card"}, "spec": {"children": [
				{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}}]}}]}`)},
			count: 1,
		},
		{
			name:  "ref child",
			docs:  []config.Document{tableDoc, pageDoc(`{"children": [{"kind": "Table", "ref": "shared"}]}`)},
			count: 1,
		},
		{
			name: "tree node",
			docs: []config.Document{pageDoc(`{"children": [{"kind": "Tree", "metadata": {"name": "tree"}, "spec": {"nodes": [
				{"id": "n1", "kind": "Table", "spec": {"dataset": "sales"}}]}}]}`)},
			count: 1,
		},
		{
			name: "grid child",
			docs: []config.Document{pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {"rows": 1, "columns": 1, "children": [
				{"row": 1, "column": 1, "kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}}]}}]}`)},
			count: 1,
		},
		{
			name:  "standalone component",
			docs:  []config.Document{tableDoc},
			root:  "shared",
			count: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, warnings := renderWithDefaults(t, tt.docs, []dataset.Result{sales}, tt.root)
			if got := strings.Count(html, `bar-columns='ac1,pl1'`); got != tt.count {
				t.Errorf("bar-columns occurrences = %d, want %d:\n%s", got, tt.count, html)
			}
			if len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
		})
	}
}

// dimResult builds a dataset result from explicit rows, folded like the executor does.
func dimResult(t *testing.T, name string, rows []map[string]any) dataset.Result {
	t.Helper()
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	defaults, warnings := dataset.FoldDefaults(name, data)
	if len(warnings) != 0 {
		t.Fatalf("fold warnings: %v", warnings)
	}
	return dataset.Result{Name: name, Data: data, Defaults: defaults}
}

func TestDatasetDefaults_ObjectFields(t *testing.T) {
	sales := dimResult(t, "sales", []map[string]any{
		{"rowGroup": "Revenue", "category": "Applications", "subCategory": "Applications-A", "ac1": 120, "_spec_table_thereof": "category"},
		{"rowGroup": "Revenue", "category": "Applications", "subCategory": "Applications-B", "ac1": 80, "_spec_table_thereof": "category"},
		{"rowGroup": "Revenue", "category": "Services", "subCategory": "Consulting", "ac1": 40},
	})
	want := `thereof='[{&#34;rowGroup&#34;:&#34;Revenue&#34;,&#34;category&#34;:&#34;Applications&#34;}]'`

	t.Run("marker fills an unset thereof", func(t *testing.T) {
		html, warnings := renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "sales"}`)}, []dataset.Result{sales}, "")
		if tag := openTag(t, html, "bn-table"); !strings.Contains(tag, want) {
			t.Errorf("want %s in %s", want, tag)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings: %v", warnings)
		}
	})
	t.Run("an empty list in YAML stays empty", func(t *testing.T) {
		html, _ := renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "sales", "thereof": []}`)}, []dataset.Result{sales}, "")
		if tag := openTag(t, html, "bn-table"); strings.Contains(tag, "thereof") {
			t.Errorf("author's empty list must win: %s", tag)
		}
	})
	t.Run("a YAML list wins", func(t *testing.T) {
		html, _ := renderWithDefaults(t, []config.Document{tablePage(`{"dataset": "sales", "thereof": [{"rowGroup": "Revenue", "category": "Services"}]}`)}, []dataset.Result{sales}, "")
		if tag := openTag(t, html, "bn-table"); !strings.Contains(tag, `&#34;category&#34;:&#34;Services&#34;`) || strings.Contains(tag, "Applications") {
			t.Errorf("author's list must win: %s", tag)
		}
	})
}

func TestDatasetDefaults_StackObjectMergesPerKey(t *testing.T) {
	sales := dimResult(t, "sales", []map[string]any{
		{"category": "A", "ac1": 1, "_spec_chartstructure_stack_by": "dimensions", "_spec_chartstructure_stack_mode": "relative"},
	})
	page := func(spec string) config.Document {
		return pageDoc(`{"children": [{"kind": "ChartStructure", "metadata": {"name": "c"}, "spec": ` + spec + `}]}`)
	}
	html, _ := renderWithDefaults(t, []config.Document{page(`{"dataset": "sales"}`)}, []dataset.Result{sales}, "")
	tag := openTag(t, html, "bn-chart-structure")
	if !strings.Contains(tag, "dimensions") || !strings.Contains(tag, "relative") {
		t.Errorf("stack default missing: %s", tag)
	}
	html, _ = renderWithDefaults(t, []config.Document{page(`{"dataset": "sales", "stack": {"by": "scenarios"}}`)}, []dataset.Result{sales}, "")
	tag = openTag(t, html, "bn-chart-structure")
	if !strings.Contains(tag, "scenarios") || strings.Contains(tag, "dimensions") || !strings.Contains(tag, "relative") {
		t.Errorf("author's key wins, the rest deep-merges: %s", tag)
	}
}
