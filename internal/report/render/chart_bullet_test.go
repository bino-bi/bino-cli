package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestChartBullet_BareTokenAttrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartBullet", json.RawMessage(`{
		"dataset": "kpis",
		"actual": "ac1",
		"target": "pl1"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	if !strings.HasPrefix(html, "<bn-chart-bullet") || !strings.HasSuffix(html, "></bn-chart-bullet>") {
		t.Fatalf("expected bn-chart-bullet element, got:\n%s", html)
	}
	for _, want := range []string{"datasets='kpis'", "actual='ac1'", "target='pl1'"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
	// Bare tokens must not be emitted as JSON strings.
	if strings.Contains(html, "actual='&#34;") {
		t.Errorf("bare token emitted with JSON quotes:\n%s", html)
	}
	// Omitted optionals must not emit attributes.
	for _, absent := range []string{"ranges=", "normalize=", "variances=", "level=", "order=", "order-direction=", "limit=", "labels=", "scale=", "chart-title=", "filter="} {
		if strings.Contains(html, absent) {
			t.Errorf("expected no %s attribute, got:\n%s", absent, html)
		}
	}
}

func TestChartBullet_ObjectAttrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartBullet", json.RawMessage(`{
		"dataset": ["kpis", "$erp"],
		"chartTitle": "KPI overview vs. plan",
		"actual": "ac1",
		"target": {"measure": "pl1", "label": "Plan", "unit": "EUR k"},
		"ranges": [0.6, 0.9],
		"normalize": "none",
		"variances": "none",
		"level": "category",
		"order": "ac1",
		"orderDirection": "desc",
		"limit": 0,
		"labels": {"show": "auto", "decimals": 0},
		"scale": "none",
		"selectedStyle": "kpi-colors",
		"ruleset": "inherited-page"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	wants := []string{
		"datasets='kpis,$erp'",
		"chart-title='KPI overview vs. plan'",
		"actual='ac1'",
		// Object props are compact JSON; writeAttr escapes double quotes.
		"target='{&#34;measure&#34;:&#34;pl1&#34;,&#34;label&#34;:&#34;Plan&#34;,&#34;unit&#34;:&#34;EUR k&#34;}'",
		"ranges='[0.6,0.9]'",
		"normalize='none'",
		"variances='none'",
		"level='category'",
		"order='ac1'",
		"order-direction='desc'",
		"limit='0'",
		"labels='{&#34;show&#34;:&#34;auto&#34;,&#34;decimals&#34;:0}'",
		"scale='none'",
		"selected-style='kpi-colors'",
		"ruleset='inherited-page'",
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}

func TestChartBullet_AutoDetectOmitsAttrs(t *testing.T) {
	html, err := ComponentFromSpec("ChartBullet", json.RawMessage(`{
		"dataset": "kpis"
	}`), nil)
	if err != nil {
		t.Fatalf("ComponentFromSpec failed: %v", err)
	}

	// Omitted actual/target auto-detect in the engine — no attributes emitted.
	for _, absent := range []string{"actual=", "target="} {
		if strings.Contains(html, absent) {
			t.Errorf("expected no %s attribute, got:\n%s", absent, html)
		}
	}
	if !strings.Contains(html, "datasets='kpis'") {
		t.Errorf("expected datasets attribute, got:\n%s", html)
	}
}

func TestChartBullet_LayoutChild(t *testing.T) {
	bulletHTML := renderPageWithChild(t, "ChartBullet", `{"dataset": "kpis", "actual": "ac1", "target": "pl1"}`)
	if !strings.Contains(bulletHTML, "<bn-chart-bullet") {
		t.Fatalf("expected bn-chart-bullet in page HTML, got:\n%s", bulletHTML)
	}
}

func TestChartBullet_RefWithOverride(t *testing.T) {
	ctx := context.Background()

	bulletDoc := makeTestDoc("ChartBullet", "kpis", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartBullet",
		"metadata": {"name": "kpis"},
		"spec": {
			"dataset": "kpi_data",
			"actual": "ac1",
			"target": "pl1",
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
					"kind": "ChartBullet",
					"ref": "kpis",
					"spec": {"chartTitle": "Override Title"}
				}
			]
		}
	}`))

	docs := []config.Document{bulletDoc, layoutPageDoc}
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)
	if !strings.Contains(html, `chart-title='Override Title'`) {
		t.Fatalf("expected overridden chart title in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `target='pl1'`) {
		t.Fatalf("expected target from referenced document in HTML, got:\n%s", html)
	}
}
