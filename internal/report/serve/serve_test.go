package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func ptr(s string) *string {
	return &s
}

func TestValidateAndMergeQueryParams(t *testing.T) {
	tests := []struct {
		name         string
		routeSpec    config.LiveRouteSpec
		requestQuery map[string][]string
		wantParams   map[string]string
		wantMissing  []string
	}{
		{
			name: "no params defined",
			routeSpec: config.LiveRouteSpec{
				QueryParams: nil,
			},
			requestQuery: nil,
			wantParams:   map[string]string{},
			wantMissing:  nil,
		},
		{
			name: "param with default, no request value",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "foo", Default: ptr("bar")},
				},
			},
			requestQuery: nil,
			wantParams:   map[string]string{"foo": "bar"},
			wantMissing:  nil,
		},
		{
			name: "param with default, request overrides",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "foo", Default: ptr("bar")},
				},
			},
			requestQuery: map[string][]string{"foo": {"baz"}},
			wantParams:   map[string]string{"foo": "baz"},
			wantMissing:  nil,
		},
		{
			name: "required param missing",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "required_param"},
				},
			},
			requestQuery: nil,
			wantParams:   map[string]string{},
			wantMissing:  []string{"required_param"},
		},
		{
			name: "required param provided",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "required_param"},
				},
			},
			requestQuery: map[string][]string{"required_param": {"value"}},
			wantParams:   map[string]string{"required_param": "value"},
			wantMissing:  nil,
		},
		{
			name: "optional param missing is ok",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "optional_param", Optional: true},
				},
			},
			requestQuery: nil,
			wantParams:   map[string]string{},
			wantMissing:  nil,
		},
		{
			name: "undeclared param in request is ignored",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "declared", Default: ptr("default")},
				},
			},
			requestQuery: map[string][]string{
				"declared":   {"value"},
				"undeclared": {"ignored"},
			},
			wantParams:  map[string]string{"declared": "value"},
			wantMissing: nil,
		},
		{
			name: "multiple params mixed",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "with_default", Default: ptr("default_val")},
					{Name: "required"},
					{Name: "optional", Optional: true},
				},
			},
			requestQuery: map[string][]string{
				"required": {"req_value"},
			},
			wantParams: map[string]string{
				"with_default": "default_val",
				"required":     "req_value",
			},
			wantMissing: nil,
		},
		{
			name: "first value used when multiple provided",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "param"},
				},
			},
			requestQuery: map[string][]string{"param": {"first", "second"}},
			wantParams:   map[string]string{"param": "first"},
			wantMissing:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateAndMergeQueryParams(tt.routeSpec, tt.requestQuery)

			// Check params
			if len(result.Params) != len(tt.wantParams) {
				t.Errorf("params length = %d, want %d", len(result.Params), len(tt.wantParams))
			}
			for k, v := range tt.wantParams {
				if result.Params[k] != v {
					t.Errorf("params[%q] = %q, want %q", k, result.Params[k], v)
				}
			}

			// Check missing names
			if len(result.MissingNames) != len(tt.wantMissing) {
				t.Errorf("missing names = %v, want %v", result.MissingNames, tt.wantMissing)
			}
			for i, name := range tt.wantMissing {
				if i < len(result.MissingNames) && result.MissingNames[i] != name {
					t.Errorf("missing[%d] = %q, want %q", i, result.MissingNames[i], name)
				}
			}
		})
	}
}

