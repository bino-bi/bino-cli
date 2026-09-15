package schema

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	yamlPkg "gopkg.in/yaml.v3"
)

func TestValidate_ValidDocument(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "valid DataSet with inline query",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  query: SELECT 1
`,
		},
		{
			name: "valid DataSet with derive and assert on a source pass-through",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  source: sales_csv
  derive:
    pp1: { from: ac1, shift: 1 month, grain: month }
    pp2: { from: ac1, shift: 1 year, grain: month }
  assert:
    pp3: { from: pl1, shift: 1 year, grain: month }
`,
		},
		{
			name: "valid Table with inline dataset declaring derive",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales_table
spec:
  dataset:
    query: SELECT * FROM sales_csv
    dependencies: [sales_csv]
    derive:
      pp1: { from: ac1, shift: 1 year, grain: month }
`,
		},
		{
			name: "valid DataSet with derive on a PRQL query",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  prql: from sales_csv
  derive:
    pp1: { from: ac1, shift: 7 day, grain: day }
`,
		},
		{
			name: "valid DataSource CSV",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: test_source
spec:
  type: csv
  path: data/test.csv
`,
		},
		{
			name: "valid ReportArtefact",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: test_report
spec:
  filename: report.pdf
  title: Test Report
  format: pdf
  orientation: portrait
`,
		},
		{
			name: "valid DataSet with constraints",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
  constraints:
    - labels.env == production
spec:
  query: SELECT 1
`,
		},
		{
			name: "valid DataSet with description",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
  description: A test dataset
spec:
  query: SELECT * FROM users
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidate_LayoutChildrenOptional(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "LayoutPage without children is a valid skeleton",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: new_page
spec: {}
`,
		},
		{
			name: "LayoutPage with empty children is a valid skeleton",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: new_page
spec:
  children: []
`,
		},
		{
			name: "LayoutPage with referenced children",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: new_page
spec:
  children:
    - kind: Text
      ref: header_text
`,
		},
		{
			name: "LayoutCard without children is a valid skeleton",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutCard
metadata:
  name: new_card
spec: {}
`,
		},
		{
			name: "LayoutCard with empty children is a valid skeleton",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutCard
metadata:
  name: new_card
spec:
  titleBusinessUnit: Sales
  children: []
`,
		},
		{
			name: "inline LayoutCard child without children stays rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: new_page
spec:
  children:
    - kind: LayoutCard
      spec:
        titleBusinessUnit: Sales
`,
			wantErr: true,
		},
		{
			name: "inline LayoutCard child with empty children stays rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: new_page
spec:
  children:
    - kind: LayoutCard
      spec:
        titleBusinessUnit: Sales
        children: []
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidate_MissingRequiredField(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantPath    string
		wantMessage string
	}{
		{
			name: "missing kind",
			yaml: `
apiVersion: bino.bi/v1alpha1
metadata:
  name: test
spec:
  query: SELECT 1
`,
			wantPath:    "(root)",
			wantMessage: "missing property 'kind'",
		},
		{
			name: "missing metadata.name",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  description: test
spec:
  query: SELECT 1
`,
			wantPath:    "metadata",
			wantMessage: "missing property 'name'",
		},
		{
			name: "missing spec",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
`,
			wantPath:    "(root)",
			wantMessage: "missing property 'spec'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			ve := &ValidationError{}
			ok := errors.As(err, &ve)
			if !ok {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Path, tt.wantPath) &&
					strings.Contains(issue.Message, tt.wantMessage) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected issue with path containing %q and message containing %q, got: %v",
					tt.wantPath, tt.wantMessage, ve.Errors)
			}
		})
	}
}

func TestValidate_ClosedNestedObjects(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "typo in titleMeasures item",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: 1x1
  titleMeasures:
    - name: Revenue
      unit: EUR
      uni: typo
  children:
    - kind: Text
      metadata:
        name: t1
      spec:
        value: hello
`,
		},
		{
			name: "typo in chart stack",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: 1x1
  children:
    - kind: ChartStructure
      metadata:
        name: c1
      spec:
        dataset: ds
        scenarios: [ac1]
        stack:
          by: scenarios
          mod: absolute
`,
		},
		{
			name: "typo in table thereof item",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: 1x1
  children:
    - kind: Table
      metadata:
        name: t1
      spec:
        dataset: ds
        scenarios: [ac1]
        thereof:
          - rowGroup: Umsatz
            categorie: typo
`,
		},
		{
			name: "typo in table attributes item",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: 1x1
  children:
    - kind: Table
      metadata:
        name: t1
      spec:
        dataset: ds
        scenarios: [ac1]
        attributes:
          - labl: Verkaufsleiter
            expression: set(_leiter)
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected validation error for unknown nested key, got nil")
			}

			ve := &ValidationError{}
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Message, "not allowed") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected an 'additional properties not allowed' issue, got: %v", ve.Errors)
			}
		})
	}
}

