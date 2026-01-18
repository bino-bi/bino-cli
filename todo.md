# Implementation Tasks for bino-cli Refactoring

These tasks address critical issues identified in `Review.md`. Each task is self-contained and should be implemented in order. **Before implementing any task, create a detailed implementation plan and get it approved.**

---

## Task 1: Create Canonical Schema Package with Core Types

### Context
The codebase has schema types scattered across multiple locations:
- `internal/cli/add_*.go` — "builder structs" like `DataSetManifestData`, `ReportArtefactManifestData`
- `internal/report/spec/*.go` — "parser types" like `InlineDataSet`, `InlineDataSource`
- `internal/report/render/spec.go` — rendering types

This causes schema drift: adding a field requires changes in 3+ places.

### Objective
Create a new `internal/schema/` package that will become the single source of truth for all YAML manifest types.

### Requirements

1. Create `internal/schema/types.go` with these structs:
   ```go
   // Document is the envelope for all manifest kinds
   type Document struct {
       APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
       Kind       string            `yaml:"kind" json:"kind"`
       Metadata   Metadata          `yaml:"metadata" json:"metadata"`
       Spec       map[string]any    `yaml:"spec" json:"spec"`
   }

   type Metadata struct {
       Name        string            `yaml:"name" json:"name"`
       Description string            `yaml:"description,omitempty" json:"description,omitempty"`
       Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
       Constraints []string          `yaml:"constraints,omitempty" json:"constraints,omitempty"`
   }
   ```

2. Create `internal/schema/datasource.go` with `DataSourceSpec`:
   - Look at `internal/report/spec/datasetref.go` lines 168-190 for `InlineDataSource`
   - Look at `internal/cli/add_datasource.go` for the fields used in generation
   - The struct must support all datasource types: csv, parquet, excel, json, inline, postgres_query, mysql_query
   - Include a `DataSourceType` enum as `type DataSourceType string` with constants

3. Create `internal/schema/dataset.go` with `DataSetSpec`:
   - Look at `internal/report/spec/datasetref.go` lines 28-50 for `InlineDataSet`
   - Must handle: query (inline or $file), prql (inline or $file), source, dependencies
   - Create `QueryField` struct that can marshal as scalar string or `$file: path` map

4. Create `internal/schema/reportartefact.go` with `ReportArtefactSpec`:
   - Look at `internal/cli/add_report.go` lines 18-28 for fields
   - Look at `internal/report/config/artefacts.go` for parsing

5. Add proper `yaml` and `json` struct tags with `omitempty` where fields are optional.

### Files to Read First
- `internal/report/spec/datasetref.go` — existing InlineDataSet, InlineDataSource, DataSourceRef, DatasetRef
- `internal/cli/add_dataset.go` lines 138-157 — DataSetManifestData
- `internal/cli/add_datasource.go` — DataSourceManifestData
- `internal/cli/add_report.go` lines 18-53 — ReportArtefactManifestData, LiveReportArtefactManifestData, SigningProfileManifestData
- `internal/report/config/artefacts.go` — ReportArtefactSpec used in parsing

### Acceptance Criteria
- [ ] `internal/schema/` package exists with types.go, datasource.go, dataset.go, reportartefact.go
- [ ] All structs have both `yaml` and `json` struct tags
- [ ] `DataSourceType` is a string enum with all 8 types
- [ ] Package compiles with `go build ./internal/schema/...`
- [ ] No imports from `internal/cli/` (schema should be independent)

### Instructions
1. **First, create a detailed plan** listing all structs you will create and their fields
2. Show field mappings: which existing type each field comes from
3. Get plan approved before writing code
4. Implement incrementally, testing compilation after each file

---

## Task 2: Add Custom YAML Marshal/Unmarshal for Union Types

### Context
Several schema fields have "union" semantics:
- `QueryField`: can be a plain string (inline SQL) or `$file: path/to/query.sql`
- `DataSourceRef`: can be a string reference (`$datasource_name`) or inline object
- `DatasetRef`: can be a string reference or inline DataSet object

The current code in `internal/report/spec/datasetref.go` handles this for JSON unmarshaling but not for YAML marshaling (which the CLI needs).

### Objective
Add `MarshalYAML()` and `UnmarshalYAML()` methods to the schema types created in Task 1.

### Requirements

