package cli

import (
	"context"
	"maps"
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
// entries, and replaces ${AREA} and ${ZONE} at startup. Page cover exists for
// serve and for build, and page sales-print for build only.
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
kind: LayoutPage
metadata:
  name: cover
  constraints:
    - mode==serve
spec:
  children:
    - kind: Text
      spec:
        value: "COVER-IS-SERVE"
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: cover
  constraints:
    - mode==build
spec:
  children:
    - kind: Text
      spec:
        value: "COVER-IS-BUILD"
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: sales-print
  constraints:
    - mode==build
spec:
  children:
    - kind: Text
      spec:
        value: "PRINT-ONLY"
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: budget
  params:
    - name: AMOUNT
      type: string
      default: "0"
spec:
  children:
    - kind: Text
      spec:
        value: "BUDGET-IS-$${AMOUNT}"
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
    /glob-query:
      layoutPages:
        - "sales-*"
      queryParams:
        - name: CAT
          default: "x"
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
    /pinned:
      layoutPages:
        - page: regional
          params:
            REGION: US
      queryParams:
        - name: REGION
          default: "EU"
    /area:
      layoutPages:
        - page: regional
          params:
            REGION: "${AREA}"
      queryParams:
        - name: AREA
          default: "DE"
        - name: ZONE
          default: "EU"
    /zone:
      layoutPages:
        - page: regional
          params:
            REGION: "${ZONE}"
      queryParams:
        - name: AREA
          default: "EU"
        - name: ZONE
          default: "EU"
    /placeholder:
      layoutPages:
        - page: regional
          params:
            REGION: "PIN-${data.kpi[0].ac1}"
      queryParams:
        - name: REGION
          default: "EU"
    /note:
      layoutPages:
        - page: regional
          params:
            NOTE: "${AREA}"
      queryParams:
        - name: AREA
          default: "EU"
    /note-param:
      layoutPages:
        - page: note
          params:
            X: "1"
      queryParams:
        - name: CAT
          default: "x"
    /cover:
      layoutPages:
        - cover
    /budget:
      layoutPages:
        - budget
      queryParams:
        - name: AMOUNT
          default: "0"
    /joined:
      layoutPages:
        - page: regional
          params:
            REGION: "${AREA}${ZONE}"
      queryParams:
        - name: AREA
          default: "EU"
        - name: ZONE
          default: ""
    /same-name:
      layoutPages:
        - page: regional
          params:
            REGION: "${REGION}"
      queryParams:
        - name: REGION
          default: "DE"
    /optional:
      layoutPages:
        - page: regional
          params:
            REGION: "${REGION}"
      queryParams:
        - name: REGION
          optional: true
    /by-name:
      layoutPages:
        - regional
      queryParams:
        - name: REGION
          default: "DE"
    /single:
      layoutPages: sales-eu
    /mixed:
      layoutPages:
        - page: regional
          params:
            REGION: US
        - "sales-*"
        - sales-eu
`,
	}
	// The query param ends the last line of this file, so a request value can
	// close the string and add a document.
	files["note.yaml"] = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: note
spec:
  children:
    - kind: Text
      spec:
        value: "NOTE-${CAT}"
`
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

// unsetFixtureEnv removes the variables that the fixtures use as query param
// and page param names. A reference without a value falls back to them.
func unsetFixtureEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"REGION", "AREA", "AREA_LABEL", "AREA__X", "ZONE", "AMOUNT", "CAT"} {
		t.Setenv(name, "") // restores the caller's value after the test
		os.Unsetenv(name)
	}
}

