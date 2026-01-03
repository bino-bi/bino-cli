package cli

import (
	"encoding/json"
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
			result := validateAndMergeQueryParams(tt.routeSpec, tt.requestQuery)

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
		result queryParamValidationResult
		want   bool
	}{
		{
			name:   "valid when no missing params",
			result: queryParamValidationResult{MissingNames: nil},
			want:   true,
		},
		{
			name:   "valid when empty missing params",
			result: queryParamValidationResult{MissingNames: []string{}},
			want:   true,
		},
		{
			name:   "invalid when has missing params",
			result: queryParamValidationResult{MissingNames: []string{"foo"}},
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

func TestBuildCacheKey(t *testing.T) {
	tests := []struct {
		name         string
		artefactName string
		params       map[string]string
		want         string
	}{
		{
			name:         "no params",
			artefactName: "report",
			params:       nil,
			want:         "report",
		},
		{
			name:         "empty params",
			artefactName: "report",
			params:       map[string]string{},
			want:         "report",
		},
		{
			name:         "single param",
			artefactName: "report",
			params:       map[string]string{"foo": "bar"},
			want:         "report?foo=bar",
		},
		{
			name:         "multiple params sorted",
			artefactName: "report",
			params:       map[string]string{"z": "3", "a": "1", "m": "2"},
			want:         "report?a=1?m=2?z=3",
		},
		{
			name:         "params with special chars",
			artefactName: "my-report",
			params:       map[string]string{"date": "2024-01-01", "name": "hello world"},
			want:         "my-report?date=2024-01-01?name=hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCacheKey(tt.artefactName, tt.params)
			if got != tt.want {
				t.Errorf("buildCacheKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildLayoutPagesCacheKey(t *testing.T) {
	tests := []struct {
		name        string
		layoutPages config.StringOrSlice
		params      map[string]string
		want        string
	}{
		{
			name:        "single page no params",
			layoutPages: config.StringOrSlice{"page1"},
			params:      nil,
			want:        "layoutPages:page1",
		},
		{
			name:        "multiple pages sorted",
			layoutPages: config.StringOrSlice{"page3", "page1", "page2"},
			params:      nil,
			want:        "layoutPages:page1,page2,page3",
		},
		{
			name:        "with params",
			layoutPages: config.StringOrSlice{"page1"},
			params:      map[string]string{"foo": "bar"},
			want:        "layoutPages:page1?foo=bar",
		},
		{
			name:        "multiple pages with multiple params",
			layoutPages: config.StringOrSlice{"b", "a"},
			params:      map[string]string{"z": "3", "a": "1"},
			want:        "layoutPages:a,b?a=1&z=3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLayoutPagesCacheKey(tt.layoutPages, tt.params)
			if got != tt.want {
				t.Errorf("buildLayoutPagesCacheKey() = %q, want %q", got, tt.want)
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
						"/dashboard": {Title: "Dashboard", Artefact: "main-report"},
					},
				},
			},
			wantRoutes: map[string]string{"/dashboard": "Dashboard"},
		},
		{
			name: "route without title uses artefact",
			liveArtefact: config.LiveArtefact{
				Spec: config.LiveReportArtefactSpec{
					Routes: map[string]config.LiveRouteSpec{
						"/report": {Artefact: "main-report"},
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
						"/":        {Title: "Home", Artefact: "home"},
						"/sales":   {Title: "Sales", Artefact: "sales-report"},
						"/metrics": {Artefact: "metrics-report"},
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
		datasetOptions map[string][]queryParamOptionItem
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
