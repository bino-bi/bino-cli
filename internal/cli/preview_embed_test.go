package cli

import (
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestPageFormatAndOrientation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		raw             string
		wantFormat      string
		wantOrientation string
	}{
		{
			name:            "explicit format and orientation",
			raw:             `{"kind":"LayoutPage","spec":{"pageFormat":"a4","pageOrientation":"portrait"}}`,
			wantFormat:      "a4",
			wantOrientation: "portrait",
		},
		{
			name:            "missing format falls back to defaults",
			raw:             `{"kind":"LayoutPage","spec":{}}`,
			wantFormat:      config.DefaultArtefactFormat,
			wantOrientation: config.DefaultArtefactOrientation,
		},
		{
			name:            "empty raw falls back to defaults",
			raw:             ``,
			wantFormat:      config.DefaultArtefactFormat,
			wantOrientation: config.DefaultArtefactOrientation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := config.Document{Kind: "LayoutPage", Name: "p", Raw: []byte(tt.raw)}
			gotF, gotO := pageFormatAndOrientation(doc)
			if gotF != tt.wantFormat {
				t.Errorf("format = %q, want %q", gotF, tt.wantFormat)
			}
			if gotO != tt.wantOrientation {
				t.Errorf("orientation = %q, want %q", gotO, tt.wantOrientation)
			}
		})
	}
}

// TestSyntheticPageArtefactAdoptsPageFormat guards the fix for the empty-context
// bug: the synthetic artefact must adopt the page's own pageFormat, otherwise the
// artefact-vs-page format filter in renderLayoutPage drops the page and the
// /__embedding response renders an empty <bn-context>.
func TestSyntheticPageArtefactAdoptsPageFormat(t *testing.T) {
	t.Parallel()

	page := config.Document{
		Kind: "LayoutPage",
		Name: "bn201_example_table_and_text",
		Raw:  []byte(`{"kind":"LayoutPage","spec":{"pageFormat":"a4","pageOrientation":"landscape"}}`),
	}
	art := syntheticPageArtefact(page)

	if art.Spec.Format != "a4" {
		t.Errorf("synthetic artefact format = %q, want %q (the page format must win, else the page is filtered out)", art.Spec.Format, "a4")
	}
	if art.Spec.Orientation != "landscape" {
		t.Errorf("synthetic artefact orientation = %q, want %q", art.Spec.Orientation, "landscape")
	}
	if len(art.Spec.LayoutPages) != 1 || art.Spec.LayoutPages[0].Page != page.Name {
		t.Errorf("synthetic artefact must reference exactly page %q, got %+v", page.Name, art.Spec.LayoutPages)
	}
	if art.Document.Kind != "ReportArtefact" {
		t.Errorf("synthetic artefact kind = %q, want ReportArtefact", art.Document.Kind)
	}
}
