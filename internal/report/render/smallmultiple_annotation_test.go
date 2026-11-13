package render

import (
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func panelAnnotation(name, panel, point string) config.Document {
	target := `{"kind":"SmallMultiple","name":"sm","panel":"` + panel + `"`
	if point != "" {
		target += `,"point":` + point
	}
	return annotationDocument(name, `{"target":`+target+`},"title":"`+name+`"}`)
}

func TestSmallMultiple_AnnotationOnPanel(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup"}}`,
		panelAnnotation("cologne", "Cologne", `{"date":"2024-01-01","scenario":"ac1"}`))
	cells := spanCells(html)
	for _, c := range cells {
		want := ""
		if c.slot == "0-1" { // Berlin, Cologne, Munich, O'Hare
			want = "t1"
		}
		if got := c.cell["data-bino-annotation-target"]; got != want {
			t.Errorf("slot %s: target = %q, want %q", c.slot, got, want)
		}
	}
	if strings.Count(html, "data-bino-annotation-target=") != 1 {
		t.Errorf("want exactly one target in:\n%s", html)
	}
	items := layerItems(t, html)
	if len(items) != 1 || items[0].Target != "t1" || items[0].Label != "1" || items[0].Point == nil || items[0].Point.Date != "2024-01-01" {
		t.Fatalf("layer items = %+v", items)
	}
	if strings.Contains(attrPattern("bn-annotation-layer").FindString(html), "panel") {
		t.Errorf("panel must not reach the engine:\n%s", html)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestSmallMultiple_AnnotationOnPanelWithCard(t *testing.T) {
	html, _ := renderSpan(t, `{`+spanHead+`, "card": {}, "panels": {"by": "rowGroup"}}`,
		panelAnnotation("munich", "Munich", ""))
	want := regexp.MustCompile(`<div slot='1-0'><bn-layout-card[^>]*>\s*<div slot='card-slot-0'[^>]* data-bino-annotation-target='t1'>\s*<bn-chart-time`)
	if !want.MatchString(html) {
		t.Errorf("the target must be on the element in the card that holds the chart:\n%s", html)
	}
	if strings.Count(html, "data-bino-annotation-target=") != 1 {
		t.Errorf("want exactly one target in:\n%s", html)
	}
	if items := layerItems(t, html); len(items) != 1 || items[0].Target != "t1" {
		t.Errorf("layer items = %+v", items)
	}
}

func TestSmallMultiple_AnnotationPointUsesThePanelRows(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup"}}`,
		panelAnnotation("berlin", "Berlin", `{"date":"2025-01-01","scenario":"ac1"}`),
		panelAnnotation("cologne", "Cologne", `{"date":"2025-01-01","scenario":"ac1"}`))
	items := layerItems(t, html)
	if len(items) != 1 || items[0].Target != "t1" {
		t.Fatalf("only the Berlin mark can be placed, got %+v", items)
	}
	found := false
	for _, w := range warnings {
		found = found || (strings.Contains(w, "annotation-point-missing: annotation cologne on SmallMultiple sm panel \"Cologne\"") && strings.Contains(w, "no row of the dataset"))
	}
	if !found {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestSmallMultiple_AnnotationUnknownPanel(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup"}}`, panelAnnotation("nowhere", "Hamburg", ""))
	if strings.Contains(html, "data-bino-annotation-target") || len(layerItems(t, html)) != 0 {
		t.Errorf("no mark expected:\n%s", html)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w, `annotation-target-missing: SmallMultiple "sm" has no panel "Hamburg"`)
	}
	if !found {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestSmallMultiple_AnnotationWithoutPanel(t *testing.T) {
	html, _ := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup"}}`,
		annotationDocument("whole", `{"mark":"frame","target":{"kind":"SmallMultiple","name":"sm"}}`))
	if !regexp.MustCompile(`<div slot='slot-0'[^>]* data-bino-annotation-target='t1'>`).MatchString(html) {
		t.Errorf("the slot of the SmallMultiple must be the target:\n%s", html)
	}
	if strings.Count(html, "data-bino-annotation-target=") != 1 {
		t.Errorf("want exactly one target in:\n%s", html)
	}
}