func TestQueryParamValidationResult_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		result QueryParamValidationResult
		want   bool
	}{
		{
			name:   "valid when no missing params",
			result: QueryParamValidationResult{MissingNames: nil},
			want:   true,
		},
		{
			name:   "valid when empty missing params",
			result: QueryParamValidationResult{MissingNames: []string{}},
			want:   true,
		},
		{
			name:   "invalid when has missing params",
			result: QueryParamValidationResult{MissingNames: []string{"foo"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildRoutesJSON(t *testing.T) {
	tests := []struct {
		name         string
		liveArtefact config.LiveArtefact
		wantRoutes   map[string]string
	}{
		{
			name: "empty routes",
			liveArtefact: config.LiveArtefact{
				Spec: config.LiveReportArtefactSpec{
					Routes: map[string]config.LiveRouteSpec{},
				},
			},
			wantRoutes: map[string]string{},
		},
		{
			name: "single route with title",
			liveArtefact: config.LiveArtefact{
				Spec: config.LiveReportArtefactSpec{
					Routes: map[string]config.LiveRouteSpec{
						"/dashboard": {Title: "Dashboard", Artifact: "main-report"},
					},
				},
			},
			wantRoutes: map[string]string{"/dashboard": "Dashboard"},
		},
		{
			name: "route without title uses artifact",
			liveArtefact: config.LiveArtefact{
				Spec: config.LiveReportArtefactSpec{
					Routes: map[string]config.LiveRouteSpec{
						"/report": {Artifact: "main-report"},
					},
				},
			},
			wantRoutes: map[string]string{"/report": "main-report"},
		},
		{
			name: "multiple routes",
			liveArtefact: config.LiveArtefact{
				Spec: config.LiveReportArtefactSpec{
					Routes: map[string]config.LiveRouteSpec{
						"/":        {Title: "Home", Artifact: "home"},
						"/sales":   {Title: "Sales", Artifact: "sales-report"},
						"/metrics": {Artifact: "metrics-report"},
					},
				},
			},
			wantRoutes: map[string]string{
				"/":        "Home",
				"/sales":   "Sales",
				"/metrics": "metrics-report",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRoutesJSON(tt.liveArtefact)
			var gotRoutes map[string]string
			if err := json.Unmarshal(got, &gotRoutes); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if len(gotRoutes) != len(tt.wantRoutes) {
				t.Errorf("got %d routes, want %d", len(gotRoutes), len(tt.wantRoutes))
			}
			for k, v := range tt.wantRoutes {
				if gotRoutes[k] != v {
					t.Errorf("route[%q] = %q, want %q", k, gotRoutes[k], v)
				}
			}
		})
	}
}

func TestBuildMissingParamsJSON(t *testing.T) {
	tests := []struct {
		name          string
		missingParams map[string]struct{}
		want          []string
	}{
		{
			name:          "nil map",
			missingParams: nil,
			want:          []string{},
		},
		{
			name:          "empty map",
			missingParams: map[string]struct{}{},
			want:          []string{},
		},
		{
			name: "single missing param",
			missingParams: map[string]struct{}{
				"foo": {},
			},
			want: []string{"foo"},
		},
		{
			name: "multiple params sorted",
			missingParams: map[string]struct{}{
				"zebra": {},
				"alpha": {},
				"beta":  {},
			},
			want: []string{"alpha", "beta", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMissingParamsJSON(tt.missingParams)
			var gotList []string
			if err := json.Unmarshal(got, &gotList); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if len(gotList) != len(tt.want) {
				t.Errorf("got %d params, want %d", len(gotList), len(tt.want))
			}
			for i, v := range tt.want {
				if i < len(gotList) && gotList[i] != v {
					t.Errorf("param[%d] = %q, want %q", i, gotList[i], v)
				}
			}
		})
	}
}

func TestBuildQueryParamsJSON(t *testing.T) {
	tests := []struct {
		name           string
		routeSpec      config.LiveRouteSpec
		datasetOptions map[string][]QueryParamOptionItem
		wantCount      int
	}{
		{
			name:           "no params",
			routeSpec:      config.LiveRouteSpec{},
			datasetOptions: nil,
			wantCount:      0,
		},
		{
			name: "single required param",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "date", Type: "date"},
				},
			},
			datasetOptions: nil,
			wantCount:      1,
		},
		{
			name: "param with default not required",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "limit", Type: "number", Default: ptr("10")},
				},
			},
			datasetOptions: nil,
			wantCount:      1,
		},
		{
			name: "multiple params",
			routeSpec: config.LiveRouteSpec{
				QueryParams: []config.LiveQueryParamSpec{
					{Name: "start_date", Type: "date"},
					{Name: "end_date", Type: "date"},
					{Name: "limit", Type: "number", Default: ptr("100")},
				},
			},
			datasetOptions: nil,
			wantCount:      3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildQueryParamsJSON(tt.routeSpec, tt.datasetOptions)
			var params []queryParamInfo
			if err := json.Unmarshal(got, &params); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}
			if len(params) != tt.wantCount {
				t.Errorf("got %d params, want %d", len(params), tt.wantCount)
			}
		})
	}
}