func TestValidate_TableAttributes(t *testing.T) {
	tableDoc := func(attributes string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: full
  children:
    - kind: Table
      metadata:
        name: t1
      spec:
        dataset: ds
        scenarios: [ac1]
        attributes: ` + attributes + `
`
	}

	tests := []struct {
		name       string
		attributes string
		wantErr    bool
	}{
		{
			name: "array form with set, sum, and lit",
			attributes: `
          - label: Verkaufsleiter
            expression: set(_leiter)
          - label: Umsatz gesamt
            expression: sum(ac1)
          - label: Region
            expression: lit(Fixed Value)`,
			wantErr: false,
		},
		{
			name:       "string form",
			attributes: `'{"Verkaufsleiter": "set(_leiter)", "Umsatz gesamt": "sum(ac1)"}'`,
			wantErr:    false,
		},
		{
			name: "unknown function",
			attributes: `
          - label: Anzahl
            expression: count(ac1)`,
			wantErr: true,
		},
		{
			name: "empty argument",
			attributes: `
          - label: Summe
            expression: sum()`,
			wantErr: true,
		},
		{
			name: "invalid field identifier",
			attributes: `
          - label: Summe
            expression: sum(1abc)`,
			wantErr: true,
		},
		{
			name: "bare field without function",
			attributes: `
          - label: Summe
            expression: ac1`,
			wantErr: true,
		},
		{
			name: "missing label",
			attributes: `
          - expression: sum(ac1)`,
			wantErr: true,
		},
		{
			name: "missing expression",
			attributes: `
          - label: Summe`,
			wantErr: true,
		},
		{
			// The YAML map form is rejected on purpose: the loader alphabetizes
			// map keys, which would silently reorder columns.
			name: "map form rejected",
			attributes: `
          Verkaufsleiter: set(_leiter)`,
			wantErr: true,
		},
		{
			name:       "non-object string rejected",
			attributes: `"hello"`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tableDoc(tt.attributes)))
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected document to validate, got: %v", err)
			}
		})
	}
}

// tableTitle was renamed to sumTitle. Because tableSpecBase sets
// additionalProperties:false, the old name must now be rejected everywhere a
// Table spec can appear — standalone, as a layout child, and as a tree node.
func TestValidate_TableSumTitle(t *testing.T) {
	standalone := func(prop string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t1
spec:
  dataset: ds
  type: sum
  ` + prop + `
`
	}
	layoutChild := func(prop string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  pageLayout: full
  children:
    - kind: Table
      metadata:
        name: t1
      spec:
        dataset: ds
        type: sum
        ` + prop + `
`
	}
	treeNode := func(prop string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: tree1
spec:
  edges: []
  nodes:
    - id: n1
      kind: Table
      spec:
        dataset: ds
        type: sum
        ` + prop + `
`
	}

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{name: "sumTitle on a standalone Table", yaml: standalone(`sumTitle: Total`), wantErr: false},
		{name: "sumTitle on a layout child", yaml: layoutChild(`sumTitle: Total`), wantErr: false},
		{name: "sumTitle on a tree node", yaml: treeNode(`sumTitle: Total`), wantErr: false},
		{name: "legacy tableTitle on a standalone Table", yaml: standalone(`tableTitle: Total`), wantErr: true},
		{name: "legacy tableTitle on a layout child", yaml: layoutChild(`tableTitle: Total`), wantErr: true},
		{name: "legacy tableTitle on a tree node", yaml: treeNode(`tableTitle: Total`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected document to validate, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), "tableTitle") {
				t.Errorf("error should name the offending property, got: %v", err)
			}
		})
	}
}