1. **QueryField** in `internal/schema/dataset.go`:
   ```go
   type QueryField struct {
       Inline string // If non-empty, marshal as scalar
       File   string // If non-empty, marshal as {$file: path}
   }
   ```
   - `MarshalYAML()`: if `Inline` is set, return scalar; if `File` is set, return `map[string]string{"$file": f.File}`
   - `UnmarshalYAML()`: if scalar node, set `Inline`; if map with `$file`, set `File`

2. **DataSourceRef** in `internal/schema/datasource.go`:
   ```go
   type DataSourceRef struct {
       Ref    string            // String reference like "my_datasource"
       Inline *DataSourceSpec   // Inline definition
   }
   ```
   - `MarshalYAML()`: if `Ref` is set, return scalar; if `Inline` is set, return the spec
   - `UnmarshalYAML()`: if scalar, set `Ref`; if map, unmarshal into `Inline`

3. **DatasetRef** — same pattern as DataSourceRef

### Files to Read First
- `internal/report/spec/datasetref.go` — see existing `UnmarshalJSON` implementations
- `internal/report/spec/datasets.go` — see `DatasetList` unmarshaling for complex cases
- `gopkg.in/yaml.v3` documentation for `yaml.Node` usage

### Example Implementation Pattern
```go
func (q QueryField) MarshalYAML() (any, error) {
    if q.File != "" {
        return map[string]string{"$file": q.File}, nil
    }
    return q.Inline, nil
}

func (q *QueryField) UnmarshalYAML(node *yaml.Node) error {
    if node.Kind == yaml.ScalarNode {
        q.Inline = node.Value
        return nil
    }
    if node.Kind == yaml.MappingNode {
        var m map[string]string
        if err := node.Decode(&m); err != nil {
            return err
        }
        if f, ok := m["$file"]; ok {
            q.File = f
            return nil
        }
    }
    return fmt.Errorf("invalid query field: expected string or $file map")
}
```

### Acceptance Criteria
- [ ] `QueryField` marshals/unmarshals correctly for both inline and $file cases
- [ ] `DataSourceRef` marshals/unmarshals correctly for both ref and inline cases
- [ ] `DatasetRef` marshals/unmarshals correctly for both ref and inline cases
- [ ] Unit tests in `internal/schema/marshal_test.go` cover all cases
- [ ] Round-trip test: create struct → marshal → unmarshal → compare

### Instructions
1. **First, create a plan** showing the marshal/unmarshal logic for each union type
2. Include test cases you will write
3. Get plan approved before implementing
4. Write tests first (TDD), then implement the methods

---

## Task 3: Add Schema Validation Function

### Context
The codebase has a JSON schema at `internal/report/spec/schema/document.schema.json` (2209 lines) that validates parsed YAML. However, the `bino add` commands generate YAML without validating it against this schema.

### Objective
Create a `Validate()` function in the schema package that validates YAML bytes against the JSON schema.

### Requirements

1. Create `internal/schema/validate.go`:
   ```go
   // Validate checks that yamlBytes represents a valid manifest document.
   // Returns nil if valid, or a ValidationError with details.
   func Validate(yamlBytes []byte) error

   // ValidationError contains structured validation failure information.
   type ValidationError struct {
       Errors []ValidationIssue
   }

   type ValidationIssue struct {
       Path    string // JSON path like "spec.query"
       Message string // Human-readable error
   }
   ```

2. Reuse the existing JSON schema from `internal/report/spec/schema/document.schema.json`:
   - Use `go:embed` to embed the schema
   - Use `github.com/xeipuuv/gojsonschema` (already a dependency) for validation

3. The function should:
   - Convert YAML to JSON (the schema is JSON Schema)
   - Validate against the embedded schema
   - Return structured errors with paths

### Files to Read First
- `internal/report/spec/validator.go` — existing `ValidateDocument()` function
- `internal/report/spec/schema/document.schema.json` — the schema itself
- Usage of gojsonschema in the codebase

### Acceptance Criteria
- [ ] `schema.Validate(yamlBytes)` returns nil for valid manifests
- [ ] Returns `ValidationError` with specific path and message for invalid manifests
- [ ] Schema is embedded (not read from disk at runtime)
- [ ] Unit tests cover: valid document, missing required field, wrong type, unknown kind
- [ ] Function is usable without importing `internal/report/spec`

