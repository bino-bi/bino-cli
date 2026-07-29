package cli

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

// The token list lives in two hand-maintained places: defaultI18nTokens (which
// feeds `bino add i18n --defaults`) and $defs.internationalizationContent in
// document.schema.json (which documents the tokens for editors and MCP agents).
// This test is what keeps them from drifting apart.
//
// It lives in package cli because defaultI18nTokens is unexported and
// internal/cli already imports internal/schema, so the reverse import would cycle.
func TestI18nSchemaTokensMatchDefaults(t *testing.T) {
	// Decoded one $def at a time: sibling definitions use a bare boolean for
	// additionalProperties, which would not fit this struct.
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(schema.DocumentSchemaBytes(), &doc); err != nil {
		t.Fatalf("parse document.schema.json: %v", err)
	}

	raw, ok := doc.Defs["internationalizationContent"]
	if !ok {
		t.Fatal("$defs.internationalizationContent is missing from document.schema.json")
	}

	var def struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		AdditionalProperties struct {
			Type string `json:"type"`
		} `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("parse $defs.internationalizationContent: %v", err)
	}

	// Free-form keys must stay legal: Text components read t('my.key').
	if def.AdditionalProperties.Type != "string" {
		t.Errorf("additionalProperties.type = %q, want \"string\"", def.AdditionalProperties.Type)
	}

	want := map[string]bool{}
	for _, tokens := range defaultI18nTokens {
		for key := range tokens {
			want[key] = true
		}
	}

	for key := range want {
		if _, ok := def.Properties[key]; !ok {
			t.Errorf("token %q is in defaultI18nTokens but not in $defs.internationalizationContent", key)
		}
	}
	for key, prop := range def.Properties {
		if !want[key] {
			t.Errorf("$defs.internationalizationContent declares %q, which is not in defaultI18nTokens", key)
		}
		if prop.Type != "string" {
			t.Errorf("token %q: type = %q, want \"string\"", key, prop.Type)
		}
		if prop.Description == "" {
			t.Errorf("token %q has no description", key)
		}
	}
}

// Proves the schema, the token table and the scaffolder agree end to end, and
// that values like "↑", "❖", "Δ{%}", " " and ", " survive the YAML round trip.
func TestBuildInternationalizationDocument_DefaultsValidateAgainstSchema(t *testing.T) {
	for _, code := range defaultI18nLocales() {
		t.Run(code, func(t *testing.T) {
			data := &InternationalizationManifestData{
				Name:    "labels_" + code,
				Code:    code,
				Content: map[string]string{},
			}
			if err := applyI18nDefaultTokens(data); err != nil {
				t.Fatalf("applyI18nDefaultTokens: %v", err)
			}

			out, err := yaml.Marshal(buildInternationalizationDocument(*data))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := schema.Validate(out); err != nil {
				t.Errorf("`bino add i18n --code %s --defaults` output fails validation:\n%v", code, err)
			}
		})
	}
}
