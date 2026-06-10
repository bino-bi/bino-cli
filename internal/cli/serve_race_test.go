package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// writeServeRaceFixture creates a minimal report bundle whose dataset SQL and
// layout text both depend on the request-scoped ${REGION} variable.
func writeServeRaceFixture(t *testing.T) string {
	t.Helper()
	workdir := t.TempDir()

	files := map[string]string{
		// The ${REGION} variable lands in the DATASOURCE content, i.e. in the
		// shared session's view definition for "revenue_data" — the exact
		// cross-request collision surface of Gap #2. ephemeral: true plus the
		// declared dependency below force the dataset cache to be skipped, so
		// every request re-creates the view and re-executes the query.
		"data.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: revenue_data
spec:
  type: inline
  ephemeral: true
  content:
    - category: "${REGION}"
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
  name: race-page
spec:
  pageLayout: 2x2
  titleBusinessUnit: "Test Corp"
  titleMeasures:
    - name: "Revenue"
      unit: "EUR k"
  titleScenarios: "AC, PY"
  footerText: "Test Corp - Race Test"
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
        name: marker_text
      spec:
        value: "Region marker: ${REGION}"
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
`,
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: race-report
spec:
  format: xga
  orientation: landscape
  language: en
  filename: race-report.pdf
  title: "Race Test Report"
  layoutPages:
    - race-page
`,
		"live.yaml": `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: race-dash
spec:
  title: Race Dashboard
  routes:
    /:
      artefact: race-report
      queryParams:
        - name: REGION
        - name: SALT
          default: "0"
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

var serveContextB64Re = regexp.MustCompile(`"initialContextBase64":"([^"]*)"`)

// decodeServeContext extracts and decodes the base64-embedded context HTML
// from a serve response body. Returns an error instead of failing the test
// because it runs on spawned goroutines where t.Fatal is not allowed.
func decodeServeContext(body string) (string, error) {
	m := serveContextB64Re.FindStringSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("response body has no initialContextBase64 field")
	}
	decoded, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return "", fmt.Errorf("decode context base64: %w", err)
	}
	return string(decoded), nil
}

// decodeServeInlinePayload decodes a "hash:base64(gzip(data))" inline payload
// from a bn-dataset element body (see render.CompressContent).
func decodeServeInlinePayload(payload string) (string, error) {
	_, encoded, found := strings.Cut(strings.TrimSpace(payload), ":")
	if !found {
		return "", fmt.Errorf("payload has no hash prefix: %q", payload)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode payload: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("gzip open payload: %w", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("gzip read payload: %w", err)
	}
	return string(decoded), nil
}

var serveDatasetRe = regexp.MustCompile(`<bn-dataset[^>]*name='filtered_revenue'[^>]*>([^<]*)</bn-dataset>`)

// TestServeRoutes_ParallelParamDivergentRequests is the Gap #2 regression
// test: two clients hammer the same route concurrently with divergent
// ${REGION} values through the shared DuckDB session. Run under -race this
// catches unsynchronized session access; the body assertions catch the
// wrong-data failure mode where one request's CREATE OR REPLACE VIEW (or
// reloaded documents) bleed into the other request's render.
func TestServeRoutes_ParallelParamDivergentRequests(t *testing.T) {
	ctx := context.Background()
	workdir := writeServeRaceFixture(t)

	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{})
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	liveArtefacts, err := config.CollectLiveArtefacts(docs)
	if err != nil {
		t.Fatalf("collect live artefacts: %v", err)
	}
	liveArtefact := config.FindLiveArtefact(liveArtefacts, "race-dash")
	if liveArtefact == nil {
		t.Fatal("live artefact race-dash not found")
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
	fn := routeSetup.RouteMap["/"]
	if fn == nil {
		t.Fatal("route / not registered")
	}

	regions := []struct {
		name  string // also the category value in this request's view rows
		other string // the other region's category, must never appear
	}{
		{name: "DACH", other: "Nordics"},
		{name: "Nordics", other: "DACH"},
	}

	const iterations = 5
	var wg sync.WaitGroup
	for _, region := range regions {
		wg.Go(func() {
			for i := 0; i < iterations; i++ {
				// A unique SALT per request defeats the render cache so every
				// request exercises document reload + dataset execution on the
				// shared session.
				salt := fmt.Sprintf("%s-%d", region.name, i)
				reqCtx := httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
					Path:     "/",
					RawQuery: "REGION=" + region.name + "&SALT=" + salt,
					Query:    url.Values{"REGION": {region.name}, "SALT": {salt}},
				})

				body, contentType, err := fn(reqCtx)
				if err != nil {
					t.Errorf("region %s request %d: %v", region.name, i, err)
					return
				}
				if !strings.Contains(contentType, "text/html") {
					t.Errorf("region %s request %d: content type %q", region.name, i, contentType)
					return
				}

				contextHTML, err := decodeServeContext(string(body))
				if err != nil {
					t.Errorf("region %s request %d: %v", region.name, i, err)
					return
				}
				combined := string(body) + contextHTML

				// Documents must have been reloaded with THIS request's params.
				if !strings.Contains(combined, "Region marker: "+region.name) {
					t.Errorf("region %s request %d: own region marker missing", region.name, i)
				}
				if strings.Contains(combined, "Region marker: "+region.other) {
					t.Errorf("region %s request %d: response contains the other request's region marker", region.name, i)
				}

				// The dataset executed on the shared session must hold THIS
				// request's rows, not the concurrent request's view.
				m := serveDatasetRe.FindStringSubmatch(combined)
				if m == nil {
					t.Errorf("region %s request %d: filtered_revenue dataset element missing", region.name, i)
					continue
				}
				payload, err := decodeServeInlinePayload(m[1])
				if err != nil {
					t.Errorf("region %s request %d: %v", region.name, i, err)
					continue
				}
				if !strings.Contains(payload, region.name) {
					t.Errorf("region %s request %d: dataset payload missing own category %s: %s", region.name, i, region.name, payload)
				}
				if strings.Contains(payload, region.other) {
					t.Errorf("region %s request %d: dataset payload contains other request's category %s: %s", region.name, i, region.other, payload)
				}
			}
		})
	}
	wg.Wait()
}