### Instructions
1. **First, plan** how you'll structure the validation and error types
2. List 5+ test cases you'll implement
3. Get plan approved
4. Consider whether to copy the schema file or import from spec package
5. Implement with tests

---

## Task 4: Refactor CLI to Use Schema Types for DataSet

### Context
Currently `internal/cli/add_dataset.go` defines its own `DataSetManifestData` struct and generates YAML via string concatenation in `RenderDataSetManifest()`. This should use the canonical `schema.DataSetSpec` type instead.

### Objective
Refactor `bino add dataset` to:
1. Populate `schema.Document` with `schema.DataSetSpec`
2. Use `yaml.Marshal()` instead of string building
3. Validate output before writing

### Requirements

1. **Remove from `internal/cli/add_dataset.go`:**
   - `DataSetManifestData` struct (lines 138-157)
   - `RenderDataSetManifest()` function

2. **Modify the command to:**
   - Build a `schema.Document` with `Kind: "DataSet"` and `schema.DataSetSpec`
   - Call `yaml.Marshal()` on the document
   - Call `schema.Validate()` on the marshaled bytes
   - Write to file

3. **Update `writeDataSetManifest()`** to accept `schema.Document` instead of `DataSetManifestData`

4. **Ensure all features still work:**
   - Inline SQL query
   - SQL file reference (`$file: path`)
   - PRQL (inline and file)
   - Pass-through source
   - Dependencies
   - Constraints
   - Description

### Files to Modify
- `internal/cli/add_dataset.go`

### Files to Read First
- `internal/cli/add_dataset.go` — current implementation
- `internal/schema/types.go` — new canonical types (from Task 1)
- `internal/schema/dataset.go` — DataSetSpec (from Task 1)

### Example Transformation
```go
// Before:
data := DataSetManifestData{
    Name:  name,
    Query: flagSQL,
}
manifest := RenderDataSetManifest(data)

// After:
doc := &schema.Document{
    APIVersion: "bino.bi/v1alpha1",
    Kind:       "DataSet",
    Metadata: schema.Metadata{
        Name:        name,
        Description: flagDesc,
        Constraints: flagConstraint,
    },
}
spec := &schema.DataSetSpec{}
if flagSQL != "" {
    spec.Query = &schema.QueryField{Inline: flagSQL}
} else if flagSQLFile != "" {
    spec.Query = &schema.QueryField{File: flagSQLFile}
}
doc.Spec = spec

yamlBytes, err := yaml.Marshal(doc)
if err != nil {
    return fmt.Errorf("marshal manifest: %w", err)
}
if err := schema.Validate(yamlBytes); err != nil {
    return fmt.Errorf("generated invalid manifest: %w", err)
}
```

### Acceptance Criteria
- [ ] `DataSetManifestData` struct deleted
- [ ] `RenderDataSetManifest()` function deleted
- [ ] `bino add dataset` uses `schema.Document` and `yaml.Marshal()`
- [ ] Output is validated with `schema.Validate()` before writing
- [ ] All existing functionality preserved (test manually with various flag combinations)
- [ ] Generated YAML is semantically identical to before (compare output)

### Instructions
1. **First, create a plan** mapping each field from `DataSetManifestData` to `schema.DataSetSpec`
2. List the test scenarios you'll verify manually
3. Get plan approved
4. Implement incrementally, testing each feature
5. Keep the old code commented until you verify the new code works

---

## Task 5: Refactor CLI to Use Schema Types for DataSource

### Context
Same issue as Task 4, but for `bino add datasource`. The `DataSourceManifestData` struct and `RenderDataSourceManifest()` function should be replaced with canonical schema types.

### Objective
Refactor `bino add datasource` to use `schema.Document` and `schema.DataSourceSpec`.

### Requirements

1. **Remove from `internal/cli/add_datasource.go`:**
   - `DataSourceManifestData` struct
   - `RenderDataSourceManifest()` function

2. **Also remove from `internal/cli/add.go`:**
   - `DataSourceType` int enum (lines 353-364) — replaced by `schema.DataSourceType` string enum

3. **Handle all datasource types:**
   - CSV: path, delimiter, header options
   - Parquet: path
   - Excel: path, sheet
   - JSON: path
   - Inline: content as YAML array
   - PostgreSQL query: connection, query
   - MySQL query: connection, query

