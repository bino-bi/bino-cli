package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// refStrings renders refs as "dataset|role|field|column" for compact asserts.
func refStrings(refs []ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, fmt.Sprintf("%s|%s|%s|%s", r.Dataset, r.Role, r.Field, r.Column))
	}
	return out
}

func assertRefs(t *testing.T, got []ColumnRef, want []string) {
	t.Helper()
	if g := refStrings(got); !slices.Equal(g, want) {
		t.Fatalf("refs mismatch\n got: %s\nwant: %s", strings.Join(g, "\n      "), strings.Join(want, "\n      "))
	}
}

func TestColumnRefsPerKind(t *testing.T) {
	t.Parallel()
	tableImplicit := []string{"sales|implicit||category", "sales|implicit||categoryIndex", "sales|implicit||operation"}
	tests := []struct {
		name     string
		kind     string
		spec     string
		datasets []string
		want     []string
	}{
		{
			name: "table full",
			kind: "Table",
			spec: `{"scenarios":["ac1","pp1"],"variances":["dac1_pp1_pos"],"order":"rowgroupindex","grouped":true,
				"thereof":[{"rowGroup":"R"}],"filter":"region = 'EMEA'","barColumns":["fc1"],
				"attributes":[{"label":"Plan","expression":"sum(pl1)"},{"label":"L","expression":"lit(5)"}]}`,
			want: append([]string{
				"sales|scenario|scenarios|ac1", "sales|scenario|scenarios|pp1",
				"sales|variance|variances|ac1", "sales|variance|variances|pp1",
				"sales|group|grouped|rowGroup", "sales|group|grouped|rowGroupIndex",
				"sales|group|thereof|rowGroup", "sales|group|thereof|category", "sales|group|thereof|subCategory",
				"sales|order|order|rowGroupIndex",
				"sales|filter|filter|region",
				"sales|attribute|attributes|pl1",
			}, tableImplicit...),
		},
		{
			name: "table blank",
			kind: "Table",
			spec: `{"variances":"","order":""}`,
			want: append([]string{"sales|scenario|scenarios|auto"}, tableImplicit...),
		},
		{
			name: "table auto order and partof with default type",
			kind: "Table",
			spec: `{"scenarios":"auto","order":"auto","partof":[{"rowGroup":"R"}]}`,
			want: append([]string{"sales|scenario|scenarios|auto", "sales|order|order|auto"}, tableImplicit...),
		},
		{
			name: "table partof with sum type",
			kind: "Table",
			spec: `{"scenarios":"ac1","type":"sum","partof":"[{\"rowGroup\":\"R\"}]"}`,
			want: append([]string{"sales|scenario|scenarios|ac1", "sales|group|partof|rowGroup", "sales|group|partof|category"}, tableImplicit...),
		},
		{
			name: "table columnthereof wins over interval",
			kind: "Table",
			spec: `{"scenarios":"ac1","interval":"year","columnthereof":[{"scenario":"pl1","name":"DE"}]}`,
			want: append([]string{
				"sales|scenario|scenarios|ac1", "sales|scenario|columnthereof|pl1",
				"sales|group|columnthereof|columnGroup", "sales|group|columnthereof|columnSubGroup",
				"sales|group|columnthereof|rowGroup", "sales|group|columnthereof|subCategory",
			}, tableImplicit...),
		},
		{
			name: "table interval setname",
			kind: "Table",
			spec: `{"scenarios":"ac1","interval":"setname"}`,
			want: append([]string{"sales|scenario|scenarios|ac1", "sales|group|interval|setname"}, tableImplicit...),
		},
		{
			name: "table interval date",
			kind: "Table",
			spec: `{"scenarios":"ac1","interval":"month"}`,
			want: append([]string{"sales|scenario|scenarios|ac1", "sales|group|interval|date"}, tableImplicit...),
		},
		{
			name: "table attributes string form keeps written order",
			kind: "Table",
			spec: `{"scenarios":"ac1","attributes":"{\"B\":\"max(_b)\",\"A\":\"sum(ac1)\"}"}`,
			want: append([]string{"sales|scenario|scenarios|ac1", "sales|attribute|attributes|_b", "sales|attribute|attributes|ac1"}, tableImplicit...),
		},
		{
			name: "table unresolved tokens stay visible",
			kind: "Table",
			spec: `{"scenarios":"inherited-page","variances":"${V}"}`,
			want: append([]string{"sales|scenario|scenarios|inherited-page", "sales|variance|variances|${V}"}, tableImplicit...),
		},
		{
			name: "full manifest wrapper",
			kind: "Table",
			spec: `{"apiVersion":"bino.bi/v1","kind":"Table","metadata":{"name":"t"},"spec":{"scenarios":"ac1"}}`,
			want: append([]string{"sales|scenario|scenarios|ac1"}, tableImplicit...),
		},
		{
			name:     "multi dataset repeats per binding",
			kind:     "ChartTime",
			spec:     `{"scenarios":"ac1"}`,
			datasets: []string{"a", "$b"},
			want: []string{
				"a|scenario|scenarios|ac1", "a|implicit||date", "a|implicit||operation",
				"$b|scenario|scenarios|ac1", "$b|implicit||date", "$b|implicit||operation",
			},
		},
		{
			name: "structure chart with dimension stack",
			kind: "ChartStructure",
			spec: `{"scenarios":"ac1,pp1","variances":"drac1_pp1_neg","level":"categoryindex","stack":{"by":"dimensions"},"filter":"[my col] > 0"}`,
			want: []string{
				"sales|scenario|scenarios|ac1", "sales|scenario|scenarios|pp1",
				"sales|variance|variances|ac1", "sales|variance|variances|pp1",
				"sales|group|level|categoryIndex", "sales|group|stack|subCategory",
				"sales|order|order|auto",
				"sales|filter|filter|my col",
				"sales|implicit||operation",
			},
		},
		{
			name: "structure chart stack on rowgroup index",
			kind: "ChartStructure",
			spec: `{"scenarios":"ac1","level":"rowgroupindex","order":"ac1","stack":{"by":"dimensions"}}`,
			want: []string{
				"sales|scenario|scenarios|ac1", "sales|group|level|rowGroupIndex", "sales|group|stack|category",
				"sales|order|order|ac1", "sales|implicit||operation",
			},
		},
		{
			name: "structure chart stack on subcategory and blank fields",
			kind: "ChartStructure",
			spec: `{"level":"subcategory","stack":{"by":"dimensions"}}`,
			want: []string{
				"sales|scenario|scenarios|auto", "sales|group|level|subCategory",
				"sales|order|order|auto", "sales|implicit||operation",
			},
		},
		{
			name: "time chart ignores level order stack and interval",
			kind: "ChartTime",
			spec: `{"scenarios":["ac1"],"level":"category","order":"ac1","dateInterval":"month","stack":{"by":"dimensions"}}`,
			want: []string{"sales|scenario|scenarios|ac1", "sales|implicit||date", "sales|implicit||operation"},
		},
		{
			name: "scatter",
			kind: "ChartScatter",
			spec: `{"x":"ac1","y":{"measure":"dac1_pp1"},"level":"subcategory","facet":{"level":"rowgroup"}}`,
			want: []string{
				"sales|variance|y|ac1", "sales|variance|y|pp1",
				"sales|measure|x|ac1",
				"sales|group|level|subCategory", "sales|group|seriesLevel|category", "sales|group|facet|rowGroup",
				"sales|implicit||subCategoryIndex", "sales|implicit||categoryIndex", "sales|implicit||rowGroupIndex",
			},
		},
		{
			name: "scatter auto levels",
			kind: "ChartScatter",
			spec: `{"x":"ac1","y":"ac2"}`,
			want: []string{
				"sales|measure|x|ac1", "sales|measure|y|ac2",
				"sales|group|level|auto", "sales|group|seriesLevel|auto",
			},
		},
		{
			name: "bubble compare skips share",
			kind: "ChartBubble",
			spec: `{"x":"ac1","y":"ac2","size":{"measure":"ac3"},"share":"ac4","compareWith":"pp","level":"category","seriesLevel":"none","facet":null}`,
			want: []string{
				"sales|measure|x|ac1", "sales|measure|y|ac2", "sales|measure|size|ac3", "sales|measure|share|ac4",
				"sales|measure|compareWith|pp1", "sales|measure|compareWith|pp2", "sales|measure|compareWith|pp3",
				"sales|group|level|category", "sales|implicit||categoryIndex",
			},
		},
		{
			name: "bullet",
			kind: "ChartBullet",
			spec: `{"actual":"ac1","target":{"measure":"pl1"},"level":"rowgroup","order":"ac1","variances":"auto"}`,
			want: []string{
				"sales|measure|actual|ac1", "sales|measure|target|pl1",
				"sales|group|level|rowGroup", "sales|order|order|ac1", "sales|implicit||operation",
			},
		},
		{
			name: "bullet blank",
			kind: "ChartBullet",
			spec: `{}`,
			want: []string{
				"sales|measure|actual|auto", "sales|measure|target|auto",
				"sales|group|level|auto", "sales|order|order|auto", "sales|implicit||operation",
			},
		},
		{
			name:     "text uses template datasets",
			kind:     "Text",
			spec:     `{"dataset":"sales","value":"Rev ${data.kpi[0].ac1} vs ${this.data['$src'][0].pp1}, n=${data.kpi.length}"}`,
			datasets: []string{"sales"},
			want:     []string{"kpi|template|value|ac1", "$src|template|value|pp1"},
		},
		{
			name: "image has none",
			kind: "Image",
			spec: `{"source":"x.png"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			datasets := tt.datasets
			if datasets == nil {
				datasets = []string{"sales"}
			}
			assertRefs(t, columnRefs(tt.kind, json.RawMessage(tt.spec), datasets, ancestors{}), tt.want)
		})
	}
}

func TestColumnRefsAuto(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{`{}`, `{"scenarios":null,"level":"","order":"AUTO","actual":"auto","target":{"measure":""}}`} {
		got := refStrings(columnRefs("ChartStructure", json.RawMessage(spec), []string{"s"}, ancestors{}))
		for _, want := range []string{"s|scenario|scenarios|auto", "s|group|level|auto", "s|order|order|auto"} {
			if !slices.Contains(got, want) {
				t.Errorf("ChartStructure %s: missing %q in %v", spec, want, got)
			}
		}
		got = refStrings(columnRefs("ChartBullet", json.RawMessage(spec), []string{"s"}, ancestors{}))
		for _, want := range []string{"s|measure|actual|auto", "s|measure|target|auto", "s|group|level|auto", "s|order|order|auto"} {
			if !slices.Contains(got, want) {
				t.Errorf("ChartBullet %s: missing %q in %v", spec, want, got)
			}
		}
	}
}

func TestFilterColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		filter string
		want   []string
	}{
		{"", nil},
		{"region = 'EMEA'", []string{"region"}},
		{`rowGroup IN ('a b', 'c') AND NOT [my col] = "x y"`, []string{"rowGroup", "my col"}},
		{"UPPER(name) LIKE 'A%' or amount > 10.5", []string{"name", "amount"}},
		{"region = '${REGION}' and ${P} = x", []string{"region", "x"}},
		{"`Sales Region` = 'it''s' -- comment words", []string{"Sales Region"}},
		{"@v = 1 AND :p = 2 AND $q = 3 AND col IS NULL", []string{"col"}},
		{"subCategory BETWEEN 1 AND 2", []string{"subCategory"}},
	}
	for _, tt := range tests {
		if got := filterColumns(tt.filter); !slices.Equal(got, tt.want) {
			t.Errorf("filterColumns(%q) = %v, want %v", tt.filter, got, tt.want)
		}
	}
}

func TestTemplateColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  [][2]string
	}{
		{"Total ${data.kpi[0].ac1}", [][2]string{{"kpi", "ac1"}}},
		{"${this.data['$src'][1].name}", [][2]string{{"$src", "name"}}},
		{`${data["x"][0]?.pp1}`, [][2]string{{"x", "pp1"}}},
		{"${data.kpi.length} ${mydata.a[0].b}", nil},
	}
	for _, tt := range tests {
		if got := templateColumns(tt.value); !slices.Equal(got, tt.want) {
			t.Errorf("templateColumns(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestColumnRefsNeverFail(t *testing.T) {
	t.Parallel()
	bad := []string{
		`{"scenarios":5,"variances":{},"grouped":"yes","thereof":"{broken","partof":7,"columnthereof":[1],"attributes":"{\"a\":1}",
		  "x":[],"y":{"measure":3},"level":{},"order":[1],"filter":7,"stack":"x","facet":"y","nodes":"z","value":3}`,
		`null`, `not json`, `[]`, `"str"`,
	}
	for _, kind := range []string{"Table", "ChartStructure", "ChartTime", "ChartScatter", "ChartBubble", "ChartBullet", "Text", "Label", "Tree", "Grid"} {
		for _, spec := range bad {
			columnRefs(kind, json.RawMessage(spec), []string{"s"}, ancestors{})
		}
	}

	page := makeDoc("LayoutPage", "p", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "LayoutPage", "metadata": {"name": "p"},
		"spec": {"titleScenarios": {"bad": true}, "children": [
			{"kind": "Table", "spec": {"dataset": "s", "scenarios": 5, "grouped": "yes", "thereof": "{broken"}}
		]}
	}`))
	if _, err := Build(context.Background(), []config.Document{page}); err != nil {
		t.Fatalf("Build with malformed column fields failed: %v", err)
	}
}

