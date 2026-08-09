package plugin

import (
	"encoding/json"
	"reflect"
	"testing"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"

	"bino.bi/bino/internal/report/config"
)

func TestManifestFromProto(t *testing.T) {
	t.Run("full manifest converts field by field", func(t *testing.T) {
		pb := &pluginv1.PluginManifest{
			Name:             "salesforce",
			Version:          "2.0.1",
			Description:      "SOQL sources",
			DuckdbExtensions: []string{"httpfs"},
			ProvidesLinter:   true,
			ProvidesAssets:   true,
			Hooks:            []string{"post-load"},
			Kinds: []*pluginv1.KindRegistration{
				{KindName: "SfDataSource", Category: pluginv1.KindCategory_KIND_DATASOURCE, DatasourceType: "sf_soql"},
				{KindName: "SfChart", Category: pluginv1.KindCategory_KIND_COMPONENT},
			},
			Commands: []*pluginv1.CommandDescriptor{{
				Name:  "sf:auth",
				Short: "Authenticate",
				Flags: []*pluginv1.FlagDescriptor{{Name: "profile", Type: "string"}},
			}},
		}

		want := PluginManifest{
			Name:             "salesforce",
			Version:          "2.0.1",
			Description:      "SOQL sources",
			DuckDBExtensions: []string{"httpfs"},
			ProvidesLinter:   true,
			ProvidesAssets:   true,
			Hooks:            []string{"post-load"},
			Kinds: []KindRegistration{
				{KindName: "SfDataSource", Category: KindCategoryDataSource, DataSourceType: "sf_soql"},
				{KindName: "SfChart", Category: KindCategoryComponent},
			},
			Commands: []CommandDescriptor{{
				Name:  "sf:auth",
				Short: "Authenticate",
				Flags: []FlagDescriptor{{Name: "profile", Type: "string"}},
			}},
		}
		if got := manifestFromProto(pb); !reflect.DeepEqual(got, want) {
			t.Fatalf("manifestFromProto() mismatch:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("nil proto yields a zero manifest, no panic", func(t *testing.T) {
		if got := manifestFromProto(nil); !reflect.DeepEqual(got, PluginManifest{}) {
			t.Fatalf("manifestFromProto(nil) = %+v, want zero value", got)
		}
	})
}

func TestKindCategoryFromProto(t *testing.T) {
	tests := []struct {
		name string
		in   pluginv1.KindCategory
		want KindCategory
	}{
		{"datasource", pluginv1.KindCategory_KIND_DATASOURCE, KindCategoryDataSource},
		{"component", pluginv1.KindCategory_KIND_COMPONENT, KindCategoryComponent},
		{"config", pluginv1.KindCategory_KIND_CONFIG, KindCategoryConfig},
		{"artifact", pluginv1.KindCategory_KIND_ARTIFACT, KindCategoryArtifact},
		// A newer plugin SDK may send enum values this binary does not know.
		// Pin the current fallback: unknown categories are treated as
		// components (rendered, never routed as data sources or artifacts).
		{"unknown enum value falls back to component", pluginv1.KindCategory(99), KindCategoryComponent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kindCategoryFromProto(tt.in); got != tt.want {
				t.Fatalf("kindCategoryFromProto(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSeverityFromProto(t *testing.T) {
	tests := []struct {
		name string
		in   pluginv1.Severity
		want Severity
	}{
		{"warning", pluginv1.Severity_WARNING, SeverityWarning},
		{"error", pluginv1.Severity_ERROR, SeverityError},
		{"info", pluginv1.Severity_INFO, SeverityInfo},
		// A newer plugin SDK may send enum values this binary does not know.
		// Pin the current fallback: unknown severities degrade to WARNING,
		// so an unrecognized value can never fail a build on its own.
		{"unknown enum value falls back to warning", pluginv1.Severity(99), SeverityWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := severityFromProto(tt.in); got != tt.want {
				t.Fatalf("severityFromProto(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHookPayloadRoundTrip(t *testing.T) {
	t.Run("nil stays nil in both directions", func(t *testing.T) {
		if got := hookPayloadToProto(nil); got != nil {
			t.Fatalf("hookPayloadToProto(nil) = %+v, want nil", got)
		}
		if got := hookPayloadFromProto(nil); got != nil {
			t.Fatalf("hookPayloadFromProto(nil) = %+v, want nil", got)
		}
	})

	t.Run("full payload survives Go -> proto -> Go", func(t *testing.T) {
		in := &HookPayload{
			Documents: []DocumentPayload{
				{File: "a.yaml", Position: 2, Kind: "DataSet", Name: "rev", Raw: []byte(`{"a":1}`)},
				{File: "b.yaml", Position: 1, Kind: "Table", Name: "tbl", Raw: []byte(`{}`)},
			},
			HTML:     []byte("<html></html>"),
			PDFPath:  "/out/report.pdf",
			Datasets: []DatasetPayload{{Name: "rev", JSONRows: []byte(`[{"a":1}]`), Columns: []string{"a"}}},
			Metadata: map[string]string{"artefact": "sales"},
		}
		got := hookPayloadFromProto(hookPayloadToProto(in))
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
		}
	})
}

func TestLintDocumentsToProto(t *testing.T) {
	docs := []DocumentPayload{
		{File: "a.yaml", Position: 4, Kind: "DataSet", Name: "rev", Raw: []byte(`{"a":1}`)},
	}
	pbs := lintDocumentsToProto(docs)
	if len(pbs) != 1 {
		t.Fatalf("expected 1 proto document, got %d", len(pbs))
	}
	pb := pbs[0]
	if pb.GetFile() != "a.yaml" || pb.GetPosition() != 4 || pb.GetKind() != "DataSet" || pb.GetName() != "rev" || string(pb.GetRaw()) != `{"a":1}` {
		t.Fatalf("lint document mangled: %+v", pb)
	}
}

func TestFindingsFromProto(t *testing.T) {
	pbs := []*pluginv1.LintFinding{
		{RuleId: "sf/x", Message: "bad", File: "a.yaml", DocIdx: 2, Path: "spec.q", Line: 7, Column: 3, Severity: pluginv1.Severity_ERROR},
		{RuleId: "sf/y", Message: "meh", Severity: pluginv1.Severity_INFO},
	}
	want := []LintFinding{
		{RuleID: "sf/x", Message: "bad", File: "a.yaml", DocIdx: 2, Path: "spec.q", Line: 7, Column: 3, Severity: SeverityError},
		{RuleID: "sf/y", Message: "meh", Severity: SeverityInfo},
	}
	if got := findingsFromProto(pbs); !reflect.DeepEqual(got, want) {
		t.Fatalf("findingsFromProto() mismatch:\n got %+v\nwant %+v", got, want)
	}

	if got := findingsFromProto(nil); len(got) != 0 {
		t.Fatalf("findingsFromProto(nil) = %+v, want empty", got)
	}
}

func TestCollectResultFromProto(t *testing.T) {
	pb := &pluginv1.CollectDataSourceResponse{
		JsonRows:         []byte(`[{"id":1}]`),
		ColumnTypes:      map[string]string{"id": "INTEGER"},
		Ephemeral:        true,
		DuckdbExpression: "read_csv('x.csv')",
		Diagnostics: []*pluginv1.Diagnostic{
			{Source: "sf", Stage: "collect", Message: "warned", Severity: pluginv1.Severity_WARNING},
		},
	}
	want := &CollectResult{
		JSONRows:         []byte(`[{"id":1}]`),
		ColumnTypes:      map[string]string{"id": "INTEGER"},
		Ephemeral:        true,
		DuckDBExpression: "read_csv('x.csv')",
		Diagnostics: []Diagnostic{
			{Source: "sf", Stage: "collect", Message: "warned", Severity: SeverityWarning},
		},
	}
	if got := collectResultFromProto(pb); !reflect.DeepEqual(got, want) {
		t.Fatalf("collectResultFromProto() mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestAssetsFromProto(t *testing.T) {
	pb := &pluginv1.GetAssetsResponse{
		Scripts: []*pluginv1.AssetFile{{
			UrlPath:   "/plugins/sf/a.js",
			Content:   []byte("js"),
			MediaType: "text/javascript",
			IsModule:  true,
		}},
		Styles: []*pluginv1.AssetFile{{
			UrlPath:  "/plugins/sf/a.css",
			FilePath: "/opt/a.css",
		}},
	}
	scripts, styles := assetsFromProto(pb)
	wantScripts := []AssetFile{{URLPath: "/plugins/sf/a.js", Content: []byte("js"), MediaType: "text/javascript", IsModule: true}}
	wantStyles := []AssetFile{{URLPath: "/plugins/sf/a.css", FilePath: "/opt/a.css"}}
	if !reflect.DeepEqual(scripts, wantScripts) {
		t.Fatalf("scripts mismatch:\n got %+v\nwant %+v", scripts, wantScripts)
	}
	if !reflect.DeepEqual(styles, wantStyles) {
		t.Fatalf("styles mismatch:\n got %+v\nwant %+v", styles, wantStyles)
	}

	emptyScripts, emptyStyles := assetsFromProto(&pluginv1.GetAssetsResponse{})
	if emptyScripts != nil || emptyStyles != nil {
		t.Fatalf("empty response should yield nil slices, got %v / %v", emptyScripts, emptyStyles)
	}
}

func TestCommandsFromProto(t *testing.T) {
	pbs := []*pluginv1.CommandDescriptor{{
		Name:  "sf:export",
		Short: "Export",
		Long:  "Export things.",
		Usage: "sf:export [flags]",
		Flags: []*pluginv1.FlagDescriptor{
			{Name: "format", Shorthand: "f", Description: "output format", DefaultValue: "csv", Type: "string", Required: true},
			{Name: "verbose", Type: "bool"},
		},
	}}
	want := []CommandDescriptor{{
		Name:  "sf:export",
		Short: "Export",
		Long:  "Export things.",
		Usage: "sf:export [flags]",
		Flags: []FlagDescriptor{
			{Name: "format", Shorthand: "f", Description: "output format", DefaultValue: "csv", Type: "string", Required: true},
			{Name: "verbose", Type: "bool"},
		},
	}}
	if got := commandsFromProto(pbs); !reflect.DeepEqual(got, want) {
		t.Fatalf("commandsFromProto() mismatch:\n got %+v\nwant %+v", got, want)
	}

	if got := commandsFromProto(nil); len(got) != 0 {
		t.Fatalf("commandsFromProto(nil) = %+v, want empty", got)
	}
}

func TestDiagnosticsFromProto(t *testing.T) {
	pbs := []*pluginv1.Diagnostic{
		{Source: "sf", Stage: "collect", Message: "a", Severity: pluginv1.Severity_ERROR},
		{Source: "sf", Stage: "hook", Message: "b", Severity: pluginv1.Severity_INFO},
	}
	want := []Diagnostic{
		{Source: "sf", Stage: "collect", Message: "a", Severity: SeverityError},
		{Source: "sf", Stage: "hook", Message: "b", Severity: SeverityInfo},
	}
	if got := diagnosticsFromProto(pbs); !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnosticsFromProto() mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDocumentsFromConfig(t *testing.T) {
	docs := []config.Document{
		{
			File:     "/abs/a.yaml",
			Position: 1,
			Kind:     "DataSet",
			Name:     "rev",
			Raw:      json.RawMessage(`{"kind":"DataSet"}`),
			Labels:   map[string]string{"team": "fin"}, // not part of the payload
		},
		{File: "/abs/b.yaml", Position: 2, Kind: "Table", Name: "tbl", Raw: json.RawMessage(`{}`)},
	}
	want := []DocumentPayload{
		{File: "/abs/a.yaml", Position: 1, Kind: "DataSet", Name: "rev", Raw: []byte(`{"kind":"DataSet"}`)},
		{File: "/abs/b.yaml", Position: 2, Kind: "Table", Name: "tbl", Raw: []byte(`{}`)},
	}
	if got := DocumentsFromConfig(docs); !reflect.DeepEqual(got, want) {
		t.Fatalf("DocumentsFromConfig() mismatch:\n got %+v\nwant %+v", got, want)
	}
}
