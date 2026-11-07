package lint

import (
	"context"
	"encoding/json"
	"testing"
)

func smDoc(kind, spec string) Document {
	if kind == "SmallMultiple" {
		return Document{Kind: kind, Name: "sm", File: "sm.yaml", Position: 1, Raw: json.RawMessage(`{"kind":"SmallMultiple","spec":` + spec + `}`)}
	}
	return Document{Kind: "LayoutPage", Name: "page", File: "page.yaml", Position: 1,
		Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"SmallMultiple","spec":` + spec + `}]}}]}}`)}
}

func TestSmallMultipleScale(t *testing.T) {
	tests := []struct {
		name  string
		spec  string
		fires bool
	}{
		{"fixed", `{"base":{"kind":"ChartTime"},"unitScaling":"group","scale":"0.5"}`, false},
		{"none and a number", `{"base":{"kind":"Table"},"unitScaling":10,"scale":"none"}`, false},
		{"scatter needs no unitScaling", `{"base":{"kind":"ChartScatter"},"scale":0.8}`, false},
		{"scale missing", `{"base":{"kind":"ChartTime"},"unitScaling":"group"}`, true},
		{"scale auto", `{"base":{"kind":"ChartTime"},"unitScaling":"group","scale":"auto"}`, true},
		{"unitScaling missing", `{"base":{"kind":"ChartTime"},"scale":"0.5"}`, true},
		{"unitScaling auto", `{"base":{"kind":"ChartStructure"},"unitScaling":"auto","scale":"0.5"}`, true},
		{"percentageScaling auto", `{"base":{"kind":"ChartTime"},"unitScaling":1,"percentageScaling":"auto","scale":"0.5"}`, true},
	}
	for _, tt := range tests {
		for _, kind := range []string{"SmallMultiple", "LayoutPage"} {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				findings := smallMultipleScale.Check(context.Background(), []Document{smDoc(kind, tt.spec)})
				if (len(findings) > 0) != tt.fires {
					t.Fatalf("findings = %+v, want fires=%v", findings, tt.fires)
				}
				if tt.fires && (findings[0].Severity != "error" || findings[0].Path == "") {
					t.Fatalf("finding = %+v", findings[0])
				}
			})
		}
	}
}

func TestSmallMultipleBaseDatasetAndByDatasource(t *testing.T) {
	doc := smDoc("LayoutPage", `{"dataset":"$src","base":{"kind":"ChartTime","spec":{"dataset":"x"}},"panels":{"by":"rowGroup"}}`)
	if f := smallMultipleBaseDataset.Check(context.Background(), []Document{doc}); len(f) != 1 || f[0].Path != "spec.children[0].spec.children[0].spec.base.spec.dataset" {
		t.Errorf("base dataset findings = %+v", f)
	}
	if f := smallMultipleByDatasource.Check(context.Background(), []Document{doc}); len(f) != 1 || f[0].Severity != "error" {
		t.Errorf("by datasource findings = %+v", f)
	}
	items := smDoc("SmallMultiple", `{"dataset":"$src","base":{"kind":"ChartTime"},"panels":{"items":[{"label":"a"}]}}`)
	if f := smallMultipleByDatasource.Check(context.Background(), []Document{items}); len(f) != 0 {
		t.Errorf("items on a datasource are fine: %+v", f)
	}
}