func TestBuildColumnsInherited(t *testing.T) {
	t.Parallel()
	page := makeDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "LayoutPage", "metadata": {"name": "page"},
		"spec": {
			"titleScenarios": ["ac1", "pp1"], "titleVariances": "dac1_pp1_pos", "titleOrder": "ac1",
			"children": [
				{"kind": "Table", "spec": {"dataset": "sales",
					"scenarios": "inherited-page", "variances": "inherited-page", "order": "inherited-page"}},
				{"kind": "LayoutCard", "spec": {"titleScenarios": "fc1", "titleVariances": "inherited-page", "children": [
					{"kind": "ChartStructure", "spec": {"dataset": "sales",
						"scenarios": "inherited-closest", "variances": "inherited-closest", "level": "category", "order": "ac1"}},
					{"kind": "Table", "spec": {"dataset": "sales", "scenarios": "inherited-page"}}
				]}}
			]
		}
	}`))
	card := makeDoc("LayoutCard", "card_root", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "LayoutCard", "metadata": {"name": "card_root"},
		"spec": {"titleScenarios": "pl1", "children": [
			{"kind": "ChartTime", "spec": {"dataset": "sales", "scenarios": "inherited-page"}},
			{"kind": "ChartTime", "spec": {"dataset": "sales", "scenarios": "inherited-closest"}}
		]}
	}`))
	standalone := makeDoc("ChartTime", "lonely", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "ChartTime", "metadata": {"name": "lonely"},
		"spec": {"dataset": "sales", "scenarios": "inherited-closest"}
	}`))
	g, err := Build(context.Background(), []config.Document{page, card, standalone})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	columns := func(id string) []string {
		t.Helper()
		node, ok := g.NodeByID(makeNodeID(NodeComponent, id))
		if !ok {
			t.Fatalf("node %s not found", id)
		}
		return refStrings(node.Columns)
	}
	timeOnly := func(scenario string) []string {
		return []string{"sales|scenario|scenarios|" + scenario, "sales|implicit||date", "sales|implicit||operation"}
	}

	if got, want := columns("page#0"), []string{
		"sales|scenario|scenarios|ac1", "sales|scenario|scenarios|pp1",
		"sales|variance|variances|ac1", "sales|variance|variances|pp1",
		"sales|order|order|ac1",
		"sales|implicit||category", "sales|implicit||categoryIndex", "sales|implicit||operation",
	}; !slices.Equal(got, want) {
		t.Errorf("page table: got %v, want %v", got, want)
	}
	if got, want := columns("page#1.0"), []string{
		"sales|scenario|scenarios|fc1",
		"sales|variance|variances|ac1", "sales|variance|variances|pp1",
		"sales|group|level|category", "sales|order|order|ac1", "sales|implicit||operation",
	}; !slices.Equal(got, want) {
		t.Errorf("card chart: got %v, want %v", got, want)
	}
	if got := columns("page#1.1"); !slices.Contains(got, "sales|scenario|scenarios|pp1") {
		t.Errorf("card table with inherited-page: got %v, want page scenarios", got)
	}
	if got, want := columns("card_root#0"), timeOnly("inherited-page"); !slices.Equal(got, want) {
		t.Errorf("root card without page: got %v, want %v", got, want)
	}
	if got, want := columns("card_root#1"), timeOnly("pl1"); !slices.Equal(got, want) {
		t.Errorf("root card closest: got %v, want %v", got, want)
	}
	if got, want := columns("lonely"), timeOnly("inherited-closest"); !slices.Equal(got, want) {
		t.Errorf("standalone: got %v, want %v", got, want)
	}
}

func TestBuildColumnsRefGridTree(t *testing.T) {
	t.Parallel()
	chart := makeDoc("ChartTime", "regionChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "ChartTime", "metadata": {"name": "regionChart"},
		"spec": {"dataset": "sales", "scenarios": "${SCEN}"}
	}`))
	chart.Params = []config.LayoutPageParamSpec{{Name: "SCEN"}}
	page := makeDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1", "kind": "LayoutPage", "metadata": {"name": "page"},
		"spec": {"children": [
			{"kind": "ChartTime", "ref": "regionChart", "params": {"SCEN": "fc1"}},
			{"kind": "Grid", "spec": {"children": [
				{"row": 0, "column": 0, "kind": "Table", "spec": {"dataset": "sales", "scenarios": "ac1"}}
			]}},
			{"kind": "Tree", "spec": {"nodes": [
				{"id": "a", "kind": "Table", "spec": {"dataset": "s1", "scenarios": "ac1"}},
				{"id": "b", "kind": "Label", "spec": {"dataset": "s2", "value": "${data.s2[0].pp1}"}},
				{"id": "c", "kind": "Table", "ref": "missing", "optional": true}
			]}}
		]}
	}`))
	g, err := Build(context.Background(), []config.Document{chart, page})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := func(id string) *Node {
		t.Helper()
		n, ok := g.NodeByID(makeNodeID(NodeComponent, id))
		if !ok {
			t.Fatalf("node %s not found", id)
		}
		return n
	}

	if got := refStrings(node("page#0").Columns); !slices.Contains(got, "sales|scenario|scenarios|fc1") {
		t.Errorf("ref child with params: got %v", got)
	}
	if got := refStrings(node("regionChart").Columns); !slices.Contains(got, "sales|scenario|scenarios|${SCEN}") {
		t.Errorf("standalone definition: got %v", got)
	}
	if cols := node("page#1").Columns; cols != nil {
		t.Errorf("grid node: got %v, want none", refStrings(cols))
	}
	if got := refStrings(node("page#1.0").Columns); !slices.Contains(got, "sales|scenario|scenarios|ac1") {
		t.Errorf("grid child: got %v", got)
	}
	assertRefs(t, node("page#2").Columns, []string{
		"s1|scenario|nodes[a].scenarios|ac1",
		"s1|implicit||category", "s1|implicit||categoryIndex", "s1|implicit||operation",
		"s2|template|nodes[b].value|pp1",
	})
}