func TestValidate_WrongType(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantPath    string
		wantMessage string
	}{
		{
			name: "constraints not array",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
  constraints: "not an array"
spec:
  query: SELECT 1
`,
			wantPath:    "metadata.constraints",
			wantMessage: "want array",
		},
		{
			name: "labels not object",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
  labels: "not an object"
spec:
  query: SELECT 1
`,
			wantPath:    "metadata.labels",
			wantMessage: "want object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			ve := &ValidationError{}
			ok := errors.As(err, &ve)
			if !ok {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Path, tt.wantPath) &&
					strings.Contains(issue.Message, tt.wantMessage) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected issue with path containing %q and message containing %q, got: %v",
					tt.wantPath, tt.wantMessage, ve.Errors)
			}
		})
	}
}

func TestValidate_UnknownKind(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: UnknownKind
metadata:
  name: test
spec:
  foo: bar
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}

	ve := &ValidationError{}
	ok := errors.As(err, &ve)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	// Should have an error about kind not being valid
	found := false
	for _, issue := range ve.Errors {
		if strings.Contains(issue.Path, "kind") ||
			strings.Contains(issue.Message, "enum") ||
			strings.Contains(issue.Message, "must be one of") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected issue about invalid kind, got: %v", ve.Errors)
	}
}

func TestValidate_InvalidYAML(t *testing.T) {
	yaml := `
this is not: valid: yaml: syntax
  - broken
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}

	ve := &ValidationError{}
	ok := errors.As(err, &ve)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	if len(ve.Errors) == 0 {
		t.Error("expected at least one error")
	}

	if !strings.Contains(ve.Errors[0].Message, "invalid YAML") {
		t.Errorf("expected 'invalid YAML' message, got: %s", ve.Errors[0].Message)
	}
}

func TestValidate_AdditionalPropertyPath(t *testing.T) {
	// additionalProperties failures must be pathed at the offending key (not
	// the parent object) so line/column resolution points at the right line.
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  query: SELECT 1
  level: warning
`

	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for unknown key, got nil")
	}

	ve := &ValidationError{}
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	found := false
	for _, issue := range ve.Errors {
		if issue.Path != "spec.level" {
			continue
		}
		found = true
		if !strings.Contains(issue.Message, "'level' not allowed") {
			t.Errorf("expected message to name 'level', got: %s", issue.Message)
		}
		if issue.Value != "warning" {
			t.Errorf("expected value 'warning', got: %v", issue.Value)
		}
	}
	if !found {
		t.Errorf("expected an issue with path 'spec.level', got: %v", ve.Errors)
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{
		Errors: []ValidationIssue{
			{Path: "metadata.name", Message: "name is required"},
			{Path: "spec.query", Message: "query must be a string"},
		},
	}

	errStr := ve.Error()
	if !strings.Contains(errStr, "metadata.name") {
		t.Errorf("expected error to contain 'metadata.name', got: %s", errStr)
	}
	if !strings.Contains(errStr, "spec.query") {
		t.Errorf("expected error to contain 'spec.query', got: %s", errStr)
	}
}

func TestIsValidationError(t *testing.T) {
	ve := &ValidationError{Errors: []ValidationIssue{{Path: "test", Message: "test"}}}

	if !IsValidationError(ve) {
		t.Error("expected IsValidationError to return true for *ValidationError")
	}

	if IsValidationError(nil) {
		t.Error("expected IsValidationError to return false for nil")
	}

	if IsValidationError(fmt.Errorf("not a validation error")) {
		t.Error("expected IsValidationError to return false for non-ValidationError")
	}
}