// TestServeRoutes_LayoutPagesSelection checks that a layoutPages route selects
// its pages like a ReportArtefact does: globs, entry params, a page listed
// more than once, and route order. Query params must still reach the page,
// and a request value must not change which pages the route renders.
func TestServeRoutes_LayoutPagesSelection(t *testing.T) {
	unsetFixtureEnv(t)
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

		LayoutPageTemplates: loadLayoutPageTemplates(ctx, logger, workdir, nil, *liveArtefact),
	})
	if err != nil {
		t.Fatalf("setup serve routes: %v", err)
	}

	// request returns the response body of a route and its decoded context.
	request := func(path string, query url.Values) (body, contextHTML string, err error) {
		fn := routeSetup.RouteMap[path]
		if fn == nil {
			t.Fatalf("route %s not registered", path)
		}
		raw, _, err := fn(httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
			Path:     path,
			RawQuery: query.Encode(),
			Query:    query,
		}))
		if err != nil {
			return "", "", err
		}
		contextHTML, err = decodeServeContext(string(raw))
		return string(raw), contextHTML, err
	}

	const (
		eu       = "data-bino-page='sales-eu'"
		us       = "data-bino-page='sales-us'"
		regional = "data-bino-page='regional"
	)
	areaAndZone := url.Values{"AREA": {"US"}, "ZONE": {"APAC"}}
	tests := []struct {
		name    string
		path    string
		query   url.Values
		warm    string   // route requested first with the same query, to fill the render cache
		want    []string // must all be on the page, in this order
		notWant []string
	}{
		{name: "pages come in route order", path: "/", want: []string{us, eu}},
		{name: "same pages in another order", path: "/reversed", warm: "/", want: []string{eu, us}},
		{name: "glob expands to the matching pages only", path: "/glob", want: []string{eu, us}, notWant: []string{regional, "PRINT-ONLY"}},
		{
			name: "of pages with the same name the one for serve renders", path: "/cover",
			want: []string{"COVER-IS-SERVE"}, notWant: []string{"COVER-IS-BUILD"},
		},
		{name: "single page name", path: "/single", want: []string{eu}, notWant: []string{us}},
		{name: "names, globs and entries with params in one list", path: "/mixed", want: []string{"REGION-IS-US", eu, us}},
		{name: "entry params reach the page", path: "/static", want: []string{"REGION-IS-US"}, notWant: []string{"REGION-IS-EU"}},
		{
			name: "page listed twice renders twice", path: "/twice",
			want: []string{"REGION-IS-US", "REGION-IS-APAC"}, notWant: []string{"REGION-IS-EU"},
		},
		{
			name: "entry param wins over a query param of the same name", path: "/pinned",
			query: url.Values{"REGION": {"APAC"}}, want: []string{"REGION-IS-US"}, notWant: []string{"REGION-IS-APAC"},
		},
		{
			name: "entry param with text for the browser stays as written", path: "/placeholder",
			query: url.Values{"REGION": {"APAC"}}, want: []string{"REGION-IS-PIN-${data.kpi[0].ac1}"}, notWant: []string{"REGION-IS-APAC"},
		},
		{
			name: "query param under another name in entry params", path: "/area",
			query: areaAndZone, want: []string{"REGION-IS-US"},
		},
		{
			name: "routes that pass different query params do not share a render", path: "/zone",
			query: areaAndZone, warm: "/area", want: []string{"REGION-IS-APAC"}, notWant: []string{"REGION-IS-US"},
		},
		{
			name: "query param in entry params", path: "/same-name",
			query: url.Values{"REGION": {"US"}}, want: []string{"REGION-IS-US"},
		},
		{name: "entry param without a value leaves the page default", path: "/optional", want: []string{"REGION-IS-EU"}},
		{
			name: "query param named like a page param", path: "/by-name",
			query: url.Values{"REGION": {"US"}}, want: []string{"REGION-IS-US"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.warm != "" {
				if _, _, err := request(tt.warm, tt.query); err != nil {
					t.Fatalf("request %s: %v", tt.warm, err)
				}
			}
			_, contextHTML, err := request(tt.path, tt.query)
			if err != nil {
				t.Fatalf("request %s: %v", tt.path, err)
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
			for _, notWant := range tt.notWant {
				if strings.Contains(contextHTML, notWant) {
					t.Errorf("route %s: must not contain %s in:\n%s", tt.path, notWant, contextHTML)
				}
			}
		})
	}

	// The reload for a request puts the request values into the manifest text.
	// This value closes the string of the entry param and adds a list entry.
	// The page does not declare NOTE, so the value does not reach the page.
	t.Run("request value cannot add pages to the route", func(t *testing.T) {
		_, contextHTML, err := request("/note", url.Values{"AREA": {"US\"\n        - \"sales-*"}})
		if err != nil {
			t.Fatalf("request /note: %v", err)
		}
		if !strings.Contains(contextHTML, regional) || strings.Contains(contextHTML, "data-bino-page='sales-") {
			t.Errorf("route /note must render page regional only:\n%s", contextHTML)
		}
	})

	// This value closes the string in note.yaml and adds a LayoutPage with a
	// name that the glob of the route matches.
	t.Run("request value cannot add a page with a new name to a glob", func(t *testing.T) {
		injected := "x\"\n---\napiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: sales-zzz\nspec:\n  children:\n    - kind: Text\n      spec:\n        value: \"INJECTED"
		_, contextHTML, err := request("/glob-query", url.Values{"CAT": {injected}})
		if err != nil {
			t.Fatalf("request /glob-query: %v", err)
		}
		if !strings.Contains(contextHTML, eu) || strings.Contains(contextHTML, "sales-zzz") || strings.Contains(contextHTML, "INJECTED") {
			t.Errorf("route /glob-query must render the pages of the manifest only:\n%s", contextHTML)
		}
	})

	// A page param is put into the JSON text of the page. This value ends the
	// string it sits in and adds a Table on a DataSet that the page does not use.
	t.Run("request value cannot add a component to the page", func(t *testing.T) {
		injected := `x"}},{"kind":"Table","spec":{"dataset":"us_data"}},{"kind":"Text","spec":{"value":"tail`
		body, contextHTML, err := request("/by-name", url.Values{"REGION": {injected}})
		if err != nil {
			t.Fatalf("request /by-name: %v", err)
		}
		if strings.Contains(body+contextHTML, "name='us_data'") || !strings.Contains(contextHTML, "REGION-IS-x") {
			t.Errorf("route /by-name must render the request value as text:\n%s", contextHTML)
		}
	})

	// Page params are expanded against the server environment. A request value
	// that could be read as a variable reference there must not become a page
	// param: the default of the query param is used, not the request value and
	// not an environment variable of the same name. The second value carries
	// the marker that the expansion itself turns into "${".
	t.Run("request value with a variable reference is not passed to the page", func(t *testing.T) {
		t.Setenv("BINO_TEST_SERVER_SECRET", "leaked")
		t.Setenv("AREA", "leaked")
		t.Setenv("REGION", "leaked")
		for _, value := range []string{
			"${BINO_TEST_SERVER_SECRET}",
			"\x00BINO_ESC_DOLLAR_BRACE\x00BINO_TEST_SERVER_SECRET}",
		} {
			for path, param := range map[string]string{"/by-name": "REGION", "/same-name": "REGION", "/area": "AREA"} {
				_, contextHTML, err := request(path, url.Values{param: {value}})
				if err != nil {
					t.Fatalf("request %s: %v", path, err)
				}
				if strings.Contains(contextHTML, "leaked") {
					t.Errorf("route %s: %q was expanded from the environment:\n%s", path, value, contextHTML)
				}
				if !strings.Contains(contextHTML, "REGION-IS-DE") {
					t.Errorf("route %s: %q did not leave the default of the query param:\n%s", path, value, contextHTML)
				}
			}
		}

		// Two harmless values that an entry param joins to a reference.
		_, contextHTML, err := request("/joined", url.Values{"AREA": {"$"}, "ZONE": {"{BINO_TEST_SERVER_SECRET}"}})
		if err != nil {
			t.Fatalf("request /joined: %v", err)
		}
		if strings.Contains(contextHTML, "leaked") || !strings.Contains(contextHTML, "REGION-IS-EU") {
			t.Errorf("route /joined: joined request values were expanded:\n%s", contextHTML)
		}

		// Page note declares no params. An entry param must not make the
		// selector expand it: the reload has put the request value into its text.
		_, contextHTML, err = request("/note-param", url.Values{"CAT": {"${BINO_TEST_SERVER_SECRET}"}})
		if err != nil {
			t.Fatalf("request /note-param: %v", err)
		}
		if strings.Contains(contextHTML, "leaked") || !strings.Contains(contextHTML, "NOTE-${BINO_TEST_SERVER_SECRET}") {
			t.Errorf("route /note-param: page without params was expanded:\n%s", contextHTML)
		}

		// A harmless value that the page text completes to a reference.
		_, contextHTML, err = request("/budget", url.Values{"AMOUNT": {"{BINO_TEST_SERVER_SECRET}"}})
		if err != nil {
			t.Fatalf("request /budget: %v", err)
		}
		if strings.Contains(contextHTML, "leaked") || !strings.Contains(contextHTML, "BUDGET-IS-${BINO_TEST_SERVER_SECRET}") {
			t.Errorf("route /budget: request value was expanded together with the page text:\n%s", contextHTML)
		}
	})
}

