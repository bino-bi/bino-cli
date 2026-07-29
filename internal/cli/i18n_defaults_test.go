package cli

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

func TestDefaultI18nTokens(t *testing.T) {
	de, en := defaultI18nTokens["de"], defaultI18nTokens["en"]
	if len(de) == 0 || len(en) == 0 {
		t.Fatal("expected de and en token sets")
	}

	// Sentinel keys in the exact flattened form the engine stores them in.
	for _, key := range []string{"global.ac1", "global.ibcssymbol_delta_ac", "bn-title.SEPERATOR_WS", "bn-table.there_of"} { //nolint:misspell // engine key is spelled this way
		if _, ok := de[key]; !ok {
			t.Errorf("de is missing sentinel key %q", key)
		}
	}
	if de["bn-table.there_of"] != "davon" || en["bn-table.there_of"] != "there of" {
		t.Errorf("unexpected there_of labels: de=%q en=%q", de["bn-table.there_of"], en["bn-table.there_of"])
	}

	// Every locale ships the same key set. A key present in one bundle but not
	// the other makes t() return the raw key to the reader in that locale.
	for key := range de {
		if _, ok := en[key]; !ok {
			t.Errorf("key %q exists in de but not en", key)
		}
	}
	for key := range en {
		if _, ok := de[key]; !ok {
			t.Errorf("key %q exists in en but not de", key)
		}
	}
}

func TestApplyI18nDefaultTokens(t *testing.T) {
	data := &InternationalizationManifestData{
		Code:    "de",
		Content: map[string]string{"global.ac1": "Ist"},
	}
	if err := applyI18nDefaultTokens(data); err != nil {
		t.Fatalf("applyI18nDefaultTokens: %v", err)
	}
	if data.Content["global.ac1"] != "Ist" {
		t.Errorf("explicit content must win, got %q", data.Content["global.ac1"])
	}
	if data.Content["bn-table.there_of"] != "davon" {
		t.Errorf("expected defaults filled in, got %q", data.Content["bn-table.there_of"])
	}

	unsupported := &InternationalizationManifestData{Code: "fr", Content: map[string]string{}}
	if err := applyI18nDefaultTokens(unsupported); err == nil {
		t.Fatal("expected error for unsupported locale")
	} else if !strings.Contains(err.Error(), "de, en") {
		t.Errorf("error should list supported locales, got: %v", err)
	}
}

func TestBuildInternationalizationDocument_Namespace(t *testing.T) {
	doc := buildInternationalizationDocument(InternationalizationManifestData{
		Name:      "de_external",
		Code:      "de",
		Namespace: "external",
		Content:   map[string]string{"global.ac1": "Ist"},
	})
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed struct {
		Spec schema.InternationalizationSpec `yaml:"spec"`
	}
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Spec.Namespace != "external" {
		t.Errorf("Namespace = %q, want external", parsed.Spec.Namespace)
	}

	// Without a namespace the field is omitted entirely (renderer defaults to _system).
	doc = buildInternationalizationDocument(InternationalizationManifestData{
		Name: "de_default", Code: "de", Content: map[string]string{"global.ac1": "Ist"},
	})
	out, err = yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "namespace") {
		t.Errorf("expected namespace omitted when empty, got:\n%s", out)
	}
}