4. **Validate generated YAML** with `schema.Validate()`

### Files to Modify
- `internal/cli/add_datasource.go`
- `internal/cli/add.go` (remove DataSourceType enum)

### Files to Read First
- `internal/cli/add_datasource.go` — current implementation
- `internal/cli/add.go` lines 353-400 — DataSourceType enum
- `internal/schema/datasource.go` — new canonical types (from Task 1)

### Acceptance Criteria
- [ ] `DataSourceManifestData` struct deleted
- [ ] `RenderDataSourceManifest()` function deleted
- [ ] `DataSourceType` int enum deleted from add.go
- [ ] All 8 datasource types work correctly
- [ ] Output validated before writing
- [ ] Generated YAML matches expected format

### Instructions
1. **First, create a plan** mapping each datasource type's fields
2. Show before/after for one complex type (e.g., postgres_query)
3. Get plan approved
4. Implement and test each datasource type individually

---

## Task 6: Refactor CLI to Use Schema Types for ReportArtefact, LiveReportArtefact, SigningProfile

### Context
Same pattern as Tasks 4-5, but for the three report-related manifest types in `internal/cli/add_report.go`.

### Objective
Refactor `bino add reportartefact`, `bino add livereportartefact`, and `bino add signingprofile` to use canonical schema types.

### Requirements

1. **Remove from `internal/cli/add_report.go`:**
   - `ReportArtefactManifestData` struct (lines 18-28)
   - `LiveReportArtefactManifestData` struct (lines 31-43)
   - `SigningProfileManifestData` struct (lines 46-53)
   - `RenderReportArtefactManifest()` function (lines 841-887)
   - `RenderLiveReportArtefactManifest()` function (lines 890-930)
   - `RenderSigningProfileManifest()` function (lines 934-970)

2. **Ensure schema types exist** (from Task 1):
   - `schema.ReportArtefactSpec`
   - `schema.LiveReportArtefactSpec`
   - `schema.SigningProfileSpec`

3. **Handle special cases:**
   - `layoutPages` are rendered as `$page_name` references
   - `routes` map in LiveReportArtefact
   - Certificate/privateKey paths in SigningProfile

### Files to Modify
- `internal/cli/add_report.go`

### Acceptance Criteria
- [ ] All three `*ManifestData` structs deleted
- [ ] All three `Render*Manifest()` functions deleted
- [ ] All three commands use schema types and `yaml.Marshal()`
- [ ] Output validated before writing
- [ ] Layout page references formatted correctly (with $ prefix)

### Instructions
1. **First, create a plan** for all three types
2. Start with SigningProfile (simplest), then ReportArtefact, then LiveReportArtefact
3. Get plan approved
4. Implement and test incrementally

---

## Task 7: Refactor Remaining Add Commands (Text, Table, Chart, Layout, Asset, Secret)

### Context
The remaining add commands also need refactoring to use schema types. These are in:
- `internal/cli/add_text.go`
- `internal/cli/add_visual.go` (Table, ChartStructure, ChartTime)
- `internal/cli/add_layout.go` (LayoutPage, LayoutCard)
- `internal/cli/add_asset.go`
- `internal/cli/add_secret.go` (ConnectionSecret)

### Objective
Complete the migration of all add commands to use canonical schema types.

### Requirements

1. **Create schema types** (if not already in Task 1):
   - `schema.TextSpec`
   - `schema.TableSpec`
   - `schema.ChartStructureSpec`
   - `schema.ChartTimeSpec`
   - `schema.LayoutPageSpec`
   - `schema.LayoutCardSpec`
   - `schema.AssetSpec`
   - `schema.ConnectionSecretSpec`

2. **For each command:**
   - Remove `*ManifestData` struct
   - Remove `Render*Manifest()` function
   - Use `schema.Document` + specific spec type
   - Validate before writing

3. **Handle visual component specifics:**
   - Dataset binding (string ref or inline)
   - Dimensions and measures arrays
   - Chart-specific options

### Files to Modify
- `internal/cli/add_text.go`
- `internal/cli/add_visual.go`
- `internal/cli/add_layout.go`
- `internal/cli/add_asset.go`
- `internal/cli/add_secret.go`

