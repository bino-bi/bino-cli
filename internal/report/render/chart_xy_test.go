package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestChartScatter_BareTokenAttrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartScatter", json.RawMessage(`{
		"dataset": "products",
		"x": "ac1",
		"y": "drac1_pp1"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	if !strings.HasPrefix(html, "<bn-chart-scatter") || !strings.HasSuffix(html, "></bn-chart-scatter>") {
		t.Fatalf("expected bn-chart-scatter element, got:\n%s", html)
	}
	for _, want := range []string{"datasets='products'", "x='ac1'", "y='drac1_pp1'"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
	// Bare tokens must not be emitted as JSON strings.
	if strings.Contains(html, "x='&#34;") {
		t.Errorf("bare token emitted with JSON quotes:\n%s", html)
	}
	// Omitted optionals must not emit attributes.
	for _, absent := range []string{"iso=", "facet=", "labels=", "legend=", "limit=", "scale=", "aspect=", "level=", "series-level=", "chart-title=", "filter=", "share=", "compare-with="} {
		if strings.Contains(html, absent) {
			t.Errorf("expected no %s attribute, got:\n%s", absent, html)
		}
	}
}

func TestChartScatter_ObjectAttrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartScatter", json.RawMessage(`{
		"dataset": ["products", "$erp"],
		"chartTitle": "Margin vs. Net sales",
		"x": {"measure": "ac1", "label": "Margin", "min": 0, "max": 40},
		"y": "ac2",
		"iso": {"values": [100, 200], "label": "Gross profit"},
		"level": "category",
		"seriesLevel": "rowgroup",
		"facet": {"level": "rowgroup", "columns": 3},
		"labels": {"points": "auto", "max": 10},
		"legend": {"position": "bottom"},
		"aspect": "21:9",
		"limit": 0,
		"scale": "none",
		"selectedStyle": "portfolio-colors",
		"ruleset": "inherited-page"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	wants := []string{
		"datasets='products,$erp'",
		"chart-title='Margin vs. Net sales'",
		// Object props are compact JSON; writeAttr escapes double quotes.
		"x='{&#34;measure&#34;:&#34;ac1&#34;,&#34;label&#34;:&#34;Margin&#34;,&#34;min&#34;:0,&#34;max&#34;:40}'",
		"y='ac2'",
		"iso='{&#34;values&#34;:[100,200],&#34;label&#34;:&#34;Gross profit&#34;}'",
		"level='category'",
		"series-level='rowgroup'",
		"facet='{&#34;level&#34;:&#34;rowgroup&#34;,&#34;columns&#34;:3}'",
		"labels='{&#34;points&#34;:&#34;auto&#34;,&#34;max&#34;:10}'",
		"legend='{&#34;position&#34;:&#34;bottom&#34;}'",
		"aspect='21:9'",
		"limit='0'",
		"scale='none'",
		"selected-style='portfolio-colors'",
		"ruleset='inherited-page'",
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}

func TestChartBubble_Attrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartBubble", json.RawMessage(`{
		"dataset": "business_units",
		"x": "ac1",
		"y": "ac2",
		"size": {"measure": "ac3", "label": "Net sales", "group": "netsales_area"},
		"share": "ac4",
		"compareWith": "pp"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	if !strings.HasPrefix(html, "<bn-chart-bubble") || !strings.HasSuffix(html, "></bn-chart-bubble>") {
		t.Fatalf("expected bn-chart-bubble element, got:\n%s", html)
	}
	wants := []string{
		"datasets='business_units'",
		"x='ac1'",
		"y='ac2'",
		"size='{&#34;measure&#34;:&#34;ac3&#34;,&#34;label&#34;:&#34;Net sales&#34;,&#34;group&#34;:&#34;netsales_area&#34;}'",
		"share='ac4'",
		"compare-with='pp'",
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}

func TestChartXY_LayoutChildren(t *testing.T) {
	scatterHTML := renderPageWithChild(t, "ChartScatter", `{"dataset": "products", "x": "ac1", "y": "ac2"}`)
	if !strings.Contains(scatterHTML, "<bn-chart-scatter") {
		t.Fatalf("expected bn-chart-scatter in page HTML, got:\n%s", scatterHTML)
	}

	bubbleHTML := renderPageWithChild(t, "ChartBubble", `{"dataset": "units", "x": "ac1", "y": "ac2", "size": "ac3"}`)
	if !strings.Contains(bubbleHTML, "<bn-chart-bubble") {
		t.Fatalf("expected bn-chart-bubble in page HTML, got:\n%s", bubbleHTML)
	}
}

func TestChartScatter_RefWithOverride(t *testing.T) {
	ctx := context.Background()

	scatterDoc := makeTestDoc("ChartScatter", "products", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartScatter",
		"metadata": {"name": "products"},
		"spec": {
			"dataset": "product_data",
			"x": "ac1",
			"y": "ac2",
			"chartTitle": "Original Title"
		}
	}`))

	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartScatter",
					"ref": "products",
					"spec": {"chartTitle": "Override Title"}
				}
			]
		}
	}`))

	docs := []config.Document{scatterDoc, layoutPageDoc}
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)
	if !strings.Contains(html, `chart-title='Override Title'`) {
		t.Fatalf("expected overridden chart title in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `x='ac1'`) {
		t.Fatalf("expected x from referenced document in HTML, got:\n%s", html)
	}
}
