package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/pkg/duckdb"
)

// servePageDataPage returns a LayoutPage with one Table bound to dataset.
func servePageDataPage(name, dataset string) string {
	return fmt.Sprintf(`apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: %s
spec:
  children:
    - kind: Table
      spec:
        dataset: %s
`, name, dataset)
}

// writeServePageDataFixture creates a bundle with two routes, each on its own
// page and DataSet, plus a standalone Table on a DataSource that no page uses.
func writeServePageDataFixture(t *testing.T) string {
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
kind: DataSource
metadata:
  name: secret_src
spec:
  type: inline
  content:
    - secret: 42
`,
		"datasets.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: a_data
spec:
  query: SELECT * FROM sales
  dependencies:
    - sales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: b_data
spec:
  query: SELECT * FROM sales
  dependencies:
    - sales
`,
		"pages.yaml": servePageDataPage("page-a", "a_data") + "---\n" + servePageDataPage("page-b", "b_data"),
		"secret.yaml": `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: secret_table
spec:
  dataset: $secret_src
`,
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: rep-a
spec:
  format: xga
  orientation: landscape
  language: en
  filename: rep-a.pdf
  title: "Report A"
  layoutPages:
    - page-a
`,
		"live.yaml": `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: page-data-dash
spec:
  title: Page Data Dashboard
  routes:
    /:
      artefact: rep-a
    /b:
      layoutPages:
        - page-b
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

// TestServeRoutes_EachRouteCarriesOnlyItsPageData checks that a served route
// sends the browser only the data its rendered components bind: not the
// DataSet of another route, and not the DataSource of a component on no page.
func TestServeRoutes_EachRouteCarriesOnlyItsPageData(t *testing.T) {
	for _, dataMode := range []string{render.DataModeInline, render.DataModeURL} {
		t.Run(dataMode, func(t *testing.T) {
			ctx := context.Background()
			workdir := writeServePageDataFixture(t)

			docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{})
			if err != nil {
				t.Fatalf("load docs: %v", err)
			}

			liveArtefacts, err := config.CollectLiveArtefacts(docs)
			if err != nil {
				t.Fatalf("collect live artefacts: %v", err)
			}
			liveArtefact := config.FindLiveArtefact(liveArtefacts, "page-data-dash")
			if liveArtefact == nil {
				t.Fatal("live artefact page-data-dash not found")
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
				PluginOptions: applyServeDataMode(nil, dataMode),
			})
			if err != nil {
				t.Fatalf("setup serve routes: %v", err)
			}

			for _, route := range []struct {
				path, own, other string
			}{
				{path: "/", own: "a_data", other: "b_data"},
				{path: "/b", own: "b_data", other: "a_data"},
			} {
				fn := routeSetup.RouteMap[route.path]
				if fn == nil {
					t.Fatalf("route %s not registered", route.path)
				}
				reqCtx := httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{Path: route.path})
				body, _, err := fn(reqCtx)
				if err != nil {
					t.Fatalf("render route %s: %v", route.path, err)
				}
				contextHTML, err := decodeServeContext(string(body))
				if err != nil {
					t.Fatalf("decode context of %s: %v", route.path, err)
				}
				combined := string(body) + contextHTML

				want := []string{"<bn-dataset name='" + route.own + "'"}
				notWant := []string{
					"name='" + route.other + "'",
					"<bn-datasource name='secret_src'",
					"/__bino/data/datasource/secret_src",
				}
				if dataMode == render.DataModeURL {
					want = append(want, "/__bino/data/dataset/"+route.own)
					notWant = append(notWant, "/__bino/data/dataset/"+route.other)
				}
				for _, s := range want {
					if !strings.Contains(combined, s) {
						t.Errorf("route %s: missing %q", route.path, s)
					}
				}
				for _, s := range notWant {
					if strings.Contains(combined, s) {
						t.Errorf("route %s: must not contain %q", route.path, s)
					}
				}
			}
		})
	}
}