func TestValidate_LayoutPageDateFields(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "date-only values",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  titleDateStart: 2025-01-31
  titleDateEnd: 2025-12-31
  children:
    - kind: Text
      metadata:
        name: t
      spec:
        value: hello
`,
		},
		{
			name: "quoted datetime values",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: test-page
spec:
  titleDateStart: "2025-01-31T08:00:00Z"
  titleDateEnd: "2025-12-31T23:59:59+01:00"
  children:
    - kind: Text
      metadata:
        name: t
      spec:
        value: hello
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestConvertYAMLToJSON_TimeNormalization(t *testing.T) {
	// YAML parses bare dates as time.Time; convertYAMLToJSON should
	// normalize midnight UTC to date-only strings.
	tests := []struct {
		name     string
		yaml     string
		wantDate string
	}{
		{
			name:     "bare date becomes date-only string",
			yaml:     "date: 2025-01-31",
			wantDate: "2025-01-31",
		},
		{
			name:     "quoted date stays string",
			yaml:     `date: "2025-06-15"`,
			wantDate: "2025-06-15",
		},
		{
			name:     "quoted datetime preserved",
			yaml:     `date: "2025-06-15T14:30:00Z"`,
			wantDate: "2025-06-15T14:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc any
			if err := yamlPkg.Unmarshal([]byte(tt.yaml), &doc); err != nil {
				t.Fatalf("YAML unmarshal error: %v", err)
			}
			converted := convertYAMLToJSON(doc)
			m, ok := converted.(map[string]any)
			if !ok {
				t.Fatalf("expected map, got %T", converted)
			}
			got, ok := m["date"].(string)
			if !ok {
				t.Fatalf("expected string for date, got %T: %v", m["date"], m["date"])
			}
			if got != tt.wantDate {
				t.Errorf("got %q, want %q", got, tt.wantDate)
			}
		})
	}
}

func TestGetValidationIssues(t *testing.T) {
	issues := []ValidationIssue{
		{Path: "test1", Message: "message1"},
		{Path: "test2", Message: "message2"},
	}
	ve := &ValidationError{Errors: issues}

	got := GetValidationIssues(ve)
	if len(got) != len(issues) {
		t.Errorf("expected %d issues, got %d", len(issues), len(got))
	}

	if GetValidationIssues(nil) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestValidate_ScopedNames(t *testing.T) {
	doc := func(kind, name, spec string) string {
		return fmt.Sprintf("apiVersion: bino.bi/v1alpha1\nkind: %s\nmetadata:\n  name: %q\nspec:\n%s", kind, name, spec)
	}

	valid := []struct {
		name string
		yaml string
	}{
		{"scoped Table", doc("Table", "@acme/revenue-table", "  dataset: revenue\n")},
		{"scoped Text", doc("Text", "@acme/intro_text", "  value: hello\n")},
		{"scoped minimal tokens", doc("Text", "@a1/x", "  value: hello\n")},
		{"unscoped name still valid", doc("Text", "intro_text", "  value: hello\n")},
		{"scoped name with package segment", doc("Text", "@acme/kit/waterfall", "  value: hello\n")},
		{"scoped DataSource", doc("DataSource", "@acme/revenue-table", "  type: csv\n  path: data/x.csv\n")},
		{"unscoped DataSource still valid", doc("DataSource", "revenue_table", "  type: csv\n  path: data/x.csv\n")},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate([]byte(tt.yaml)); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		yaml string
	}{
		{"scope without name", doc("Text", "@acme", "  value: hello\n")},
		{"scope with empty name", doc("Text", "@acme/", "  value: hello\n")},
		{"uppercase scope", doc("Text", "@Acme/x", "  value: hello\n")},
		{"two package segments", doc("Text", "@acme/kit/sub/deep", "  value: hello\n")},
		{"uppercase package segment", doc("Text", "@acme/Kit/x", "  value: hello\n")},
		{"empty package segment", doc("Text", "@acme//x", "  value: hello\n")},
		{"package segment without definition", doc("Text", "@acme/kit/", "  value: hello\n")},
		{"package segment without scope", doc("Text", "kit/waterfall", "  value: hello\n")},
		{"at sign inside name", doc("Text", "x@y", "  value: hello\n")},
		{"empty scope", doc("Text", "@/x", "  value: hello\n")},
		{"name part starts with hyphen", doc("Text", "@acme/-x", "  value: hello\n")},
		{"name part ends with hyphen", doc("Text", "@acme/x-", "  value: hello\n")},
		{"unscoped DataSource with hyphen rejected", doc("DataSource", "revenue-table", "  type: csv\n  path: data/x.csv\n")},
		{"unscoped DataSource uppercase rejected", doc("DataSource", "Revenue_Table", "  type: csv\n  path: data/x.csv\n")},
		{"scoped DataSource uppercase name rejected", doc("DataSource", "@acme/Revenue_Table", "  type: csv\n  path: data/x.csv\n")},
		{"scoped DataSource nested slash rejected", doc("DataSource", "@acme/revenue/table", "  type: csv\n  path: data/x.csv\n")},
		{"scoped DataSource name starts with hyphen rejected", doc("DataSource", "@acme/-revenue", "  type: csv\n  path: data/x.csv\n")},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Path, "metadata") && strings.Contains(issue.Path, "name") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected an issue on metadata.name, got: %v", ve.Errors)
			}
		})
	}
}

func TestValidate_RefParams(t *testing.T) {
	valid := []struct {
		name string
		yaml string
	}{
		{
			name: "layout child ref with params",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: commentary-page
spec:
  children:
    - kind: Text
      ref: "@thatscalaguy/test"
      params:
        REGION: test
`,
		},
		{
			name: "grid child ref with params",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata:
  name: test-grid
spec:
  rowHeaders: ["r1"]
  columnHeaders: ["c1"]
  children:
    - row: 0
      column: 0
      kind: Table
      ref: "@acme/revenue-table"
      params:
        REGION: EU
`,
		},
		{
			name: "tree node ref with params",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: test-tree
spec:
  edges: []
  nodes:
    - id: root
      kind: Table
      ref: "@acme/revenue-table"
      params:
        REGION: EU
`,
		},
		{
			name: "component document declaring metadata.params",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: "@thatscalaguy/test"
  params:
    - name: REGION
      type: string
      default: EU
spec:
  value: "Report for ${REGION}"
`,
		},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate([]byte(tt.yaml)); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		yaml string
	}{
		{
			name: "layout child params without ref",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: commentary-page
spec:
  children:
    - kind: Text
      params:
        REGION: test
      spec:
        value: hello
`,
		},
		{
			name: "grid child params without ref",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata:
  name: test-grid
spec:
  rowHeaders: ["r1"]
  columnHeaders: ["c1"]
  children:
    - row: 0
      column: 0
      kind: Table
      params:
        REGION: EU
      spec:
        dataset: revenue
`,
		},
		{
			name: "tree node params without ref",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: test-tree
spec:
  edges: []
  nodes:
    - id: root
      kind: Table
      params:
        REGION: EU
      spec:
        dataset: revenue
`,
		},
		{
			name: "layout child param value must be a string",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: commentary-page
spec:
  children:
    - kind: Text
      ref: "@thatscalaguy/test"
      params:
        YEAR: 2024
`,
		},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate([]byte(tt.yaml)); err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestValidate_LiveReportArtefactPWA(t *testing.T) {
	liveDoc := func(pwa string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: live_dashboard
spec:
  title: Dashboard
  routes:
    "/":
      artefact: home
  pwa:` + pwa + `
`
	}

	tests := []struct {
		name    string
		pwa     string
		wantErr bool
	}{
		{
			name: "full pwa block with any and maskable icons",
			pwa: `
    name: Sales Dashboard
    shortName: Sales
    description: Quarterly sales
    themeColor: "#0B5FFF"
    backgroundColor: "#FFFFFF"
    display: standalone
    icons:
      - asset: app_icon
        sizes: 512x512
        purpose: any
      - asset: app_icon_maskable
        sizes: 512x512
        purpose: maskable`,
			wantErr: false,
		},
		{
			name: "empty icons array",
			pwa: `
    icons: []`,
			wantErr: true,
		},
		{
			name: "sizes not WIDTHxHEIGHT",
			pwa: `
    icons:
      - asset: app_icon
        sizes: "512"`,
			wantErr: true,
		},
		{
			name: "invalid display mode",
			pwa: `
    display: kiosk
    icons:
      - asset: app_icon
        sizes: 512x512`,
			wantErr: true,
		},
		{
			name: "invalid icon purpose",
			pwa: `
    icons:
      - asset: app_icon
        sizes: 512x512
        purpose: monochrome`,
			wantErr: true,
		},
		{
			name: "unknown field inside pwa",
			pwa: `
    orientation: landscape
    icons:
      - asset: app_icon
        sizes: 512x512`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(liveDoc(tt.pwa)))
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected document to validate, got: %v", err)
			}
		})
	}
}
