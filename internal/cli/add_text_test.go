package cli

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

func TestBuildTextDocument(t *testing.T) {
	t.Run("static value with scale", func(t *testing.T) {
		data := TextManifestData{
			Name:  "report_title",
			Value: "Monthly Sales Report",
			Scale: "none",
		}
		got := wizardRoundTrip(t, buildTextDocument(data), "text.yaml")
		assertContainsAll(t, got, []string{
			"kind: Text",
			"name: report_title",
			"value: Monthly Sales Report",
			"scale: none",
		})
		if strings.Contains(got, "dataset:") {
			t.Errorf("static text must not reference a dataset:\n%s", got)
		}
	})

	t.Run("dataset is written as given", func(t *testing.T) {
		data := TextManifestData{
			Name:    "total_sales",
			Dataset: "sales_summary",
			Value:   "Total: ${data.sales_summary[0].ac1}",
		}
		got := wizardRoundTrip(t, buildTextDocument(data), "text_ds.yaml")
		assertContainsAll(t, got, []string{"dataset: sales_summary"})
	})

	// The wizard now always collects a value (bn-text renders only the value
	// template - a dataset alone produces an empty block), and the write gate
	// backstops that: a dataset-only Text must never land on disk. If this
	// starts failing, textSpec no longer requires value.
	t.Run("dataset-only text is rejected at write time", func(t *testing.T) {
		data := TextManifestData{Name: "no_value_text", Dataset: "sales_summary"}
		err := WriteSchemaDocument(buildTextDocument(data), t.TempDir(), "no_value.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the missing value")
		}
	})
}

// TestAddTextNoPromptRequiresValue pins the non-interactive contract:
// --dataset alone is not enough, the flags must also carry the value
// template the schema requires.
func TestAddTextNoPromptRequiresValue(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newAddTextCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"total_sales", "--dataset", "sales_summary", "--output", "text.yaml", "--no-prompt"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a missing-flags error")
	}
	if !strings.Contains(err.Error(), "--value") {
		t.Errorf("error %q does not mention --value", err)
	}
}

func TestBuildComponentStyleDocument(t *testing.T) {
	t.Run("flat JSON object becomes a structured map", func(t *testing.T) {
		data := ComponentStyleManifestData{
			Name:    "header_style",
			Content: `{"global": {"fontFamily": "Arial"}, "bn-text": {"fontSizePx": 24}}`,
		}
		got := wizardRoundTrip(t, buildComponentStyleDocument(data), "style.yaml")
		assertContainsAll(t, got, []string{
			"kind: ComponentStyle",
			"name: header_style",
			"fontFamily: Arial",
			"fontSizePx: 24",
		})
	})

	t.Run("raw CSS string is kept verbatim", func(t *testing.T) {
		data := ComponentStyleManifestData{
			Name:    "css_style",
			Content: "color: red;",
		}
		got := wizardRoundTrip(t, buildComponentStyleDocument(data), "style_css.yaml")
		assertContainsAll(t, got, []string{"content:", "color: red;"})
	})
}

// The help example is what users copy, so it must be a style the schema accepts.
func TestAddComponentStyleHelpExampleIsValid(t *testing.T) {
	example := newAddComponentStyleCommand().Example
	start := strings.Index(example, "--content '")
	if start < 0 {
		t.Fatalf("no --content example in:\n%s", example)
	}
	content := example[start+len("--content '"):]
	content = content[:strings.Index(content, "'")]
	manifest, err := yaml.Marshal(buildComponentStyleDocument(ComponentStyleManifestData{Name: "header_style", Content: content}))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(manifest); err != nil {
		t.Fatalf("help example is not a valid ComponentStyle: %v\n%s", err, manifest)
	}
	// styleContent allows unknown keys, but the engine rejects them.
	var doc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schema.DocumentSchemaBytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var sections map[string]map[string]any
	if err := json.Unmarshal([]byte(content), &sections); err != nil {
		t.Fatalf("help example is not a JSON object of sections: %v", err)
	}
	for section := range sections {
		if _, ok := doc.Defs["styleContent"].Properties[section]; !ok {
			t.Errorf("help example uses unknown style section %q", section)
		}
	}
}

func TestBuildRuleSetDocument(t *testing.T) {
	t.Run("JSON content becomes structured YAML", func(t *testing.T) {
		data := RuleSetManifestData{
			Name:    "corporate_rules",
			Content: `{"scenarios": {"pl": {"name": "PLAN", "sortIndex": 900}}}`,
		}
		got := wizardRoundTrip(t, buildRuleSetDocument(data), "ruleset.yaml")
		assertContainsAll(t, got, []string{
			"kind: RuleSet",
			"name: corporate_rules",
			"scenarios:",
			"name: PLAN",
			"sortIndex: 900",
		})
	})

	t.Run("non-JSON content is kept as a string", func(t *testing.T) {
		data := RuleSetManifestData{
			Name:    "raw_rules",
			Content: "not json at all",
		}
		got := wizardRoundTrip(t, buildRuleSetDocument(data), "ruleset_raw.yaml")
		assertContainsAll(t, got, []string{"content: not json at all"})
	})
}

func TestBuildScalingGroupDocument(t *testing.T) {
	data := ScalingGroupManifestData{
		Name:        "revenue_scale",
		Description: "Shared revenue scaling",
		Value:       250000,
	}
	got := wizardRoundTrip(t, buildScalingGroupDocument(data), "scaling.yaml")
	assertContainsAll(t, got, []string{
		"kind: ScalingGroup",
		"name: revenue_scale",
		"description: Shared revenue scaling",
		"value: 250000",
	})
}
