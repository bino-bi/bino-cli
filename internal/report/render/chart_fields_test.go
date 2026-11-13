package render

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

func TestChartTime_NewFields(t *testing.T) {
	html, err := ComponentFromSpec("ChartTime", json.RawMessage(`{
		"dataset": "revenue",
		"dataFormatDigitsDecimal": 0,
		"dataFormatDigitsPercent": 1,
		"axisLabelsStep": 2,
		"scenarioSwitch": true,
		"axisTitlePosition": "right"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec: %v", err)
	}
	for _, want := range []string{
		" data-format-digits-decimal='0'", " data-format-digits-percent='1'", " axis-labels-step='2'",
		" scenario-switch='true'", " axis-title-position='right'",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected%s in HTML, got:\n%s", want, html)
		}
	}
}

func TestChartStructure_NewFields(t *testing.T) {
	html, err := ComponentFromSpec("ChartStructure", json.RawMessage(`{
		"dataset": "revenue", "dataFormatDigitsDecimal": 2, "dataFormatDigitsPercent": 0
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec: %v", err)
	}
	for _, want := range []string{" data-format-digits-decimal='2'", " data-format-digits-percent='0'"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected%s in HTML, got:\n%s", want, html)
		}
	}
}

// A chart that sets none of the new fields renders as before.
func TestCharts_NewFieldsAbsentByDefault(t *testing.T) {
	for _, kind := range []string{"ChartTime", "ChartStructure"} {
		html, err := ComponentFromSpec(kind, json.RawMessage(`{"dataset": "revenue", "variances": ["dac1_pp1_pos"]}`), nil)
		if err != nil {
			t.Fatalf("ComponentFromSpec: %v", err)
		}
		for _, absent := range []string{" data-format-digits", " axis-labels-step=", " scenario-switch=", " axis-title-position=", " scenarios="} {
			if strings.Contains(html, absent) {
				t.Errorf("%s: expected no%s attribute, got:\n%s", kind, absent, html)
			}
		}
	}
}

func TestCharts_ScenariosAttr(t *testing.T) {
	tests := []struct {
		kind, scenarios, want string
	}{
		{"ChartTime", `[]`, " scenarios='none'"},
		{"ChartStructure", `[]`, " scenarios='none'"},
		{"ChartTime", `["ac1","pp1"]`, " scenarios='ac1,pp1'"},
		{"ChartTime", `"ac1"`, " scenarios='ac1'"},
		{"ChartTime", `null`, ""},
		{"ChartTime", `""`, ""},
		{"ChartStructure", `null`, ""},
		// Only the two charts know "none": a Table keeps detecting its scenarios.
		{"Table", `[]`, ""},
	}
	for _, tt := range tests {
		html, err := ComponentFromSpec(tt.kind, json.RawMessage(`{"dataset": "revenue", "scenarios": `+tt.scenarios+`}`), nil)
		if err != nil {
			t.Fatalf("%s %s: %v", tt.kind, tt.scenarios, err)
		}
		if tt.want == "" {
			if strings.Contains(html, " scenarios=") {
				t.Errorf("%s scenarios %s: expected no scenarios attribute, got:\n%s", tt.kind, tt.scenarios, html)
			}
			continue
		}
		if !strings.Contains(html, tt.want) {
			t.Errorf("%s scenarios %s: expected%s, got:\n%s", tt.kind, tt.scenarios, tt.want, html)
		}
	}
}

const scenariosNoneManifests = `apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata:
  name: variance_only
spec:
  dataset: profit
  chartTitle: standalone
  scenarios: []
  variances: [dac1_pp1_pos]
---
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata:
  name: with_params
  params:
    - name: TITLE
      type: string
      default: none
spec:
  dataset: profit
  chartTitle: ${TITLE}
  scenarios: []
  variances: [dac1_pp1_pos]
---
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata:
  name: with_scenarios
spec:
  dataset: profit
  chartTitle: base with scenarios
  scenarios: [ac1, pp1]
---
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: tree
spec:
  edges:
    - { from: a, to: b }
  nodes:
    - id: a
      kind: ChartTime
      ref: variance_only
      spec:
        chartTitle: tree ref
    - id: b
      kind: ChartStructure
      spec:
        dataset: profit
        chartTitle: tree inline
        scenarios: []
        variances: [dac1_pp1_pos]
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartTime
      metadata:
        name: page_inline
      spec:
        dataset: profit
        chartTitle: page inline
        scenarios: []
        variances: [dac1_pp1_pos]
    - kind: ChartTime
      ref: variance_only
    - kind: ChartTime
      ref: with_params
      params:
        TITLE: page ref params
    - kind: ChartTime
      ref: with_scenarios
      spec:
        chartTitle: page ref override
        scenarios: []
    - kind: LayoutCard
      metadata:
        name: card
      spec:
        children:
          - kind: ChartStructure
            metadata:
              name: card_inline
            spec:
              dataset: profit
              chartTitle: card inline
              scenarios: []
              variances: [dac1_pp1_pos]
    - kind: Grid
      metadata:
        name: grid
      spec:
        rowHeaders: [r]
        columnHeaders: [a, b, c]
        children:
          - row: 0
            column: 0
            kind: ChartTime
            spec:
              dataset: profit
              chartTitle: grid inline
              scenarios: []
              variances: [dac1_pp1_pos]
          - row: 0
            column: 1
            kind: ChartTime
            ref: variance_only
            spec:
              chartTitle: grid ref
          - row: 0
            column: 2
            kind: ChartTime
            ref: with_params
            params:
              TITLE: grid ref params
    - kind: Tree
      ref: tree
    - kind: SmallMultiple
      metadata:
        name: sm_inline
      spec:
        dataset: profit
        unitScaling: 10
        scale: none
        base:
          kind: ChartTime
          spec:
            chartTitle: sm inline ${panel.label}
            scenarios: []
            variances: [dac1_pp1_pos]
        panels:
          by: rowGroup
          limit: 1
          items:
            - label: item
              filter: "ac1 > 0"
              spec:
                variances: [dac1_pl1_pos]
    - kind: SmallMultiple
      metadata:
        name: sm_ref
      spec:
        dataset: profit
        unitScaling: 10
        scale: none
        base:
          kind: ChartTime
          ref: with_params
          params:
            TITLE: sm ref params
        card:
          cardShowBorder: true
        panels:
          by: rowGroup
          limit: 1
    - kind: SmallMultiple
      metadata:
        name: sm_item
      spec:
        dataset: profit
        unitScaling: 10
        scale: none
        base:
          kind: ChartTime
          ref: with_scenarios
        panels:
          items:
            - label: keeps
              spec:
                chartTitle: sm item keeps
            - label: none
              spec:
                chartTitle: sm item none
                scenarios: []
`

// An explicit empty scenario list must reach the chart from every place a
// chart spec can be written: a [] that turns into "not set" on the way would
// bring the scenarios back.
func TestCharts_ScenariosNoneSurvivesEveryPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(scenariosNoneManifests), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results := []dataset.Result{{Name: "profit", Data: json.RawMessage(profitRows)}}
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, results, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.46", nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	scenarios := map[string]string{}
	for _, tag := range regexp.MustCompile(`<bn-chart-(?:time|structure)[^>]*>`).FindAllString(string(res.HTML), -1) {
		attrs := attrsOf(tag)
		scenarios[attrs["chart-title"]] = attrs["scenarios"]
	}
	want := map[string]string{
		"page inline":       "none",
		"standalone":        "none",
		"page ref params":   "none",
		"page ref override": "none",
		"card inline":       "none",
		"grid inline":       "none",
		"grid ref":          "none",
		"grid ref params":   "none",
		"tree ref":          "none",
		"tree inline":       "none",
		"sm inline Berlin":  "none",
		"sm inline item":    "none",
		"sm ref params":     "none",
		"sm item keeps":     "ac1,pp1",
		"sm item none":      "none",
	}
	for title, value := range want {
		got, ok := scenarios[title]
		if !ok {
			t.Errorf("chart %q is not in the HTML", title)
			continue
		}
		if got != value {
			t.Errorf("chart %q: scenarios = %q, want %q", title, got, value)
		}
	}
}

// Dataset defaults fill fields that are not set. An empty scenario list is set.
func TestCharts_ScenariosNoneBeatsDatasetDefault(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "ChartTime", "metadata": {"name": "c"}, "spec": {"dataset": "profit", "scenarios": []}}]}`)
	results := []dataset.Result{{
		Name:     "profit",
		Data:     json.RawMessage(profitRows),
		Defaults: dataset.Defaults{dataset.AnyToken: {"scenarios": json.RawMessage(`["ac1","pp1"]`)}},
	}}
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.46", nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := attrsOf(openTag(t, string(res.HTML), "bn-chart-time"))["scenarios"]; got != "none" {
		t.Fatalf("scenarios = %q, want none", got)
	}
}
