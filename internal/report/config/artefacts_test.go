package config

import (
	"encoding/json"
	"testing"
)

func TestCollectArtefacts(t *testing.T) {
	doc := Document{
		File:     "report.yaml",
		Position: 1,
		Kind:     "ReportArtefact",
		Name:     "weekly",
	}
	payload := map[string]any{
		"spec": map[string]any{
			"format":      "a4",
			"orientation": "portrait",
			"language":    "de",
			"filename":    "weekly.pdf",
			"title":       "Weekly",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	doc.Raw = raw

	artifacts, err := CollectArtefacts([]Document{doc})
	if err != nil {
		t.Fatalf("CollectArtefacts returned error: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	got := artifacts[0]
	if got.Document.Name != "weekly" {
		t.Fatalf("unexpected artifact name %q", got.Document.Name)
	}
	if got.Spec.Filename != "weekly.pdf" {
		t.Fatalf("unexpected filename %q", got.Spec.Filename)
	}
}

func TestCollectArtefactsDuplicateNames(t *testing.T) {
	payload := map[string]any{
		"spec": map[string]any{
			"format":      "a4",
			"orientation": "portrait",
			"language":    "de",
			"filename":    "a.pdf",
			"title":       "A",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	docs := []Document{
		{File: "report.yaml", Position: 1, Kind: "ReportArtefact", Name: "dup", Raw: raw},
		{File: "report2.yaml", Position: 1, Kind: "ReportArtefact", Name: "dup", Raw: raw},
	}
	if _, err := CollectArtefacts(docs); err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestApplyReportArtefactDefaults(t *testing.T) {
	spec := ReportArtefactSpec{}
	warnings := applyReportArtefactDefaults("demo", &spec)
	if spec.Format != DefaultArtefactFormat {
		t.Fatalf("expected default format %q, got %q", DefaultArtefactFormat, spec.Format)
	}
	if spec.Orientation != DefaultArtefactOrientation {
		t.Fatalf("expected default orientation %q, got %q", DefaultArtefactOrientation, spec.Orientation)
	}
	if spec.Language != DefaultArtefactLanguage {
		t.Fatalf("expected default language %q, got %q", DefaultArtefactLanguage, spec.Language)
	}
	if len(spec.LayoutPages) != 1 || spec.LayoutPages[0].Page != "*" {
		t.Fatalf("expected default layoutPages [\"*\"], got %v", spec.LayoutPages)
	}
	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d", len(warnings))
	}

	explicit := ReportArtefactSpec{Format: "a4", Orientation: "portrait", Language: "en", LayoutPages: LayoutPagesOrRefs{{Page: "cover"}, {Page: "summary"}}}
	if warns := applyReportArtefactDefaults("demo", &explicit); len(warns) != 0 {
		t.Fatalf("expected no warnings when fields are set, got %d", len(warns))
	}
	// Explicit layoutPages should not be overwritten
	if len(explicit.LayoutPages) != 2 || explicit.LayoutPages[0].Page != "cover" {
		t.Fatalf("explicit layoutPages should not be changed, got %v", explicit.LayoutPages)
	}
}

func TestReportArtefactSpecLayoutPages(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLen   int
		expectedFirst string
	}{
		{
			name:          "string array format",
			input:         `{"spec": {"layoutPages": ["cover", "sales-*", "appendix-*"]}}`,
			expectedLen:   3,
			expectedFirst: "cover",
		},
		{
			name:          "single string format",
			input:         `{"spec": {"layoutPages": "cover"}}`,
			expectedLen:   1,
			expectedFirst: "cover",
		},
		{
			name:          "omitted uses default",
			input:         `{"spec": {}}`,
			expectedLen:   1,
			expectedFirst: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Document{
				File:     "report.yaml",
				Position: 1,
				Kind:     "ReportArtefact",
				Name:     "test",
				Raw:      json.RawMessage(tt.input),
			}

			artifacts, err := CollectArtefacts([]Document{doc})
			if err != nil {
				t.Fatalf("CollectArtefacts returned error: %v", err)
			}

			if len(artifacts) != 1 {
				t.Fatalf("expected 1 artifact, got %d", len(artifacts))
			}

			if len(artifacts[0].Spec.LayoutPages) != tt.expectedLen {
				t.Errorf("expected %d layoutPages, got %d", tt.expectedLen, len(artifacts[0].Spec.LayoutPages))
			}

			if artifacts[0].Spec.LayoutPages[0].Page != tt.expectedFirst {
				t.Errorf("expected first layoutPage %q, got %q", tt.expectedFirst, artifacts[0].Spec.LayoutPages[0].Page)
			}
		})
	}
}

func TestCollectLiveArtefacts(t *testing.T) {
	defaultVal := "all"
	payload := map[string]any{
		"spec": map[string]any{
			"title": "Dashboard",
			"routes": map[string]any{
				"/": map[string]any{
					"artifact": "main-report",
					"queryParams": []any{
						map[string]any{
							"name":        "REGION",
							"default":     defaultVal,
							"description": "Filter region",
						},
						map[string]any{
							"name":        "YEAR",
							"description": "Year filter",
						},
					},
				},
				"/sales": map[string]any{
					"artifact": "sales-report",
					"title":    "Sales",
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	doc := Document{
		File:     "live.yaml",
		Position: 1,
		Kind:     "LiveReportArtefact",
		Name:     "dashboard",
		Raw:      raw,
	}

	liveArtefacts, err := CollectLiveArtefacts([]Document{doc})
	if err != nil {
		t.Fatalf("CollectLiveArtefacts returned error: %v", err)
	}
	if len(liveArtefacts) != 1 {
		t.Fatalf("expected 1 live artifact, got %d", len(liveArtefacts))
	}
	live := liveArtefacts[0]
	if live.Document.Name != "dashboard" {
		t.Fatalf("unexpected name %q", live.Document.Name)
	}
	if live.Spec.Title != "Dashboard" {
		t.Fatalf("unexpected title %q", live.Spec.Title)
	}
	if len(live.Spec.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(live.Spec.Routes))
	}
	if live.Spec.Routes["/"].Artifact != "main-report" {
		t.Fatalf("unexpected root artifact %q", live.Spec.Routes["/"].Artifact)
	}
	if len(live.Spec.Routes["/"].QueryParams) != 2 {
		t.Fatalf("expected 2 query params on root route, got %d", len(live.Spec.Routes["/"].QueryParams))
	}
}

func TestLiveRouteSpec_GetQueryParamDefaults(t *testing.T) {
	defaultVal := "2024"
	route := LiveRouteSpec{
		Artifact: "test-report",
		QueryParams: []LiveQueryParamSpec{
			{Name: "YEAR", Default: &defaultVal},
			{Name: "REGION"}, // no default
		},
	}
	defaults := route.GetQueryParamDefaults()
	if len(defaults) != 1 {
		t.Fatalf("expected 1 default, got %d", len(defaults))
	}
	if defaults["YEAR"] != "2024" {
		t.Fatalf("unexpected default for YEAR: %q", defaults["YEAR"])
	}
}

func TestLiveRouteSpec_GetRequiredQueryParams(t *testing.T) {
	defaultVal := "2024"
	route := LiveRouteSpec{
		Artifact: "test-report",
		QueryParams: []LiveQueryParamSpec{
			{Name: "YEAR", Default: &defaultVal},
			{Name: "REGION"}, // no default = required
		},
	}
	required := route.GetRequiredQueryParams()
	if len(required) != 1 {
		t.Fatalf("expected 1 required, got %d", len(required))
	}
	if required[0] != "REGION" {
		t.Fatalf("expected REGION to be required, got %q", required[0])
	}
}

func TestFindLiveArtefact(t *testing.T) {
	artifacts := []LiveArtefact{
		{Document: Document{Name: "alpha"}},
		{Document: Document{Name: "beta"}},
	}
	found := FindLiveArtefact(artifacts, "beta")
	if found == nil {
		t.Fatalf("expected to find beta")
	}
	if found.Document.Name != "beta" {
		t.Fatalf("unexpected name %q", found.Document.Name)
	}

	notFound := FindLiveArtefact(artifacts, "gamma")
	if notFound != nil {
		t.Fatalf("expected nil for non-existent artifact")
	}
}

func TestSourcesOrStrings_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "string array format",
			input:    `["./docs/intro.md", "./docs/chapter1.md"]`,
			expected: []string{"./docs/intro.md", "./docs/chapter1.md"},
			wantErr:  false,
		},
		{
			name:     "string array with glob pattern",
			input:    `["./docs/*.md", "./appendix/a.md"]`,
			expected: []string{"./docs/*.md", "./appendix/a.md"},
			wantErr:  false,
		},
		{
			name:     "legacy object array format",
			input:    `[{"file": "./docs/intro.md"}, {"file": "./docs/chapter1.md"}]`,
			expected: []string{"./docs/intro.md", "./docs/chapter1.md"},
			wantErr:  false,
		},
		{
			name:     "empty array",
			input:    `[]`,
			expected: []string{},
			wantErr:  false,
		},
		{
			name:    "invalid format - object",
			input:   `{"file": "test.md"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sources SourcesOrStrings
			err := json.Unmarshal([]byte(tt.input), &sources)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(sources) != len(tt.expected) {
					t.Errorf("UnmarshalJSON() got %d items, want %d", len(sources), len(tt.expected))
					return
				}
				for i, v := range sources {
					if v != tt.expected[i] {
						t.Errorf("UnmarshalJSON()[%d] = %v, want %v", i, v, tt.expected[i])
					}
				}
			}
		})
	}
}

func TestSourcesOrStrings_MarshalJSON(t *testing.T) {
	sources := SourcesOrStrings{"./docs/intro.md", "./docs/*.md"}
	data, err := json.Marshal(sources)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	expected := `["./docs/intro.md","./docs/*.md"]`
	if string(data) != expected {
		t.Errorf("MarshalJSON() = %s, want %s", string(data), expected)
	}
}

func TestCollectDocumentArtefacts(t *testing.T) {
	tests := []struct {
		name           string
		sources        any
		expectedCount  int
		expectedSource string
	}{
		{
			name:           "string array format",
			sources:        []string{"./docs/intro.md", "./docs/chapter1.md"},
			expectedCount:  2,
			expectedSource: "./docs/intro.md",
		},
		{
			name: "legacy object array format",
			sources: []map[string]string{
				{"file": "./docs/intro.md"},
				{"file": "./docs/chapter1.md"},
			},
			expectedCount:  2,
			expectedSource: "./docs/intro.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"spec": map[string]any{
					"format":   "a4",
					"filename": "test.pdf",
					"title":    "Test",
					"sources":  tt.sources,
				},
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			doc := Document{
				File:     "test.yaml",
				Position: 1,
				Kind:     "DocumentArtefact",
				Name:     "test-doc",
				Raw:      raw,
			}

			artifacts, err := CollectDocumentArtefacts([]Document{doc})
			if err != nil {
				t.Fatalf("CollectDocumentArtefacts() error = %v", err)
			}

			if len(artifacts) != 1 {
				t.Fatalf("expected 1 artifact, got %d", len(artifacts))
			}

			if len(artifacts[0].Spec.Sources) != tt.expectedCount {
				t.Errorf("expected %d sources, got %d", tt.expectedCount, len(artifacts[0].Spec.Sources))
			}

			if len(artifacts[0].Spec.Sources) > 0 && artifacts[0].Spec.Sources[0] != tt.expectedSource {
				t.Errorf("first source = %q, want %q", artifacts[0].Spec.Sources[0], tt.expectedSource)
			}
		})
	}
}

func TestLayoutPagesOrRefs_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLen   int
		expectedFirst string
		expectedError bool
	}{
		{
			name:          "single string",
			input:         `"cover"`,
			expectedLen:   1,
			expectedFirst: "cover",
		},
		{
			name:          "string array",
			input:         `["cover", "sales-*", "appendix"]`,
			expectedLen:   3,
			expectedFirst: "cover",
		},
		{
			name:          "object with params",
			input:         `[{"page": "regional-sales", "params": {"REGION": "EU"}}]`,
			expectedLen:   1,
			expectedFirst: "regional-sales",
		},
		{
			name:          "mixed array",
			input:         `["cover", {"page": "sales", "params": {"YEAR": "2024"}}]`,
			expectedLen:   2,
			expectedFirst: "cover",
		},
		{
			name:          "object without params",
			input:         `[{"page": "simple"}]`,
			expectedLen:   1,
			expectedFirst: "simple",
		},
		{
			name:          "glob with params - error",
			input:         `[{"page": "sales-*", "params": {"REGION": "EU"}}]`,
			expectedError: true,
		},
		{
			name:          "empty page name - error",
			input:         `[{"page": "", "params": {"REGION": "EU"}}]`,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refs LayoutPagesOrRefs
			err := json.Unmarshal([]byte(tt.input), &refs)

			if tt.expectedError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(refs) != tt.expectedLen {
				t.Errorf("expected %d refs, got %d", tt.expectedLen, len(refs))
			}

			if len(refs) > 0 && refs[0].Page != tt.expectedFirst {
				t.Errorf("expected first page %q, got %q", tt.expectedFirst, refs[0].Page)
			}
		})
	}
}

func TestLayoutPagesOrRefs_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		refs     LayoutPagesOrRefs
		expected string
	}{
		{
			name:     "single page no params",
			refs:     LayoutPagesOrRefs{{Page: "cover"}},
			expected: `"cover"`,
		},
		{
			name:     "multiple pages no params",
			refs:     LayoutPagesOrRefs{{Page: "cover"}, {Page: "sales"}},
			expected: `["cover","sales"]`,
		},
		{
			name:     "page with params",
			refs:     LayoutPagesOrRefs{{Page: "sales", Params: map[string]string{"REGION": "EU"}}},
			expected: `[{"page":"sales","params":{"REGION":"EU"}}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.refs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(data))
			}
		})
	}
}

func TestLayoutPageRef_IsGlob(t *testing.T) {
	tests := []struct {
		name     string
		ref      LayoutPageRef
		expected bool
	}{
		{name: "simple name", ref: LayoutPageRef{Page: "cover"}, expected: false},
		{name: "asterisk glob", ref: LayoutPageRef{Page: "sales-*"}, expected: true},
		{name: "question glob", ref: LayoutPageRef{Page: "chapter-?"}, expected: true},
		{name: "bracket glob", ref: LayoutPageRef{Page: "[abc]"}, expected: true},
		{name: "with params", ref: LayoutPageRef{Page: "sales", Params: map[string]string{"REGION": "EU"}}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.IsGlob(); got != tt.expected {
				t.Errorf("IsGlob() = %v, want %v", got, tt.expected)
			}
		})
	}
}
