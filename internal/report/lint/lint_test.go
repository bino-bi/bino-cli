package lint

import (
	"context"
	"testing"
)

func TestRunnerEmpty(t *testing.T) {
	runner := NewRunner(nil)
	findings := runner.Run(context.Background(), nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestRunnerWithRule(t *testing.T) {
	testRule := Rule{
		ID:          "test-rule",
		Name:        "Test Rule",
		Description: "A rule for testing.",
		Check: func(_ context.Context, docs []Document) []Finding {
			findings := make([]Finding, 0, len(docs))
			for _, doc := range docs {
				findings = append(findings, Finding{
					RuleID:  "test-rule",
					Message: "test finding",
					File:    doc.File,
					DocIdx:  doc.Position,
				})
			}
			return findings
		},
	}

	runner := NewRunner([]Rule{testRule})
	docs := []Document{
		{File: "/test/a.yaml", Position: 1, Kind: "Dataset", Name: "test"},
	}

	findings := runner.Run(context.Background(), docs)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "test-rule" {
		t.Errorf("expected rule ID 'test-rule', got %q", findings[0].RuleID)
	}
	if findings[0].File != "/test/a.yaml" {
		t.Errorf("expected file '/test/a.yaml', got %q", findings[0].File)
	}
}

func TestDefaultRunner(t *testing.T) {
	runner := NewDefaultRunner()
	if runner == nil {
		t.Fatal("NewDefaultRunner returned nil")
	}
	// Just verify it doesn't panic with empty docs
	_ = runner.Run(context.Background(), nil)
}

func TestRunnerRules(t *testing.T) {
	rule1 := Rule{ID: "rule1", Name: "Rule 1"}
	rule2 := Rule{ID: "rule2", Name: "Rule 2"}
	runner := NewRunner([]Rule{rule1, rule2})

	rules := runner.Rules()
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != "rule1" {
		t.Errorf("expected first rule ID 'rule1', got %q", rules[0].ID)
	}
}

func TestRunnerSkipsNilCheck(t *testing.T) {
	ruleWithNilCheck := Rule{
		ID:    "nil-check",
		Name:  "Nil Check Rule",
		Check: nil,
	}
	ruleWithCheck := Rule{
		ID:   "has-check",
		Name: "Has Check Rule",
		Check: func(_ context.Context, _ []Document) []Finding {
			return []Finding{{RuleID: "has-check", Message: "found"}}
		},
	}

	runner := NewRunner([]Rule{ruleWithNilCheck, ruleWithCheck})
	findings := runner.Run(context.Background(), nil)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (skipping nil check), got %d", len(findings))
	}
	if findings[0].RuleID != "has-check" {
		t.Errorf("expected rule ID 'has-check', got %q", findings[0].RuleID)
	}
}
