package cli

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// writeServeSelectOptionsFixture creates a bundle whose REGION select takes its
// options from a DataSet that filters by ${REGION} and returns every region
// when the variable is empty. Both route kinds declare the same parameters.
func writeServeSelectOptionsFixture(t *testing.T) string {
	t.Helper()
	workdir := t.TempDir()

	const queryParams = `      queryParams:
        - name: REGION
          type: select
          default: "DACH"
          options:
            dataset: region_options
            valueColumn: category
        - name: YEAR
          type: number
          default: "2024"
`
	files := map[string]string{
		"data.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
spec:
  type: inline
  content:
    - category: "DACH"
      ac1: 4250
    - category: "Nordics"
      ac1: 3100
    - category: "Iberia"
      ac1: 2700
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: region_options
spec:
  query: SELECT category FROM sales WHERE ('${REGION}' = '' OR category = '${REGION}')
  dependencies:
    - sales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: region_sales
spec:
  query: SELECT * FROM sales WHERE category = '${REGION}'
  dependencies:
    - sales
`,
		"pages.yaml": servePageDataPage("region-page", "region_sales"),
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: region-report
spec:
  format: xga
  orientation: landscape
  language: en
  filename: region-report.pdf
  title: "Region Report"
  layoutPages:
    - region-page
`,
		"live.yaml": `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: options-dash
spec:
  title: Options Dashboard
  routes:
    /:
      artefact: region-report
` + queryParams + `    /pages:
      layoutPages:
        - region-page
` + queryParams,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

// TestServeRoutes_SelectOptionsUseRequestParams covers the options of a select
// parameter that come from a DataSet. They must be resolved with the
// parameters of the request on every answer. The page served from the render
// cache and the parameter form for a rejected value used the documents loaded
// at startup, where no parameter is set, so the option DataSet returned the
// rows of every region.
func TestServeRoutes_SelectOptionsUseRequestParams(t *testing.T) {
	ctx := context.Background()
	workdir := writeServeSelectOptionsFixture(t)

	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	liveArtefacts, err := config.CollectLiveArtefacts(docs)
	if err != nil {
		t.Fatalf("collect live artefacts: %v", err)
	}
	liveArtefact := config.FindLiveArtefact(liveArtefacts, "options-dash")
	if liveArtefact == nil {
		t.Fatal("live artefact options-dash not found")
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
	})
	if err != nil {
		t.Fatalf("setup serve routes: %v", err)
	}

	for _, path := range []string{"/", "/pages"} {
		fn := routeSetup.RouteMap[path]
		if fn == nil {
			t.Fatalf("route %s not registered", path)
		}
		get := func(query url.Values) string {
			t.Helper()
			body, _, err := fn(httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
				Path:     path,
				RawQuery: query.Encode(),
				Query:    query,
			}))
			if err != nil {
				t.Fatalf("route %s %s: %v", path, query.Encode(), err)
			}
			return string(body)
		}

		for _, tc := range []struct {
			name  string
			query url.Values
		}{
			{name: "first render", query: url.Values{"REGION": {"DACH"}}},
			{name: "cached page", query: url.Values{"REGION": {"DACH"}}},
			{name: "form for a rejected value", query: url.Values{"REGION": {"DACH"}, "YEAR": {"abc"}}},
		} {
			body := get(tc.query)
			if !strings.Contains(body, `"value":"DACH"`) {
				t.Errorf("route %s, %s: option DACH missing", path, tc.name)
			}
			for _, other := range []string{"Nordics", "Iberia"} {
				if strings.Contains(body, other) {
					t.Errorf("route %s, %s: page has the option %s of another region", path, tc.name, other)
				}
			}
		}
	}
}