### Acceptance Criteria
- [ ] All `*ManifestData` structs removed from CLI package
- [ ] All `Render*Manifest()` functions removed
- [ ] All 16 add commands use schema types
- [ ] All output validated before writing
- [ ] No string-based YAML generation remains in CLI

### Instructions
1. **First, inventory** all remaining structs and render functions
2. **Plan schema types** needed (some may already exist from Task 1)
3. Get plan approved
4. Implement in order: Text → Asset → Secret → Layout → Visual (increasing complexity)

---

## Task 8: Create Generic Manifest Write Function

### Context
Every add command has a nearly identical `write*Manifest()` function with the same:
- Name validation
- Path resolution
- Append vs create logic
- Error handling

This is ~30 lines duplicated 16 times.

### Objective
Create a single generic function that all add commands use for writing manifests.

### Requirements

1. Create `internal/cli/manifest_writer.go`:
   ```go
   // WriteManifestFile writes a schema.Document to the specified path.
   // If appendMode is true, appends to existing multi-document YAML.
   // Validates the document before writing.
   func WriteManifestFile(
       cmd *cobra.Command,
       workdir string,
       doc *schema.Document,
       outputPath string,
       appendMode bool,
   ) error
   ```

2. The function should:
   - Validate `doc.Metadata.Name` with existing `ValidateName()`
   - Marshal document with `yaml.Marshal()`
   - Validate with `schema.Validate()`
   - Resolve absolute path
   - Call `AppendToManifest()` or `WriteManifest()` as appropriate
   - Print success message to `cmd.OutOrStdout()`

3. **Refactor all add commands** to use this function instead of their individual `write*Manifest()` functions.

4. **Delete** all individual `write*Manifest()` functions.

### Files to Create
- `internal/cli/manifest_writer.go`

### Files to Modify
- All `internal/cli/add_*.go` files

### Acceptance Criteria
- [ ] Single `WriteManifestFile()` function exists
- [ ] All 16 add commands use it
- [ ] All individual `write*Manifest()` functions deleted
- [ ] Behavior identical to before (same messages, same file output)
- [ ] ~400 lines of code removed

### Instructions
1. **First, analyze** all existing `write*Manifest()` functions for differences
2. **Plan** the generic function signature and behavior
3. Get plan approved
4. Implement generic function with tests
5. Migrate one command, verify it works
6. Migrate remaining commands

---

## Task 9: Add Round-Trip Golden Tests

### Context
There are no tests that verify YAML generated by `bino add` can be parsed by `bino build`. Schema drift between generator and parser is currently undetectable.

### Objective
Create comprehensive round-trip tests that:
1. Create schema structs
2. Marshal to YAML
3. Parse back
4. Verify semantic equivalence

### Requirements

1. Create `internal/schema/roundtrip_test.go` with tests for all 15 manifest kinds.

2. Create `internal/schema/testdata/golden/` directory with example YAML files:
   ```
   testdata/golden/
   ├── dataset_inline_sql.yaml
   ├── dataset_file_reference.yaml
   ├── dataset_with_deps.yaml
   ├── datasource_csv.yaml
   ├── datasource_postgres.yaml
   ├── reportartefact_pdf.yaml
   ├── text_simple.yaml
   ├── table_basic.yaml
   └── ... (one per manifest variation)
   ```

3. **Golden file test pattern:**
   ```go
   func TestGoldenFiles(t *testing.T) {
       files, _ := filepath.Glob("testdata/golden/*.yaml")
       for _, f := range files {
           t.Run(filepath.Base(f), func(t *testing.T) {
               original, _ := os.ReadFile(f)

               // Parse
               doc, err := schema.ParseDocument(original)
               require.NoError(t, err)

               // Re-marshal
               remarshaled, err := yaml.Marshal(doc)
               require.NoError(t, err)

               // Parse again
               reparsed, err := schema.ParseDocument(remarshaled)
               require.NoError(t, err)

               // Compare (use deep equality, not string comparison)
               assert.Equal(t, doc, reparsed)
           })
       }
   }
   ```

