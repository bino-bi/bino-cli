package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

func writeProjectWithEngine(t *testing.T, dir, engineVersion string) {
	t.Helper()
	pin := ""
	if engineVersion != "" {
		pin = "engine-version = \"" + engineVersion + "\"\n"
	}
	body := "report-id = \"test-compat\"\n" + pin
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCompatFinding_Unsupported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	finding, fatal := engineCompatFinding(dir, "v0.5.0")
	if !fatal {
		t.Fatal("expected fatal=true for unsupported pin")
	}
	if finding.RuleID != "engine-version-incompatible" {
		t.Errorf("RuleID = %q, want engine-version-incompatible", finding.RuleID)
	}
	if filepath.Base(finding.File) != "bino.toml" {
		t.Errorf("File = %q, want bino.toml", finding.File)
	}
	if finding.Line != 2 {
		t.Errorf("Line = %d, want 2", finding.Line)
	}
	if !strings.Contains(finding.Message, "v0.5.0") {
		t.Errorf("Message missing version: %s", finding.Message)
	}
}

func TestEngineCompatFinding_Supported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v1.0.0-alpha.19")
	_, fatal := engineCompatFinding(dir, "v1.0.0-alpha.19")
	if fatal {
		t.Error("expected fatal=false for supported pin")
	}
}

func TestEngineCompatFinding_NoPinNoCache(t *testing.T) {
	// Without a pin and (likely) no engine cached at the test runner's
	// HOME, the helper must skip silently rather than fail.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeProjectWithEngine(t, dir, "")
	_, fatal := engineCompatFinding(dir, "")
	if fatal {
		t.Error("expected fatal=false when no engine is resolvable")
	}
}

func TestEngineCompatDiagnostic_Unsupported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	diag, ok := engineCompatDiagnostic(dir)
	if !ok {
		t.Fatal("expected diagnostic for unsupported pin")
	}
	if diag.Severity != "error" {
		t.Errorf("Severity = %q, want error", diag.Severity)
	}
	if diag.Code != "engine-version-incompatible" {
		t.Errorf("Code = %q, want engine-version-incompatible", diag.Code)
	}
	if filepath.Base(diag.File) != "bino.toml" {
		t.Errorf("File = %q, want bino.toml", diag.File)
	}
	if diag.Line != 2 {
		t.Errorf("Line = %d, want 2", diag.Line)
	}
}

func TestEngineCompatDiagnostic_Supported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v1.0.0-alpha.19")
	_, ok := engineCompatDiagnostic(dir)
	if ok {
		t.Error("expected no diagnostic for supported pin")
	}
}

