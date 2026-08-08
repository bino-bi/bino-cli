package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/schema"
)

// wizardRoundTrip writes a wizard-built document through the validating write
// path, reads the file back, asserts it still passes schema.Validate, and
// returns the on-disk YAML. This is the shared loop for the build*Document
// round-trip tests: what the wizard writes is what a build would consume.
func wizardRoundTrip(t *testing.T, doc any, filename string) string {
	t.Helper()
	dir := t.TempDir()

	var err error
	switch d := doc.(type) {
	case *schema.Document:
		err = WriteSchemaDocument(d, dir, filename, false, discardCmd().OutOrStdout())
	case map[string]any:
		err = WriteRawDocument(d, dir, filename, false, discardCmd().OutOrStdout())
	default:
		t.Fatalf("unsupported document type %T", doc)
	}
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("read written manifest: %v", err)
	}
	if err := schema.Validate(content); err != nil {
		t.Fatalf("written manifest failed schema.Validate:\n%s\nerror: %v", content, err)
	}
	return string(content)
}

// assertContainsAll asserts every want substring occurs in got.
func assertContainsAll(t *testing.T, got string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildTableDocument(t *testing.T) {
	data := TableManifestData{
		Name:        "sales_table",
		Description: "Monthly sales",
		Constraints: []string{"mode == build"},
		Dataset:     "sales_data",
		Type:        "sum",
		SumTitle:    "Total",
	}
	got := wizardRoundTrip(t, buildTableDocument(data), "table.yaml")
	assertContainsAll(t, got, []string{
		"kind: Table",
		"name: sales_table",
		"description: Monthly sales",
		"mode == build",
		"dataset: $sales_data",
		"type: sum",
		"sumTitle: Total",
	})
}

func TestBuildChartStructureDocument(t *testing.T) {
	t.Run("dataset and title survive", func(t *testing.T) {
		data := ChartStructureManifestData{
			Name:    "sales_by_region",
			Dataset: "region_sales",
			Title:   "Sales by Region",
		}
		got := wizardRoundTrip(t, buildChartStructureDocument(data), "chart.yaml")
		assertContainsAll(t, got, []string{
			"kind: ChartStructure",
			"name: sales_by_region",
			"dataset: $region_sales",
			"chartTitle: Sales by Region",
		})
	})

	// Canary: chartStructureSpecBase is closed and has no `type` property,
	// yet the wizard offers --type bar/pie/donut/radar — a document carrying
	// it is rejected by the validating write path instead of landing on
	// disk. If this starts failing, the schema gained the property and the
	// wizard prompt should be revisited.
	t.Run("the wizard's chart type is rejected at write time", func(t *testing.T) {
		data := ChartStructureManifestData{
			Name:      "typed_chart",
			Dataset:   "region_sales",
			ChartType: "bar",
		}
		err := WriteSchemaDocument(buildChartStructureDocument(data), t.TempDir(), "typed_chart.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the unsupported type property")
		}
	})
}

func TestBuildChartTimeDocument(t *testing.T) {
	data := ChartTimeManifestData{
		Name:    "sales_trend",
		Dataset: "monthly_sales",
		Title:   "Sales Trend",
	}
	got := wizardRoundTrip(t, buildChartTimeDocument(data), "charttime.yaml")
	assertContainsAll(t, got, []string{
		"kind: ChartTime",
		"name: sales_trend",
		"dataset: $monthly_sales",
		"chartTitle: Sales Trend",
	})
}

func TestBuildChartScatterDocument(t *testing.T) {
	data := ChartScatterManifestData{
		Name:    "product_margin",
		Dataset: "products",
		X:       "ac1",
		Y:       "dac1_pp1",
		Title:   "Margin vs. net sales",
	}
	got := wizardRoundTrip(t, buildChartScatterDocument(data), "scatter.yaml")
	assertContainsAll(t, got, []string{
		"kind: ChartScatter",
		"dataset: $products",
		"x: ac1",
		// yaml.v3 quotes the key: y is a YAML 1.1 boolean literal.
		`"y": dac1_pp1`,
		"chartTitle: Margin vs. net sales",
	})
}

func TestBuildChartBubbleDocument(t *testing.T) {
	data := ChartBubbleManifestData{
		Name:    "portfolio",
		Dataset: "business_units",
		X:       "ac1",
		Y:       "ac2",
		Size:    "ac3",
	}
	got := wizardRoundTrip(t, buildChartBubbleDocument(data), "bubble.yaml")
	assertContainsAll(t, got, []string{
		"kind: ChartBubble",
		"dataset: $business_units",
		"x: ac1",
		// yaml.v3 quotes the key: y is a YAML 1.1 boolean literal.
		`"y": ac2`,
		"size: ac3",
	})
}

func TestBuildChartBulletDocument(t *testing.T) {
	t.Run("explicit measures survive", func(t *testing.T) {
		data := ChartBulletManifestData{
			Name:    "kpi_overview",
			Dataset: "kpis",
			Actual:  "ac1",
			Target:  "pl1",
			Title:   "KPI overview vs. plan",
		}
		got := wizardRoundTrip(t, buildChartBulletDocument(data), "bullet.yaml")
		assertContainsAll(t, got, []string{
			"kind: ChartBullet",
			"dataset: $kpis",
			"actual: ac1",
			"target: pl1",
			"chartTitle: KPI overview vs. plan",
		})
	})

	t.Run("empty measures stay omitted for auto-detection", func(t *testing.T) {
		data := ChartBulletManifestData{Name: "auto_bullet", Dataset: "kpis"}
		got := wizardRoundTrip(t, buildChartBulletDocument(data), "bullet_auto.yaml")
		for _, forbidden := range []string{"actual:", "target:"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("%q must be omitted so the engine auto-detects:\n%s", forbidden, got)
			}
		}
	})
}