// TestLoadLayoutPageTemplates checks the entry params of a route against the
// query params of a request. The entry is valid YAML only once its references
// are replaced, and the artefact has a reference in a number field.
func TestLoadLayoutPageTemplates(t *testing.T) {
	unsetFixtureEnv(t)
	ctx := context.Background()
	workdir := t.TempDir()
	manifest := `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: regional
  params:
    - name: REGION
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
        - {page: regional, params: {REGION: ${AREA:US}, TITLE: ${AREA__X:t}, LABEL: "${AREA_LABEL}", PAIR: "${AREA}-${ZONE}"}}
      queryParams:
        - name: AREA
          optional: true
        - name: AREA__X
          optional: true
        - name: ZONE
          optional: true
        - name: LIMIT
          type: number
          default: "10"
        - name: TOP
          type: number
          optional: true
          options:
            max: ${LIMIT:10}
`
	if err := os.WriteFile(filepath.Join(workdir, "report.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write report.yaml: %v", err)
	}
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

	entries := liveArtefact.Spec.Routes["/"].LayoutPages
	templates := loadLayoutPageTemplates(ctx, logx.Nop(), workdir, nil, *liveArtefact)["/"]
	if templates == nil {
		t.Fatal("no templates for route /")
	}
	tests := []struct {
		name        string
		queryParams map[string]string
		param, want string
	}{
		{"query param sent", map[string]string{"AREA": "APAC"}, "REGION", "APAC"},
		{"query param not sent keeps the inline default", nil, "REGION", "US"},
		{"label of a query param", map[string]string{"AREA": "a", "AREA_LABEL": "Asia"}, "LABEL", "Asia"},
		{"query param whose name starts like another", map[string]string{"AREA": "one", "AREA__X": "two"}, "TITLE", "two"},
		{"one of two references sent", map[string]string{"AREA": "APAC"}, "PAIR", "APAC-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := resolveLayoutPageParams(entries, templates, tt.queryParams)
			if len(refs) != 1 || refs[0].Params[tt.param] != tt.want {
				t.Errorf("entries = %v, want %s=%s", refs, tt.param, tt.want)
			}
		})
	}
}

