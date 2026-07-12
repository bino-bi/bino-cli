package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestRuleset_EmittedForChildren(t *testing.T) {
	tests := []struct {
		kind string
		spec string
	}{
		{"Table", `{"dataset": "test", "ruleset": "corporate-rules"}`},
		{"ChartStructure", `{"dataset": "test", "ruleset": "corporate-rules"}`},
		{"ChartTime", `{"dataset": "test", "ruleset": "corporate-rules"}`},
		{"LayoutCard", `{"children": [], "ruleset": "corporate-rules"}`},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			html := renderPageWithChild(t, tt.kind, tt.spec)
			if !strings.Contains(html, `ruleset='corporate-rules'`) {
				t.Fatalf("expected ruleset='corporate-rules' in HTML for %s, got:\n%s", tt.kind, html)
			}
		})
	}
}

func TestRuleset_LayoutPage(t *testing.T) {
	layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"ruleset": "corporate-rules",
			"children": []
		}
	}`))

	result, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{layoutPageDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}
	html := string(result.HTML)

	pageTag := html[strings.Index(html, "<bn-layout-page"):]
	pageTag = pageTag[:strings.Index(pageTag, ">")]
	if !strings.Contains(pageTag, `ruleset='corporate-rules'`) {
		t.Fatalf("expected ruleset='corporate-rules' on <bn-layout-page> tag, got:\n%s", pageTag)
	}
}

func TestRuleset_AbsentByDefault(t *testing.T) {
	html := renderPageWithChild(t, "Table", `{"dataset": "test"}`)

	if strings.Contains(html, "ruleset=") {
		t.Fatalf("expected no ruleset attribute in HTML, got:\n%s", html)
	}
}

// renderWithRuleSetDoc renders a minimal page bundle plus a RuleSet document
// with the given spec JSON and returns the generated HTML.
func renderWithRuleSetDoc(t *testing.T, name, spec string) (string, error) {
	t.Helper()

	layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {"children": []}
	}`))
	ruleSetDoc := makeTestDoc("RuleSet", name, json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "RuleSet",
		"metadata": {"name": "`+name+`"},
		"spec": `+spec+`
	}`))

	result, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{layoutPageDoc, ruleSetDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		return "", err
	}
	return string(result.HTML), nil
}

func TestRuleSet_ElementFromObjectContent(t *testing.T) {
	html, err := renderWithRuleSetDoc(t, "corporate-rules", `{"content": {"scenarios": {"pl": {"name": "PLAN", "sortIndex": 900}}}}`)
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	if !strings.Contains(html, `<bn-ruleset name='corporate-rules'>`) {
		t.Fatalf("expected <bn-ruleset name='corporate-rules'> in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `&#34;scenarios&#34;`) || !strings.Contains(html, `&#34;PLAN&#34;`) {
		t.Fatalf("expected escaped rule-set JSON body in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `</bn-ruleset>`) {
		t.Fatalf("expected closing </bn-ruleset> tag in HTML, got:\n%s", html)
	}
}

func TestRuleSet_ElementFromStringContent(t *testing.T) {
	html, err := renderWithRuleSetDoc(t, "_default", `{"content": "{\"fallback\": {\"name\": \"Series\"}}"}`)
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	if !strings.Contains(html, `<bn-ruleset name='_default'>`) {
		t.Fatalf("expected <bn-ruleset name='_default'> in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `&#34;fallback&#34;`) {
		t.Fatalf("expected escaped rule-set JSON body in HTML, got:\n%s", html)
	}
}

func TestRuleSet_InvalidContentFails(t *testing.T) {
	_, err := renderWithRuleSetDoc(t, "broken", `{"content": "not json"}`)
	if err == nil {
		t.Fatal("expected error for invalid rule-set content, got nil")
	}
	if !strings.Contains(err.Error(), "rule set") {
		t.Fatalf("expected rule set error, got: %v", err)
	}
}
