package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestPresentationEmitsRuleSets(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test", "ruleset": "corp_rules"}}]}`)
	rules := makeTestDoc("RuleSet", "corp_rules", json.RawMessage(`{
		"kind": "RuleSet",
		"metadata": {"name": "corp_rules"},
		"spec": {"content": {"scenarios": {"pl": {"name": "PLAN", "colorIndex": 50}}}}
	}`))
	artifact := config.Artifact{
		Document: makeTestDoc("ReportArtefact", "deck", json.RawMessage(`{"kind": "ReportArtefact", "metadata": {"name": "deck"}, "spec": {}}`)),
	}
	docs := []config.Document{page, rules}

	result, _, err := GeneratePresentationHTML(context.Background(), docs, nil, artifact, config.PresentationConfig{}, nil, nil, "v1.0.0", nil, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePresentationHTML failed: %v", err)
	}
	if html := string(result.HTML); !strings.Contains(html, `<bn-ruleset name='corp_rules'>`) {
		t.Fatalf("expected <bn-ruleset name='corp_rules'> in the presentation, got:\n%s", html)
	}

	frame, _, err := GeneratePresentationFrameAndContext(context.Background(), docs, nil, artifact, config.PresentationConfig{}, nil, nil, "v1.0.0", nil, nil)
	if err != nil {
		t.Fatalf("GeneratePresentationFrameAndContext failed: %v", err)
	}
	if !strings.Contains(string(frame.ContextHTML), `<bn-ruleset name='corp_rules'>`) {
		t.Fatalf("expected <bn-ruleset name='corp_rules'> in the presentation context, got:\n%s", frame.ContextHTML)
	}
}