func TestRunLSPValidate_EmitsCompatDiagnostic(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")

	var buf bytes.Buffer
	if err := runLSPValidate(context.Background(), dir, false, &buf); err != nil {
		t.Fatalf("runLSPValidate: %v", err)
	}
	var result LSPValidateResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false")
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "engine-version-incompatible" {
			found = true
			if d.Severity != "error" {
				t.Errorf("Severity = %q, want error", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected an engine-version-incompatible diagnostic, got: %+v", result.Diagnostics)
	}
}

func TestLintCommand_FailsOnIncompatibleEngine(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	// A complete minimal manifest set so manifest loading succeeds and the
	// compat check is the only error surfaced.
	report := `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  format: a4
  orientation: portrait
  language: en
  filename: out.pdf
  title: Sample
  layoutPages:
    - p
`
	page := `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
`
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.yaml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newLintCommand()
	cmd.SetArgs([]string{"--work-dir", dir, "--out-dir", filepath.Join(dir, "out")})
	cmd.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected non-zero exit, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(err.Error(), "engine-version-incompatible") {
		t.Errorf("error message missing rule id: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "engine-version-incompatible") {
		t.Errorf("output should mention rule id, got: %s", combined)
	}
	if !strings.Contains(combined, "bino.toml:2:1") {
		t.Errorf("output should include line:col, got: %s", combined)
	}
}

func TestSmallMultipleSpanEngineFinding(t *testing.T) {
	doc := func(raw string) []config.Document {
		return []config.Document{{Kind: "x", Raw: json.RawMessage(raw)}}
	}
	span := doc(`{"kind":"SmallMultiple","spec":{"base":{"kind":"ChartTime","ref":"t"},"panels":{"by":"g","overrides":[{"value":"a","rowSpan":2}]}}}`)
	plain := doc(`{"kind":"SmallMultiple","spec":{"base":{"kind":"ChartTime","ref":"t"},"panels":{"by":"g"}}}`)
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		want   string
	}{
		{"old engine with a span", "v1.0.0-next.45", span, "SmallMultiple rowSpan: template engine v1.0.0-next.46 or newer is needed"},
		{"old engine with a grid background", "v1.0.0-next.45", doc(`{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","background":"red"}]}}`), "Grid background: template engine"},
		{"old engine with a page id", "v1.0.0-next.45", doc(`{"kind":"LayoutPage","spec":{"titleId":"13A"}}`), "LayoutPage titleId: template engine"},
		{"old engine with a variance only chart", "v1.0.0-next.45", doc(`{"kind":"ChartTime","spec":{"scenarios":[]}}`), "ChartTime scenarios: []: template engine"},
		{"old engine with a style key", "v1.0.0-next.45", doc(`{"kind":"ComponentStyle","spec":{"content":{"bn-layout-card":{"titleFontSize":14}}}}`), "ComponentStyle bn-layout-card.titleFontSize: template engine"},
		{"old engine without the fields", "v1.0.0-next.45", plain, ""},
		{"new engine with a span", "v1.0.0-next.46", span, ""},
		{"local build with a span", "v1.0.0-next.960", span, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := smallMultipleSpanEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != (c.want != "") {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.want != "", finding)
			}
			if fatal && (finding.RuleID != "small-multiple-span-engine-version" || finding.Line != 2 || !strings.Contains(finding.Message, c.want)) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestSmallMultipleSpanDefaultEngineFinding(t *testing.T) {
	chart := []config.Document{{Kind: "ChartTime", Raw: json.RawMessage(`{"kind":"ChartTime","spec":{"dataset":"sales"}}`)}}
	table := []config.Document{{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"dataset":"sales"}}`)}}
	inManifest := []config.Document{{Kind: "ChartTime", Raw: json.RawMessage(`{"kind":"ChartTime","spec":{"dataset":"sales","scenarioSwitch":true}}`)}}
	results := []dataset.Result{{Name: "sales", Defaults: dataset.Defaults{
		"charttime":      {"scenarioSwitch": json.RawMessage(`true`)},
		dataset.AnyToken: {"dataFormatDigitsDecimal": json.RawMessage(`0`)},
	}}}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		fatal  bool
	}{
		{"old engine, chart on the dataset", "v1.0.0-next.45", chart, true},
		{"old engine, table on the dataset", "v1.0.0-next.45", table, false},
		{"old engine, the manifest gate reports it already", "v1.0.0-next.45", inManifest, false},
		{"new engine", "v1.0.0-next.46", chart, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := smallMultipleSpanDefaultEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs, results)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "small-multiple-span-engine-version" ||
				!strings.Contains(finding.Message, "ChartTime dataFormatDigitsDecimal, ChartTime scenarioSwitch")) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestGridCellLayoutEngineFinding(t *testing.T) {
	grid := config.Document{Kind: "Grid", Raw: json.RawMessage(`{"kind":"Grid","spec":{"cellHeight":"equal"}}`)}
	plain := config.Document{Kind: "Grid", Raw: json.RawMessage(`{"kind":"Grid","spec":{}}`)}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		fatal  bool
	}{
		{"old engine with the fields", "v1.0.0-next.30", []config.Document{grid}, true},
		{"old engine without the fields", "v1.0.0-next.30", []config.Document{plain}, false},
		{"new engine with the fields", "v1.0.0-next.32", []config.Document{grid}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := gridCellLayoutEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "grid-cell-layout-engine-version" || finding.Line != 2) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestOperationSentimentEngineFinding(t *testing.T) {
	op := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"type":"opt","variances":["dac1_pl1_op"]}}`)}
	plain := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"type":"opt","variances":["dac1_pl1_pos"]}}`)}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		fatal  bool
	}{
		{"old engine with _op", "v1.0.0-next.33", []config.Document{op}, true},
		{"old engine without _op", "v1.0.0-next.33", []config.Document{plain}, false},
		{"new engine with _op", "v1.0.0-next.34", []config.Document{op}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := operationSentimentEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "operation-sentiment-engine-version" || finding.Line != 2) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestTableOrderMeasureEngineFinding(t *testing.T) {
	measure := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"order":"rowgroupindex,dac1_pp1_pos"}}`)}
	plain := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"order":"rowgroupindex,ac1"}}`)}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		fatal  bool
	}{
		{"old engine with variance order", "v1.0.0-next.34", []config.Document{measure}, true},
		{"old engine without variance order", "v1.0.0-next.34", []config.Document{plain}, false},
		{"new engine with variance order", "v1.0.0-next.35", []config.Document{measure}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := tableOrderMeasureEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "table-order-engine-version" || finding.Line != 2) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestCategoryIntervalPositionEngineFinding(t *testing.T) {
	moved := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"interval":"setname","categoryIntervalPosition":2}}`)}
	plain := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"interval":"setname"}}`)}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		fatal  bool
	}{
		{"old engine with position", "v1.0.0-next.38", []config.Document{moved}, true},
		{"old engine without position", "v1.0.0-next.38", []config.Document{plain}, false},
		{"new engine with position", "v1.0.0-next.39", []config.Document{moved}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := categoryIntervalPositionEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "table-category-interval-position-engine-version" || finding.Line != 2) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestCategoryIntervalPositionDefaultEngineFinding(t *testing.T) {
	plain := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"dataset":"sales","interval":"setname"}}`)}
	moved := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"interval":"setname","categoryIntervalPosition":2}}`)}
	withDefault := []dataset.Result{{Name: "sales", Defaults: dataset.Defaults{"table": {"categoryIntervalPosition": json.RawMessage(`2`)}}}}
	cases := []struct {
		name    string
		engine  string
		docs    []config.Document
		results []dataset.Result
		fatal   bool
	}{
		{"old engine with default", "v1.0.0-next.38", []config.Document{plain}, withDefault, true},
		{"old engine without default", "v1.0.0-next.38", []config.Document{plain}, []dataset.Result{{Name: "sales"}}, false},
		{"manifest value is the other finding", "v1.0.0-next.38", []config.Document{moved}, withDefault, false},
		{"new engine with default", "v1.0.0-next.39", []config.Document{plain}, withDefault, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := categoryIntervalPositionDefaultEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs, c.results)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && finding.RuleID != "table-category-interval-position-engine-version" {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestTableCalculationSchemeEngineFinding(t *testing.T) {
	doc := func(spec string) []config.Document {
		return []config.Document{{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{` + spec + `}}`)}}
	}
	cases := []struct {
		name   string
		engine string
		docs   []config.Document
		prop   string // empty: no finding
	}{
		{"old engine with runningSum", "v1.0.0-next.39", doc(`"runningSum":true`), "runningSum"},
		{"old engine with thereofPosition above", "v1.0.0-next.39", doc(`"thereofPosition":"above"`), "thereofPosition"},
		{"old engine with varianceWaterfall", "v1.0.0-next.39", doc(`"varianceWaterfall":true`), "varianceWaterfall"},
		{"old engine with the defaults", "v1.0.0-next.39", doc(`"runningSum":false,"thereofPosition":"below","varianceWaterfall":false`), ""},
		{"old engine without the fields", "v1.0.0-next.39", doc(`"type":"optnototal"`), ""},
		{"new engine with all", "v1.0.0-next.40", doc(`"runningSum":true,"thereofPosition":"above","varianceWaterfall":true`), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := tableCalculationSchemeEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs)
			if fatal != (c.prop != "") {
				t.Fatalf("fatal = %v (%+v)", fatal, finding)
			}
			if fatal && (finding.RuleID != "table-calculation-scheme-engine-version" || finding.Line != 2 || !strings.Contains(finding.Message, c.prop)) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

func TestTableCalculationSchemeDefaultEngineFinding(t *testing.T) {
	plain := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"dataset":"pnl"}}`)}
	running := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"dataset":"pnl","runningSum":true}}`)}
	off := config.Document{Kind: "Table", Raw: json.RawMessage(`{"kind":"Table","spec":{"dataset":"pnl","varianceWaterfall":false}}`)}
	chart := config.Document{Kind: "ChartStructure", Raw: json.RawMessage(`{"kind":"ChartStructure","spec":{"dataset":"pnl"}}`)}
	withDefault := []dataset.Result{{Name: "pnl", Defaults: dataset.Defaults{"table": {"varianceWaterfall": json.RawMessage(`true`)}}}}
	cases := []struct {
		name    string
		engine  string
		docs    []config.Document
		results []dataset.Result
		fatal   bool
	}{
		{"old engine with default", "v1.0.0-next.39", []config.Document{plain}, withDefault, true},
		{"old engine without default", "v1.0.0-next.39", []config.Document{plain}, []dataset.Result{{Name: "pnl"}}, false},
		{"manifest turns the default off", "v1.0.0-next.39", []config.Document{off}, withDefault, false},
		{"only a chart binds the dataset", "v1.0.0-next.39", []config.Document{chart}, withDefault, false},
		{"manifest value is the other finding", "v1.0.0-next.39", []config.Document{running}, withDefault, false},
		{"new engine with default", "v1.0.0-next.40", []config.Document{plain}, withDefault, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectWithEngine(t, dir, c.engine)
			finding, fatal := tableCalculationSchemeDefaultEngineFinding(dir, &pathutil.ProjectConfig{EngineVersion: c.engine}, c.docs, c.results)
			if fatal != c.fatal {
				t.Fatalf("fatal = %v, want %v (%+v)", fatal, c.fatal, finding)
			}
			if fatal && (finding.RuleID != "table-calculation-scheme-engine-version" || !strings.Contains(finding.Message, "varianceWaterfall")) {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}
