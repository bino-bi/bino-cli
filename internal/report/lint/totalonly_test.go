package lint

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTotalOnlyScenarios(t *testing.T) {
	doc := func(raw string) Document {
		return Document{File: "pages.yaml", Position: 1, Kind: "LayoutCard", Raw: json.RawMessage(raw)}
	}
	clean := doc(`{"kind":"ChartTime","spec":{"scenarios":["ac1","pl1"],"totals":{"by":"all","scenarios":["pp1"]}}}`)
	if f := totalOnlyScenarios.Check(context.Background(), []Document{clean}); len(f) != 0 {
		t.Fatalf("clean chart: got %+v", f)
	}
	time := doc(`{"kind":"ChartTime","spec":{"scenarios":["ac1","pl1"],"totals":{"scenarios":["pp1","pl1"]}}}`)
	f := totalOnlyScenarios.Check(context.Background(), []Document{time})
	if len(f) != 1 || f[0].Path != "spec.totals.scenarios[1]" || f[0].RuleID != "total-only-scenario" {
		t.Fatalf("time chart: got %+v", f)
	}
	// an inline chart in a card
	card := doc(`{"kind":"LayoutCard","spec":{"children":[{"kind":"ChartStructure","spec":{"scenarios":["ac1","pp1"],"sumScenarios":["pp1"]}}]}}`)
	f = totalOnlyScenarios.Check(context.Background(), []Document{card})
	if len(f) != 1 || f[0].Path != "spec.children[0].spec.sumScenarios[0]" {
		t.Fatalf("structure chart in a card: got %+v", f)
	}
	// the fields of the other kind are not read
	other := doc(`{"kind":"ChartStructure","spec":{"scenarios":["ac1"],"totals":{"scenarios":["ac1"]}}}`)
	if f := totalOnlyScenarios.Check(context.Background(), []Document{other}); len(f) != 0 {
		t.Fatalf("other kind: got %+v", f)
	}
}
