package render

import (
	"encoding/json"
	"strings"
	"testing"
)

// The template engine removed level, order, order-direction and limit from the
// time chart. The schema still accepts the four fields so existing reports
// keep building, but none of them may reach the HTML.
func TestChartTime_FieldsWithoutEffectAreNotWritten(t *testing.T) {
	html, err := ComponentFromSpec("ChartTime", json.RawMessage(`{
		"dataset": "revenue",
		"dateInterval": "month",
		"level": "category",
		"order": "ac1",
		"orderDirection": "desc",
		"limit": 12,
		"maxBars": 24,
		"intervalSpanLimit": 6,
		"scenarios": ["ac1", "pp1"]
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec: %v", err)
	}

	if !strings.HasPrefix(html, "<bn-chart-time") {
		t.Fatalf("expected bn-chart-time element, got:\n%s", html)
	}
	for _, absent := range []string{" level=", " order=", " order-direction=", " limit="} {
		if strings.Contains(html, absent) {
			t.Errorf("expected no%s attribute, got:\n%s", absent, html)
		}
	}
	for _, want := range []string{"datasets='revenue'", "date-interval='month'", "max-bars='24'", "interval-span-limit='6'", "scenarios='ac1,pp1'"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}
