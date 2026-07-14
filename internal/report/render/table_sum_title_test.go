package render

import (
	"strings"
	"testing"
)

func TestTableSumTitle_Emitted(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test", "type": "sum", "sumTitle": "Total revenue"}`)

	if want := `sum-title='Total revenue'`; !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

// The engine dropped the table-title attribute in the same release that added
// sum-title; emitting the old name would silently lose the label.
func TestTableSumTitle_LegacyAttributeGone(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test", "type": "sum", "sumTitle": "Total revenue"}`)

	if strings.Contains(html, "table-title=") {
		t.Fatalf("expected no legacy table-title attribute in HTML, got:\n%s", html)
	}
}

func TestTableSumTitle_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test"}`)

	if strings.Contains(html, "sum-title=") {
		t.Fatalf("expected no sum-title attribute in HTML, got:\n%s", html)
	}
}
