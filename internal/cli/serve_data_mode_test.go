package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/pkg/duckdb"
)

// writeServeURLModeFixture creates a minimal static report bundle (no query
// params) for exercising the serve render path in url data mode.
func writeServeURLModeFixture(t *testing.T) string {
	t.Helper()
	workdir := t.TempDir()

	files := map[string]string{
		"data.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: revenue_data
spec:
  type: inline
  content:
    - category: "DACH"
      categoryIndex: 1
      operation: "+"
      ac1: 4250
      pp1: 3800
`,
		"datasets.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: filtered_revenue
spec:
  query: SELECT * FROM revenue_data
  dependencies:
    - revenue_data
`,
		"pages.yaml": `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: url-page
spec:
  pageLayout: 2x2
  titleBusinessUnit: "Test Corp"
  titleMeasures:
    - name: "Revenue"
      unit: "EUR k"
  titleScenarios: "AC, PY"
  footerText: "Test Corp - URL Mode Test"
  children:
    - kind: ChartStructure
      metadata:
        name: revenue_chart
      spec:
        dataset: filtered_revenue
        scenarios:
          - ac1
          - pp1
        level: category
        order: ac1
        orderDirection: desc
        chartTitle: "Revenue"
    - kind: Text
      metadata:
        name: filler_one
      spec:
        value: Filler one.
    - kind: Text
      metadata:
        name: filler_two
      spec:
        value: Filler two.
    - kind: Text
      metadata:
        name: filler_three
      spec:
        value: Filler three.
`,
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: url-report
spec:
  format: xga
  orientation: landscape
  language: en
  filename: url-report.pdf
  title: "URL Mode Report"
  layoutPages:
    - url-page
`,
		"live.yaml": `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: url-dash
spec:
  title: URL Mode Dashboard
  routes:
    /:
      artefact: url-report
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

var serveURLModeDatasetRe = regexp.MustCompile(`<bn-dataset[^>]*name='filtered_revenue'[^>]*>([^<]*)</bn-dataset>`)

// TestServeRoutes_URLModeEmitsRelativeDataURLs is the regression test for the
// bind-address data-URL bug: serve pinned url-mode dataset bodies to its bind
// address (http://127.0.0.1:<port>/...), so a client browsing via localhost —
// a different origin, and the data route sends no CORS headers — failed every
// data fetch and every chart showed "No Data". The plugin options serve wires
// into its routes (applyServeDataMode) must leave the base empty so bodies
// come out as relative, same-origin paths.
func TestServeRoutes_URLModeEmitsRelativeDataURLs(t *testing.T) {
	ctx := context.Background()
	workdir := writeServeURLModeFixture(t)

	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	liveArtefacts, err := config.CollectLiveArtefacts(docs)
	if err != nil {
		t.Fatalf("collect live artefacts: %v", err)
	}
	liveArtefact := config.FindLiveArtefact(liveArtefacts, "url-dash")
	if liveArtefact == nil {
		t.Fatal("live artefact url-dash not found")
	}

	artifacts, err := config.CollectArtefacts(docs)
	if err != nil {
		t.Fatalf("collect artefacts: %v", err)
	}
	artefactMap := make(map[string]config.Artifact, len(artifacts))
	for _, a := range artifacts {
		artefactMap[a.Document.Name] = a
	}

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Fatalf("duckdb options: %v", err)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		t.Fatalf("open duckdb session: %v", err)
	}
	defer session.Close()

	logger := logx.Nop()
	routeSetup, err := setupServeRoutes(serveRouteConfig{
		LiveArtefact:  *liveArtefact,
		ArtefactMap:   artefactMap,
		HookRunner:    hooks.NewRunner(hooks.Resolve(nil, nil, logger), logger, workdir),
		HookEnv:       hooks.HookEnv{Mode: "serve", Workdir: workdir},
		Logger:        logger,
		Workdir:       workdir,
		BaseDocs:      docs,
		EngineVersion: "v1.0.0",
		Session:       session,
		PluginOptions: applyServeDataMode(nil, render.DataModeURL),
	})
	if err != nil {
		t.Fatalf("setup serve routes: %v", err)
	}
	fn := routeSetup.RouteMap["/"]
	if fn == nil {
		t.Fatal("route / not registered")
	}

	reqCtx := httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{Path: "/"})
	body, contentType, err := fn(reqCtx)
	if err != nil {
		t.Fatalf("render route /: %v", err)
	}
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("content type = %q, want text/html", contentType)
	}

	contextHTML, err := decodeServeContext(string(body))
	if err != nil {
		t.Fatalf("decode context: %v", err)
	}
	combined := string(body) + contextHTML

	m := serveURLModeDatasetRe.FindStringSubmatch(combined)
	if m == nil {
		t.Fatal("filtered_revenue dataset element missing from rendered HTML")
	}
	payload := strings.TrimSpace(m[1])
	if !strings.HasPrefix(payload, "/__bino/data/dataset/") {
		t.Fatalf("url-mode dataset body must be a relative same-origin path, got %q", payload)
	}
	// The absolute bind-address base is exactly the regression: a client
	// loading the page via any other host name got a cross-origin data fetch.
	if strings.Contains(combined, "http://127.0.0.1") || strings.Contains(combined, "http://localhost") {
		t.Fatalf("rendered HTML pins data URLs to the bind address:\n%s", combined)
	}
}