4. **Struct round-trip test pattern:**
   ```go
   func TestDataSetRoundTrip(t *testing.T) {
       cases := []schema.DataSetSpec{
           {Query: &schema.QueryField{Inline: "SELECT 1"}},
           {Query: &schema.QueryField{File: "queries/test.sql"}},
           {PRQL: &schema.QueryField{Inline: "from table"}},
           {Source: &schema.DataSourceRef{Ref: "my_source"}},
       }
       for _, tc := range cases {
           doc := &schema.Document{
               APIVersion: "bino.bi/v1alpha1",
               Kind:       "DataSet",
               Metadata:   schema.Metadata{Name: "test"},
           }
           doc.SetSpec(tc)

           yamlBytes, _ := yaml.Marshal(doc)
           parsed, _ := schema.ParseDocument(yamlBytes)

           // Compare specs
           assert.Equal(t, tc, parsed.GetSpec())
       }
   }
   ```

### Acceptance Criteria
- [ ] Golden file tests exist for all 15 manifest kinds
- [ ] At least 3 variations per complex kind (DataSet, DataSource, ReportArtefact)
- [ ] Round-trip struct tests cover all union type variations
- [ ] Tests run in CI
- [ ] All tests pass

### Instructions
1. **First, list** all the golden file variations needed
2. **Plan** the test structure and helper functions
3. Get plan approved
4. Create golden files first (can be hand-written or copied from existing examples)
5. Implement tests
6. Ensure tests are included in CI

---

## Task 10: Add golangci-lint Configuration and Fix Issues

### Context
The project has no linter configuration. Common issues like inconsistent error handling, unused code, and missing exhaustive switches are not caught.

### Objective
Add `golangci-lint` configuration and fix the issues it finds.

### Requirements

1. Create `.golangci.yml` at repository root:
   ```yaml
   run:
     timeout: 5m

   linters:
     enable:
       - errcheck
       - govet
       - ineffassign
       - staticcheck
       - unused
       - gosimple
       - gocritic
       - gofmt
       - goimports
       - misspell
       - unconvert
       - unparam
       - errorlint
       - nilerr

   linters-settings:
     gocritic:
       enabled-tags:
         - diagnostic
         - style
         - performance
     errorlint:
       errorf: true
       asserts: true
       comparison: true

   issues:
     exclude-rules:
       - path: _test\.go
         linters:
           - errcheck
           - gocritic
   ```

2. Run `golangci-lint run ./...` and categorize issues:
   - **Must fix:** errcheck, govet, staticcheck errors
   - **Should fix:** errorlint (improper error wrapping)
   - **Can defer:** style issues

3. Fix all "must fix" issues.

4. Add lint check to CI (if GitHub Actions workflow exists, add a lint job).

### Files to Create
- `.golangci.yml`

### Files to Potentially Modify
- Any files with linter errors
- `.github/workflows/*.yml` (add lint job)

### Acceptance Criteria
- [ ] `.golangci.yml` exists with sensible configuration
- [ ] `golangci-lint run ./...` passes (or only has acceptable warnings)
- [ ] CI runs linter on every PR
- [ ] No `errcheck`, `govet`, or `staticcheck` errors remain

### Instructions
1. **First, run** `golangci-lint run ./...` with a basic config
2. **Categorize** all findings by severity and effort to fix
3. **Plan** which issues to fix now vs defer
4. Get plan approved
5. Fix issues incrementally, committing after each category
6. Add CI integration last

---

## Task Dependency Order

```
Task 1 (Schema Types)
    ↓
Task 2 (Marshal/Unmarshal)
    ↓
Task 3 (Validation)
    ↓
┌───┴───┬───────┬───────┐
↓       ↓       ↓       ↓
Task 4  Task 5  Task 6  Task 7
(DataSet)(DataSrc)(Report)(Others)
    └───────┴───────┴───────┘
                ↓
            Task 8 (Generic Write)
                ↓
            Task 9 (Tests)
                ↓
            Task 10 (Linting)
```

Tasks 4-7 can be parallelized after Tasks 1-3 are complete.

---

## Notes for Coding Agents

1. **Always plan first.** Before writing any code, create a detailed implementation plan and present it for approval.

2. **Read referenced files.** Each task lists specific files to read. Understanding the existing code is critical.

3. **Preserve behavior.** The goal is refactoring, not feature changes. Generated YAML should be semantically identical.

4. **Test incrementally.** After each change, verify the affected command still works.

5. **Commit often.** Make small, focused commits with clear messages.

6. **Ask questions.** If a task is unclear or you find unexpected code, ask before proceeding.