// TestResolveLayoutPageParams_NoTemplates checks the entries of a route whose
// references could not be read at startup. An empty value may be a reference
// that the startup load replaced, so it must not blank the page param.
func TestResolveLayoutPageParams_NoTemplates(t *testing.T) {
	unsetFixtureEnv(t)
	entries := config.LayoutPagesOrRefs{{Page: "regional", Params: map[string]string{"REGION": "", "YEAR": "2024"}}}
	refs := resolveLayoutPageParams(entries, nil, map[string]string{"REGION": "US"})
	if _, set := refs[0].Params["REGION"]; set || refs[0].Params["YEAR"] != "2024" {
		t.Errorf("params = %v, want YEAR=2024 only", refs[0].Params)
	}
}

// TestPlainQueryParams checks what a request value looks like when it may
// become a page param.
func TestPlainQueryParams(t *testing.T) {
	got := plainQueryParams(map[string]string{
		"TEXT":      `Food & "Beverage" <a\b>`,
		"REFERENCE": "${HOME}",
		"FALLBACK":  "${HOME}",
		"UNCLOSED":  "a${b",
	}, map[string]string{"FALLBACK": "EU"})
	want := map[string]string{
		"TEXT":     `Food & \"Beverage\" <a\\b>`,
		"FALLBACK": "EU",
		"UNCLOSED": "a${b",
	}
	if !maps.Equal(got, want) {
		t.Errorf("plainQueryParams() = %v, want %v", got, want)
	}
}
