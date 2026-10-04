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

// writeServeLayoutPagesFixture creates a bundle with one layoutPages route per
// selection case. The pages and the LiveReportArtefact share a file, so the
// loader keeps ${REGION}, a param of page regional, as text in the route
// entries. ${AREA} is no page param and the loader resolves it.
func writeServeLayoutPagesFixture(t *testing.T) string {
	t.Helper()
	workdir := t.TempDir()

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
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: eu_data
spec:
  query: SELECT * FROM sales
  dependencies:
    - sales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: us_data
spec:
  query: SELECT * FROM sales
  dependencies:
    - sales
`,
		"report.yaml": servePageDataPage("sales-eu", "eu_data") + "---\n" + servePageDataPage("sales-us", "us_data") + `---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: regional
  params:
    - name: REGION
      type: string
      default: "EU"
spec:
  children:
    - kind: Text
      spec:
        value: "REGION-IS-${REGION}"
---
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: dash
spec:
  title: Dash
  routes:
    /:
      layoutPages:
        - sales-us
        - sales-eu
    /reversed:
      layoutPages:
        - sales-eu
        - sales-us
    /glob:
      layoutPages:
        - "sales-*"
    /static:
      layoutPages:
        - page: regional
          params:
            REGION: US
    /twice:
      layoutPages:
        - page: regional
          params:
            REGION: US
        - page: regional
          params:
            REGION: APAC
    /renamed:
      layoutPages:
        - page: regional
          params:
            REGION: "${AREA}"
      queryParams:
        - name: AREA
          default: "EU"
    /same-name:
      layoutPages:
        - page: regional
          params:
            REGION: "${REGION}"
      queryParams:
        - name: REGION
          default: "EU"
    /by-name:
      layoutPages:
        - regional
      queryParams:
        - name: REGION
          default: "EU"
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

// TestServeRoutes_LayoutPagesSelection checks that a layoutPages route selects
// its pages like a ReportArtefact does: globs, entry params, a page listed
// more than once, and route order. Query params must still reach the page.
func TestServeRoutes_LayoutPagesSelection(t *testing.T) {
	ctx := context.Background()
	workdir := writeServeLayoutPagesFixture(t)

	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	liveArtefacts, err := config.CollectLiveArtefacts(docs)
	if err != nil {
		t.Fatalf("collect live artefacts: %v", err)
	}
	liveArtefact := config.FindLiveArtefact(liveArtefacts, "dash")
	if liveArtefact == nil {
		t.Fatal("live artefact dash not found")
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
		ArtefactMap:   map[string]config.Artifact{},
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

	const (
		eu = "data-bino-page='sales-eu'"
		us = "data-bino-page='sales-us'"
	)
	tests := []struct {
		name  string
		path  string
		query url.Values
		want  []string // must all be on the page, in this order
	}{
		{"pages come in route order", "/", nil, []string{us, eu}},
		// Same pages as "/": the cached render of "/" must not answer it.
		{"same pages in another order", "/reversed", nil, []string{eu, us}},
		{"glob expands to the matching pages", "/glob", nil, []string{eu, us}},
		{"entry params reach the page", "/static", nil, []string{"REGION-IS-US"}},
		{"page listed twice renders twice", "/twice", nil, []string{"REGION-IS-US", "REGION-IS-APAC"}},
		{"query param under another name in entry params", "/renamed", url.Values{"AREA": {"US"}}, []string{"REGION-IS-US"}},
		{"query param in entry params", "/same-name", url.Values{"REGION": {"US"}}, []string{"REGION-IS-US"}},
		{"query param named like a page param", "/by-name", url.Values{"REGION": {"US"}}, []string{"REGION-IS-US"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := routeSetup.RouteMap[tt.path]
			if fn == nil {
				t.Fatalf("route %s not registered", tt.path)
			}
			body, _, err := fn(httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
				Path:     tt.path,
				RawQuery: tt.query.Encode(),
				Query:    tt.query,
			}))
			if err != nil {
				t.Fatalf("render route %s: %v", tt.path, err)
			}
			contextHTML, err := decodeServeContext(string(body))
			if err != nil {
				t.Fatalf("decode context of %s: %v", tt.path, err)
			}

			last := -1
			for _, want := range tt.want {
				at := strings.Index(contextHTML, want)
				if at < 0 {
					t.Errorf("route %s: missing %s in:\n%s", tt.path, want, contextHTML)
					continue
				}
				if at < last {
					t.Errorf("route %s: %s is out of order, want %v in:\n%s", tt.path, want, tt.want, contextHTML)
				}
				last = at
			}
		})
	}
}
