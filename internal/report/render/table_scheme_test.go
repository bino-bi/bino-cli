package render

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

func TestTableCalculationScheme_RenderedAttributes(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"type": "optnototal",
		"runningSum": true,
		"thereofPosition": "above",
		"varianceWaterfall": true
	}`)

	for _, want := range []string{`running-sum='true'`, `thereof-position='above'`, `variance-waterfall='true'`} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}

func TestTableCalculationScheme_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test", "type": "optnototal"}`)

	for _, attr := range []string{"running-sum=", "thereof-position=", "variance-waterfall="} {
		if strings.Contains(html, attr) {
			t.Fatalf("expected no %s attribute in HTML, got:\n%s", attr, html)
		}
	}
}

func TestTableCalculationScheme_EngineGate(t *testing.T) {
	table := `{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", %s}}]}`
	for field, prop := range map[string]string{
		`"runningSum": true`:         "runningSum",
		`"thereofPosition": "above"`: "thereofPosition",
		`"varianceWaterfall": true`:  "varianceWaterfall",
	} {
		docs := []config.Document{pageDoc(fmt.Sprintf(table, field))}
		_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.39")
		if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.40") || !strings.Contains(err.Error(), prop) ||
			!strings.Contains(err.Error(), "table-calculation-scheme-engine-version") {
			t.Fatalf("%s: err = %v", prop, err)
		}
		if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.40"); err != nil {
			t.Fatalf("%s on the new engine: %v", prop, err)
		}
	}
	plain := []config.Document{pageDoc(fmt.Sprintf(table, `"runningSum": false, "thereofPosition": "below", "varianceWaterfall": false`))}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), plain, "en", "", "", ModeBuild, "v1.0.0-next.39"); err != nil {
		t.Fatalf("defaults: %v", err)
	}
}

// A field that only a dataset default turns on reaches the gate too.
func TestTableCalculationScheme_EngineGateDatasetDefault(t *testing.T) {
	docs := []config.Document{pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test"}}]}`)}
	results := []dataset.Result{{Name: "test", Data: json.RawMessage(`[]`), Defaults: dataset.Defaults{"table": {"runningSum": json.RawMessage(`true`)}}}}
	err := checkTableCalculationSchemeEngine(docs, results, "v1.0.0-next.39")
	if err == nil || !strings.Contains(err.Error(), "runningSum") {
		t.Fatalf("err = %v", err)
	}
	if err := checkTableCalculationSchemeEngine(docs, results, "v1.0.0-next.40"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := checkTableCalculationSchemeEngine(docs, []dataset.Result{{Name: "test"}}, "v1.0.0-next.39"); err != nil {
		t.Fatalf("no default: %v", err)
	}
}

// The gate follows the Table that renders: a value in the manifest wins over
// the dataset default, and a default counts only for a Table that binds it.
func TestTableCalculationScheme_EngineGateFollowsTheTable(t *testing.T) {
	results := []dataset.Result{{Name: "test", Data: json.RawMessage(`[]`), Defaults: dataset.Defaults{"table": {
		"runningSum":      json.RawMessage(`true`),
		"thereofPosition": json.RawMessage(`"above"`),
	}}}}
	page := func(child string) []config.Document {
		return []config.Document{pageDoc(`{"children": [` + child + `]}`)}
	}
	base := config.Document{Kind: "Table", Name: "base", Raw: json.RawMessage(
		`{"kind":"Table","metadata":{"name":"base"},"spec":{"dataset":"other","runningSum":false,"thereofPosition":"below"}}`)}
	cases := []struct {
		name string
		docs []config.Document
		prop string // empty: no error
	}{
		{"manifest turns both off", page(`{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", "runningSum": false, "thereofPosition": "below"}}`), ""},
		{"manifest turns one off", page(`{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", "runningSum": false}}`), "thereofPosition"},
		{"null leaves the default", page(`{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", "runningSum": null, "thereofPosition": "below"}}`), "runningSum"},
		{"only a chart binds the dataset", page(`{"kind": "ChartStructure", "metadata": {"name": "c"}, "spec": {"dataset": "test"}}`), ""},
		{"not the primary dataset", page(`{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": ["other", "test"]}}`), ""},
		{"referenced Table turns both off", append(page(`{"kind": "Table", "ref": "base", "spec": {"dataset": "test"}}`), base), ""},
	}
	for _, c := range cases {
		err := checkTableCalculationSchemeEngine(c.docs, results, "v1.0.0-next.39")
		if c.prop == "" && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.prop != "" && (err == nil || !strings.Contains(err.Error(), "a Table with "+c.prop+" needs")) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

// A dataset that the Table only gets at render time still reaches the gate:
// the dataset of a SmallMultiple or of a panel item, and a param placeholder.
func TestTableCalculationScheme_EngineGateUnresolvedDataset(t *testing.T) {
	results := []dataset.Result{{Name: "test", Data: json.RawMessage(`[]`), Defaults: dataset.Defaults{"table": {"runningSum": json.RawMessage(`true`)}}}}
	page := func(child string) config.Document {
		return pageDoc(`{"children": [` + child + `]}`)
	}
	multiple := func(spec string) []config.Document {
		return []config.Document{page(`{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"scale": 1, "unitScaling": 1, ` + spec + `}}`)}
	}
	base := func(fields string) config.Document {
		return config.Document{Kind: "Table", Name: "base", Raw: json.RawMessage(
			`{"kind":"Table","metadata":{"name":"base"},"spec":{"dataset":"${DS}"` + fields + `}}`)}
	}
	child := page(`{"kind": "Table", "ref": "base", "params": {"DS": "test"}}`)
	cases := []struct {
		name string
		docs []config.Document
		hit  bool
	}{
		{"SmallMultiple dataset", multiple(`"dataset": "test", "base": {"kind": "Table", "spec": {"type": "optnototal"}}, "panels": {"by": "rowGroup"}`), true},
		{"panel item dataset", multiple(`"base": {"kind": "Table", "spec": {"type": "optnototal"}}, "panels": {"items": [{"label": "a", "dataset": "test"}]}`), true},
		{"SmallMultiple base turns it off", multiple(`"dataset": "test", "base": {"kind": "Table", "spec": {"runningSum": false}}, "panels": {"by": "rowGroup"}`), false},
		{"SmallMultiple chart base", multiple(`"dataset": "test", "base": {"kind": "ChartTime", "spec": {}}, "panels": {"by": "rowGroup"}`), false},
		{"param placeholder", []config.Document{child, base("")}, true},
		{"param placeholder, base turns it off", []config.Document{child, base(`,"runningSum":false`)}, false},
	}
	for _, c := range cases {
		err := checkTableCalculationSchemeEngine(c.docs, results, "v1.0.0-next.39")
		if !c.hit && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.hit && (err == nil || !strings.Contains(err.Error(), "a Table with runningSum needs")) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}
