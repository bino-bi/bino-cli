package markdown

import (
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func i18nTestDoc(t *testing.T, spec string) config.Document {
	t.Helper()
	return config.Document{
		Kind: "Internationalization",
		Name: "i18n",
		Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "Internationalization",
			"metadata": {"name": "i18n"},
			"spec": ` + spec + `
		}`),
	}
}

func TestNewRenderContext_CollectsI18nContent(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		wantValue     string
		wantNamespace string
	}{
		{
			name:          "object content with explicit namespace",
			spec:          `{"code": "de", "namespace": "external", "content": {"global.ac1": "Ist"}}`,
			wantValue:     `{"global.ac1": "Ist"}`,
			wantNamespace: "external",
		},
		{
			name:          "string content, namespace defaults to _system",
			spec:          `{"code": "de", "content": "{\"global.ac1\":\"Ist\"}"}`,
			wantValue:     `{"global.ac1":"Ist"}`,
			wantNamespace: "_system",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewRenderContext([]config.Document{i18nTestDoc(t, tt.spec)}, nil, nil, "v1.0.0")
			if len(rc.Internationalizations) != 1 {
				t.Fatalf("expected 1 i18n entry, got %d", len(rc.Internationalizations))
			}
			entry := rc.Internationalizations[0]
			if entry.Code != "de" {
				t.Errorf("Code = %q, want de", entry.Code)
			}
			if entry.Namespace != tt.wantNamespace {
				t.Errorf("Namespace = %q, want %q", entry.Namespace, tt.wantNamespace)
			}
			if entry.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", entry.Value, tt.wantValue)
			}
		})
	}
}

func TestNewRenderContext_SkipsInvalidI18nContent(t *testing.T) {
	rc := NewRenderContext([]config.Document{i18nTestDoc(t, `{"code": "de", "content": "not json"}`)}, nil, nil, "v1.0.0")
	if len(rc.Internationalizations) != 0 {
		t.Fatalf("expected invalid content to be skipped, got %v", rc.Internationalizations)
	}
}