func TestBuildScriptConfigIsValidJSON(t *testing.T) {
	liveArtefact := config.LiveArtefact{
		Spec: config.LiveReportArtefactSpec{
			Routes: map[string]config.LiveRouteSpec{
				"/dashboard": {Title: "Dashboard", Artifact: "main"},
			},
		},
	}
	routeSpec := config.LiveRouteSpec{
		QueryParams: []config.LiveQueryParamSpec{
			{Name: "region", Type: "string", Default: ptr("EU")},
		},
	}

	script := buildScript(liveArtefact, "/dashboard", routeSpec, "region=EU", "Y29udGV4dA==", nil, map[string]struct{}{"region": {}})

	if !strings.Contains(script, `window.__binoServeConfig = `) {
		t.Fatalf("script missing config assignment: %s", script)
	}
	if !strings.Contains(script, `<script type="module" src="/__bino/static/serve.js"></script>`) {
		t.Errorf("script missing serve.js reference")
	}

	// The emitted config object must be valid JSON despite the manual
	// contextBase64 concatenation.
	start := strings.Index(script, "window.__binoServeConfig = ") + len("window.__binoServeConfig = ")
	end := strings.Index(script, ";</script>")
	if end <= start {
		t.Fatalf("could not locate config object in script: %s", script)
	}
	var cfg struct {
		Routes               map[string]string `json:"routes"`
		CurrentPath          string            `json:"currentPath"`
		CurrentURL           string            `json:"currentURL"`
		MissingParams        []string          `json:"missingParams"`
		InitialContextBase64 string            `json:"initialContextBase64"`
	}
	if err := json.Unmarshal([]byte(script[start:end]), &cfg); err != nil {
		t.Fatalf("config object is not valid JSON: %v\n%s", err, script[start:end])
	}
	if cfg.CurrentPath != "/dashboard" {
		t.Errorf("currentPath = %q, want %q", cfg.CurrentPath, "/dashboard")
	}
	if cfg.CurrentURL != "/dashboard?region=EU" {
		t.Errorf("currentURL = %q, want %q", cfg.CurrentURL, "/dashboard?region=EU")
	}
	if cfg.InitialContextBase64 != "Y29udGV4dA==" {
		t.Errorf("initialContextBase64 = %q, want %q", cfg.InitialContextBase64, "Y29udGV4dA==")
	}
	if len(cfg.MissingParams) != 1 || cfg.MissingParams[0] != "region" {
		t.Errorf("missingParams = %v, want [region]", cfg.MissingParams)
	}
}

func TestWithStyles(t *testing.T) {
	t.Run("injects styles and shell wrapper", func(t *testing.T) {
		in := []byte(`<html><head><title>x</title></head><body class="a"><p>content</p></body></html>`)
		got := string(WithStyles(in))

		if !strings.Contains(got, `id="bn-serve-style"`) {
			t.Errorf("missing serve style link: %s", got)
		}
		if !strings.Contains(got, `<bino-serve-shell><p>content</p></bino-serve-shell>`) {
			t.Errorf("body content not wrapped in shell: %s", got)
		}
		// Styles must land inside <head>
		headClose := strings.Index(got, "</head>")
		styleIdx := strings.Index(got, "bn-serve-style")
		if styleIdx == -1 || styleIdx > headClose {
			t.Errorf("style block not injected before </head>: %s", got)
		}
	})

	t.Run("idempotent when styles already present", func(t *testing.T) {
		in := []byte(`<html><head><link id="bn-serve-style"></head><body></body></html>`)
		got := WithStyles(in)
		if string(got) != string(in) {
			t.Errorf("WithStyles changed already-styled HTML")
		}
	})

	t.Run("empty input unchanged", func(t *testing.T) {
		if got := WithStyles(nil); len(got) != 0 {
			t.Errorf("WithStyles(nil) = %q, want empty", got)
		}
	})

	t.Run("no head tag prepends styles", func(t *testing.T) {
		in := []byte(`<p>fragment</p>`)
		got := string(WithStyles(in))
		if !strings.HasSuffix(got, `<p>fragment</p>`) || !strings.Contains(got, "bn-serve-style") {
			t.Errorf("styles not prepended to fragment: %s", got)
		}
	})
}

func TestBuildMissingParamsHTML(t *testing.T) {
	liveArtefact := config.LiveArtefact{
		Spec: config.LiveReportArtefactSpec{
			Routes: map[string]config.LiveRouteSpec{
				"/": {Title: "Home", Artifact: "main"},
			},
		},
	}
	routeSpec := config.LiveRouteSpec{
		QueryParams: []config.LiveQueryParamSpec{{Name: "region"}},
	}

	got := string(BuildMissingParamsHTML(liveArtefact, "/", routeSpec, "", []string{"region"}, nil))

	if !strings.Contains(got, "window.__binoServeConfig") {
		t.Errorf("missing serve config script: %s", got)
	}
	if !strings.Contains(got, `"missingParams":["region"]`) {
		t.Errorf("missing params not embedded: %s", got)
	}
	if !strings.Contains(got, "bn-serve-style") {
		t.Errorf("serve styles not applied: %s", got)
	}
}
