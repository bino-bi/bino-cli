package pipeline

import (
	"strings"
	"testing"
)

func TestDefaultDocumentHeaderTemplate(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "plain title",
			title: "Quarterly Report",
			want:  "Quarterly Report",
		},
		{
			name:  "html special characters escaped",
			title: `Q1 <Sales> & "Marketing"`,
			want:  "Q1 &lt;Sales&gt; &amp; &quot;Marketing&quot;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultDocumentHeaderTemplate(tt.title)
			if !strings.Contains(got, tt.want) {
				t.Errorf("DefaultDocumentHeaderTemplate(%q) = %q, want it to contain %q", tt.title, got, tt.want)
			}
			if strings.Contains(got, tt.title) && tt.title != tt.want {
				t.Errorf("DefaultDocumentHeaderTemplate(%q) contains unescaped title", tt.title)
			}
		})
	}
}

func TestDefaultDocumentFooterTemplate(t *testing.T) {
	got := DefaultDocumentFooterTemplate()
	for _, class := range []string{`class="date"`, `class="pageNumber"`, `class="totalPages"`} {
		if !strings.Contains(got, class) {
			t.Errorf("DefaultDocumentFooterTemplate() missing %s", class)
		}
	}
}

func TestTOCFooterTemplate(t *testing.T) {
	got := tocFooterTemplate()
	if !strings.Contains(got, `class="date"`) {
		t.Errorf("tocFooterTemplate() missing date class")
	}
	// Roman numerals are stamped by pdfcpu — the Chrome footer must not
	// render its own page numbers.
	if strings.Contains(got, "pageNumber") {
		t.Errorf("tocFooterTemplate() must not contain a pageNumber span")
	}
}

func TestDocumentTOCPDFOptionsProgress(t *testing.T) {
	var got []string
	opts := DocumentTOCPDFOptions{Progress: func(msg string) { got = append(got, msg) }}
	opts.progress("phase 1")
	if len(got) != 1 || got[0] != "phase 1" {
		t.Errorf("progress callback not invoked: %v", got)
	}

	// nil Progress must not panic
	var noCallback DocumentTOCPDFOptions
	noCallback.progress("ignored")
}
