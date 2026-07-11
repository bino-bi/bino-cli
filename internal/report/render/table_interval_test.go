package render

import (
	"strings"
	"testing"
)

func TestTableInterval_RenderedAttribute(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"interval": "year"
	}`)

	want := `interval='year'`
	if !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

func TestTableInterval_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test"}`)

	if strings.Contains(html, "interval=") {
		t.Fatalf("expected no interval attribute in HTML, got:\n%s", html)
	}
}
