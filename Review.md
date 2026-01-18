# bino-cli Code Review

## Executive Summary

1. **Blocker: Schema duplication between `bino add` and `bino build`** — CLI commands define separate "builder structs" (`DataSetManifestData`, `ReportArtefactManifestData`, etc.) instead of reusing the canonical types in `internal/report/spec`. This means schema changes must be made in multiple places, and generated YAML is never validated against the same types used for parsing.

2. **Blocker: YAML generation via string concatenation** — All `Render*Manifest()` functions in the CLI package build YAML by string concatenation (`strings.Builder`), bypassing type safety entirely. Any field name typo, incorrect indentation, or missing quoting results in invalid YAML that isn't caught until runtime.

3. **Major: No round-trip testing** — There are no tests that generate a manifest via `bino add`, parse it back with the loader, and verify semantic equivalence. Schema drift between generator and parser is inevitable.

4. **Major: DataSource type enum duplicated** — `DataSourceType` is defined as an `int` enum in `internal/cli/add.go:353` with string mappings, while `datasource/spec.go` uses raw strings. The allowed values are implicit and scattered.

5. **Major: Validation only on parse, never on generation** — The JSON schema (`document.schema.json`) validates parsed documents but the `add` commands never validate their output against this schema before writing.

6. **Major: Repeated boilerplate across 16 add commands** — Each `add_*.go` file duplicates the same patterns: flag parsing, validation, prompt flows, file writing, and error handling. ~6,000 lines could be reduced to ~2,000 with proper abstraction.

7. **Minor: Inconsistent error wrapping** — Some functions wrap errors with `fmt.Errorf("...: %w", err)`, others return raw errors. Sentinel errors are not used consistently.

8. **Minor: No linter configuration committed** — No `.golangci.yml` or equivalent is present; linting rules are not enforced in CI.

---

## Architecture & Maintainability

### Current Package Structure

```
internal/
├── cli/                  # 16,441 lines - Commands + "builder" structs + YAML rendering
├── report/
│   ├── config/          # Document loading, inline materialization, validation
│   ├── spec/            # JSON schema validation, type unions (DatasetRef, etc.)
│   ├── datasource/      # DataSource materialization, DuckDB views
│   ├── dataset/         # Query execution
│   ├── render/          # HTML generation, component specs
│   ├── lint/            # Validation rules
│   ├── graph/           # Dependency analysis
│   ├── pipeline/        # Build orchestration
│   └── buildlog/        # Build logging
```

### Key Problems

1. **CLI package has too many responsibilities:**
   - Command handlers (appropriate)
   - Schema types for YAML generation (inappropriate — duplicates `spec`)
   - YAML rendering logic (inappropriate — should be in `report/schema`)
   - Interactive prompt logic (borderline — could be a subpackage)

2. **`spec` package conflates two concerns:**
   - JSON Schema validation (technical mechanism)
   - Go type definitions for the schema (domain model)

   These should be separated so the domain types can be imported without bringing in the JSON schema validator.

3. **`render/spec.go` defines additional component structs** (e.g., `ChartStructureSpec`, `TableSpec`) that are used for HTML rendering but are not the same types used in `config/inline.go` or `cli/add_*.go`. This is a third parallel definition.

### Recommended Refactor

```
internal/
├── cli/                  # Commands only, no schema types
│   ├── add/             # Add subcommands (thin wrappers)
│   └── prompts/         # Interactive prompt utilities
├── schema/              # NEW: Single source of truth for YAML schema
│   ├── types.go         # All manifest kinds as Go structs
│   ├── marshal.go       # yaml.Marshal/Unmarshal with proper tags
│   ├── validate.go      # JSON schema + semantic validation
│   └── version.go       # Schema version, migration helpers
├── report/
│   ├── config/          # Uses schema.* types for loading
│   ├── datasource/      # Uses schema.DataSourceSpec
│   ├── dataset/         # Uses schema.DataSetSpec
│   └── ...
```

**Migration steps:**
1. Create `internal/schema/types.go` with canonical structs for all 15 manifest kinds.
2. Add proper `yaml` struct tags with `omitempty` where appropriate.
3. Move JSON schema validation into `internal/schema/validate.go`.
4. Refactor `config.LoadDir()` to unmarshal into `schema.*` types.
5. Refactor `cli/add_*.go` to populate `schema.*` types and marshal them.
6. Delete all `Render*Manifest()` functions and `*ManifestData` structs from CLI.
7. Add round-trip tests.

---

## YAML Report Schema: Single Source of Truth (Critical)

### Current State: Three Parallel Schema Definitions

| Location | Purpose | Example Types |
|----------|---------|---------------|
| `cli/add_*.go` | Generate YAML | `DataSetManifestData`, `ReportArtefactManifestData`, `DataSourceManifestData` |
| `report/spec/*.go` | Parse/validate | `InlineDataSet`, `InlineDataSource`, `DatasetRef`, `DataSourceRef` |
| `report/render/spec.go` | HTML rendering | `ChartStructureSpec`, `TableSpec`, `TextSpec` |

### Why This Is Fragile

1. **Adding a field** (e.g., `spec.timeout` on DataSet) requires changes in:
   - `document.schema.json` (JSON Schema)
   - `spec/datasetref.go` (if inline)
   - `render/spec.go` (if used in rendering)
   - `cli/add_dataset.go` (`DataSetManifestData` struct + `RenderDataSetManifest()`)
   - Possibly `datasource/spec.go` or `dataset/executor.go`

2. **Field name typos in string-based rendering** compile successfully:
   ```go
   // cli/add_report.go:868 — what if this said "orentation"?
   b.WriteString(fmt.Sprintf("  orientation: %s\n", data.Orientation))
   ```

3. **No compile-time check** that generated YAML matches the schema.

### Proposed Canonical Schema

Create `internal/schema/types.go`:

```go
package schema

// Document is the envelope for all manifest kinds.
type Document struct {
    APIVersion string   `yaml:"apiVersion"`
    Kind       string   `yaml:"kind"`
    Metadata   Metadata `yaml:"metadata"`
    Spec       any      `yaml:"-"` // Populated based on Kind
}

type Metadata struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description,omitempty"`
    Labels      map[string]string `yaml:"labels,omitempty"`
    Constraints []string          `yaml:"constraints,omitempty"`
}

// DataSetSpec is the spec for Kind=DataSet.
type DataSetSpec struct {
    Query        *QueryField      `yaml:"query,omitempty"`
    PRQL         *QueryField      `yaml:"prql,omitempty"`
    Source       *DataSourceRef   `yaml:"source,omitempty"`
    Dependencies []DataSourceRef  `yaml:"dependencies,omitempty"`
}

type QueryField struct {
    Inline string `yaml:"-"` // If set, marshal as scalar
    File   string `yaml:"-"` // If set, marshal as "$file: path"
}

// Custom marshaler to handle scalar vs map representation
func (q QueryField) MarshalYAML() (any, error) { ... }
func (q *QueryField) UnmarshalYAML(node *yaml.Node) error { ... }

// DataSourceRef can be a string ref or inline definition.
type DataSourceRef struct {
    Ref    string             `yaml:"-"`
    Inline *InlineDataSource  `yaml:"-"`
}

func (r DataSourceRef) MarshalYAML() (any, error) { ... }
func (r *DataSourceRef) UnmarshalYAML(node *yaml.Node) error { ... }

// ... similar for all 15 kinds
```

### How `bino build` and `bino add` Should Share

| Concern | Current | Proposed |
|---------|---------|----------|
| **Types** | Separate structs | Single `schema.*` types |
| **Validation** | JSON schema on parse only | JSON schema + Go validation on both parse and generate |
| **Marshaling** | String building (add) vs `json.Unmarshal` (build) | `yaml.Marshal` / `yaml.Unmarshal` everywhere |
| **Versioning** | `apiVersion` field exists but unused | Add `schema.Migrate(doc, targetVersion)` for future evolution |

### Enforcement Strategy

1. **Compile-time:** `bino add` uses the same structs as `bino build`.
2. **Test-time:** Round-trip golden tests (see Testing section).
3. **CI:** `go vet` + `golangci-lint` with `exhaustruct` to catch missing fields.

---

## DRY Violations & Refactors

### 1. Write Function Duplication

**Location:** `cli/add_dataset.go:677`, `cli/add_report.go:751`, `cli/add_datasource.go:689`, etc.

**Problem:** Every `write*Manifest()` function has identical structure:
```go
func writeDataSetManifest(cmd *cobra.Command, workdir string, data DataSetManifestData, outputPath string, appendMode bool) error {
    out := cmd.OutOrStdout()
    if err := ValidateName(data.Name); err != nil {
        return ConfigError(err)
    }
    manifest := RenderDataSetManifest(data)
    absPath := outputPath
    if !filepath.IsAbs(outputPath) {
        absPath = filepath.Join(workdir, outputPath)
    }
    if appendMode {
        if err := AppendToManifest(absPath, manifest); err != nil { ... }
    } else {
        if err := WriteManifest(absPath, manifest); err != nil { ... }
    }
    return nil
}
```

**Fix:** Extract generic `WriteManifestFile[T Manifest](cmd, workdir, data T, outputPath string, appendMode bool) error`.

### 2. Prompt Flow Duplication

**Location:** All `add_*.go` files.

**Problem:** Each wizard repeats:
- Name prompting with validation
- Description prompting
- Constraints prompting
- Output location prompting
- Preview and confirmation

**Fix:** Create a `WizardBuilder` that composes common steps:
```go
wizard := prompts.NewWizard(reader, out).
    WithName("DataSet", manifests).
    WithDescription().
    WithConstraints().
    WithOutputLocation(workdir, manifests, "DataSet")

data, err := wizard.Run()
```

### 3. DataSource Type Enum

**Location:** `cli/add.go:353-364` vs implicit strings in `datasource/spec.go`.

**Problem:**
```go
// cli/add.go
type DataSourceType int
const (
    DataSourceTypeCSV DataSourceType = iota
    // ...
)

// datasource/spec.go — uses raw strings
switch s.Type {
case "csv":
case "parquet":
// ...
}
```

**Fix:** Define once in `schema/types.go`:
```go
type DataSourceType string

const (
    DataSourceTypeCSV      DataSourceType = "csv"
    DataSourceTypeParquet  DataSourceType = "parquet"
    DataSourceTypePostgres DataSourceType = "postgres_query"
    // ...
)

func (t DataSourceType) IsValid() bool { ... }
```

### 4. Constraint Parsing Duplication

**Location:** `spec/constraints.go` for parsing, but `cli/add_prompts.go` for building.

**Problem:** The constraint syntax (`labels.env==prod`, `mode in [build,preview]`) is defined implicitly in the parser. The add command builds constraints without validating them against the same rules.

**Fix:** `schema.ParseConstraint()` and `schema.FormatConstraint()` as inverse operations.

### Refactor Plan

| Step | Files Changed | Risk | Benefit |
|------|---------------|------|---------|
| 1. Create `internal/schema/` | New package | Low | Foundation for unification |
| 2. Move type definitions | `spec/*.go` → `schema/types.go` | Medium | Single source of truth |
| 3. Add `yaml` struct tags | `schema/types.go` | Low | Enable marshal/unmarshal |
| 4. Refactor `config.LoadDir()` | `config/loader.go` | Medium | Uses canonical types |
| 5. Refactor `cli/add_*.go` | 16 files | High | Delete ~4000 lines |
| 6. Add round-trip tests | New test files | Low | Prevent regression |
| 7. Delete dead code | `cli/add_*.go` | Low | Cleanup |

---

## Correctness, Error Handling, and UX

### Missing Validations

1. **Generated YAML not validated against JSON schema:**
   ```go
   // cli/add_dataset.go:687
   manifest := RenderDataSetManifest(data)
   // manifest is written directly — no validation!
   ```

2. **Constraint syntax not validated in add commands:**
   ```go
   // cli/add_prompts.go — builds constraint string but doesn't parse/validate
   constraint := fmt.Sprintf("%s %s %s", field, op, value)
   ```

3. **File path validation inconsistent:**
   - `--sql-file` accepts any path, doesn't check existence until build time.
   - Should validate file exists or warn.

### Inconsistent Error Handling

**Pattern 1: Wrapped errors (good)**
```go
return fmt.Errorf("scan manifests: %w", err)
```

**Pattern 2: Raw errors (bad)**
```go
return err // No context
```

**Pattern 3: Custom error types (inconsistent)**
```go
return ConfigError(err)    // cli/errors.go
return RuntimeError(err)   // cli/errors.go
// But spec package doesn't use these
```

### Proposed Error Strategy

```go
// internal/errors/errors.go
package errors

import "errors"

// Sentinel errors for common cases
var (
    ErrInvalidName       = errors.New("invalid manifest name")
    ErrDuplicateName     = errors.New("duplicate manifest name")
    ErrSchemaValidation  = errors.New("schema validation failed")
    ErrFileNotFound      = errors.New("file not found")
)

// Structured error with location
type ManifestError struct {
    File     string
    Position int
    Kind     string
    Name     string
    Err      error
}

func (e *ManifestError) Error() string {
    return fmt.Sprintf("%s:%d: %s %q: %v", e.File, e.Position, e.Kind, e.Name, e.Err)
}

func (e *ManifestError) Unwrap() error { return e.Err }
```

### CLI UX Issues

1. **Exit codes not documented:**
   - What does exit code 1 mean vs 2?
   - Recommendation: 1 = user error (bad config), 2 = runtime error, 130 = interrupted.

2. **`--no-prompt` validation errors are verbose:**
   ```
   missing required values in non-interactive mode:
     name (as argument)
     --sql, --sql-file, --prql, --prql-file, or --source
     --output or --append-to
   ```
   Should also show `bino add dataset --help` suggestion.

3. **No `--dry-run` flag:**
   - Users can't preview what will be written without the interactive wizard.

---

## Testing Strategy

### Current Coverage

| Package | Test Files | Notes |
|---------|------------|-------|
| `cli/` | 4 files | Only `add_test.go`, `init_test.go`, `lsp_test.go`, `serve_test.go` — no tests for `build.go` |
| `report/config/` | 4 files | Good coverage of loader, envexpand |
| `report/spec/` | 3 files | `constraints_test.go` is thorough (22KB) |
| `report/lint/` | 3 files | `layout_slots_test.go` is thorough (16KB) |
| `report/dataset/` | 2 files | `executor_test.go` is thorough (16KB) |
| `report/render/` | 3 files | Basic coverage |

### Critical Gaps

1. **No tests for `bino build` command end-to-end.**
2. **No tests for `bino add` command output correctness.**
3. **No round-trip tests (generate → parse → compare).**
4. **No golden file tests for YAML stability.**

### Recommended Test Plan

#### 1. Round-Trip Golden Tests

```go
// schema/roundtrip_test.go
func TestDataSetRoundTrip(t *testing.T) {
    original := &schema.Document{
        APIVersion: "bino.bi/v1alpha1",
        Kind:       "DataSet",
        Metadata:   schema.Metadata{Name: "test_dataset"},
        Spec: &schema.DataSetSpec{
            Query: &schema.QueryField{Inline: "SELECT 1"},
        },
    }

    // Marshal to YAML
    yamlBytes, err := yaml.Marshal(original)
    require.NoError(t, err)

    // Parse back
    parsed, err := schema.ParseDocument(yamlBytes)
    require.NoError(t, err)

    // Compare
    assert.Equal(t, original.Kind, parsed.Kind)
    assert.Equal(t, original.Metadata.Name, parsed.Metadata.Name)
    // Deep comparison of Spec
}
```

#### 2. Golden File Strategy

```
testdata/
├── golden/
│   ├── dataset_simple.yaml
│   ├── dataset_with_deps.yaml
│   ├── datasource_csv.yaml
│   ├── datasource_postgres.yaml
│   ├── reportartefact_pdf.yaml
│   └── ...
```

```go
func TestGoldenFiles(t *testing.T) {
    files, _ := filepath.Glob("testdata/golden/*.yaml")
    for _, f := range files {
        t.Run(filepath.Base(f), func(t *testing.T) {
            content, _ := os.ReadFile(f)

            // Parse
            doc, err := schema.ParseDocument(content)
            require.NoError(t, err)

            // Re-marshal
            remarshaled, err := yaml.Marshal(doc)
            require.NoError(t, err)

            // Should be semantically equivalent (ignoring whitespace)
            assert.YAMLEq(t, string(content), string(remarshaled))
        })
    }
}
```

#### 3. Add Command Output Tests

```go
func TestAddDataSetGeneratesValidYAML(t *testing.T) {
    data := cli.DataSetManifestData{
        Name:  "test_ds",
        Query: "SELECT 1",
    }

    output := cli.RenderDataSetManifest(data)

    // Validate against JSON schema
    err := schema.ValidateYAML([]byte(output))
    require.NoError(t, err)

    // Parse and verify fields
    doc, err := schema.ParseDocument([]byte(output))
    require.NoError(t, err)
    assert.Equal(t, "DataSet", doc.Kind)
    assert.Equal(t, "test_ds", doc.Metadata.Name)
}
```

#### 4. Schema Regression Tests

```go
func TestSchemaBackwardCompatibility(t *testing.T) {
    // Load all YAML files from testdata/v1alpha1/
    // Ensure they still parse correctly after schema changes
}
```

---

## Tooling & CI

### Recommended `golangci-lint` Configuration

Create `.golangci.yml`:

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
    - exhaustive      # Ensure switch statements are exhaustive
    - nilerr          # Catch returning nil after checking error
    - errorlint       # Proper error wrapping
    - forcetypeassert # Catch unsafe type assertions

linters-settings:
  exhaustive:
    default-signifies-exhaustive: true
  gocritic:
    enabled-tags:
      - diagnostic
      - style
      - performance

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
        - gocritic
```

### CI Commands

```yaml
# .github/workflows/ci.yml
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: golangci/golangci-lint-action@v3
        with:
          version: v1.55

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test -race -coverprofile=coverage.out ./...
      - run: go tool cover -func=coverage.out

  schema-validation:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test -run TestGoldenFiles ./internal/schema/...
      - run: go test -run TestRoundTrip ./internal/schema/...
```

---

## Findings (Actionable List)

### [F01] Schema Types Duplicated Between CLI and Spec (Severity: Blocker)

* **Location:** `internal/cli/add_dataset.go:138` (`DataSetManifestData`), `internal/cli/add_report.go:18` (`ReportArtefactManifestData`), `internal/cli/add_datasource.go` (`DataSourceManifestData`) vs `internal/report/spec/datasetref.go` (`InlineDataSet`, `InlineDataSource`)
* **Problem:** The CLI defines separate structs for generating YAML that are not the same as the structs used for parsing. Fields can drift between them.
* **Why it matters:** Adding a field to the schema requires changes in 3+ places. A field present in parsing but absent in generation (or vice versa) causes silent data loss or invalid output.
* **Evidence:**
  ```go
  // cli/add_dataset.go:138
  type DataSetManifestData struct {
      Name         string
      Description  string
      Constraints  []string
      Query        string   // Inline SQL
      QueryFile    string   // $file reference
      PRQL         string
      PRQLFile     string
      Source       string
      Dependencies []string
  }

  // spec/datasetref.go:28
  type InlineDataSet struct {
      Query        QueryField       `json:"query,omitempty"`
      Prql         QueryField       `json:"prql,omitempty"`
      Source       *DataSourceRef   `json:"source,omitempty"`
      Dependencies []DataSourceRef  `json:"dependencies,omitempty"`
  }
  ```
  Note the differences: `QueryFile` vs `QueryField.File`, `string` vs `DataSourceRef`.
* **Fix proposal:**
  1. Create `internal/schema/types.go` with canonical `DataSetSpec` struct.
  2. Add proper `yaml` struct tags.
  3. Refactor CLI to populate and marshal the canonical type.
  4. Delete `DataSetManifestData` and `RenderDataSetManifest()`.
* **Acceptance criteria:**
  - Only one Go struct exists per manifest kind.
  - `bino add` uses `yaml.Marshal()` on that struct.
  - Round-trip test passes.

---

### [F02] YAML Generated via String Concatenation (Severity: Blocker)

* **Location:** `internal/cli/add_report.go:841-887` (`RenderReportArtefactManifest`), `internal/cli/add_dataset.go` (`RenderDataSetManifest`), and all other `Render*Manifest` functions.
* **Problem:** YAML is built by appending strings with `fmt.Sprintf`. Field names, indentation, and quoting are hardcoded.
* **Why it matters:** Typos in field names compile successfully. Indentation errors cause parse failures. Special characters in values may not be quoted correctly.
* **Evidence:**
  ```go
  // cli/add_report.go:867-869
  if data.Format != "" {
      b.WriteString(fmt.Sprintf("  format: %s\n", data.Format))
  }
  ```
  If someone writes `"  fromat: %s\n"` it compiles fine but produces invalid YAML.
* **Fix proposal:**
  1. Define structs with `yaml` tags.
  2. Use `yaml.Marshal()` for all output.
  3. Delete all `Render*Manifest()` functions.
* **Acceptance criteria:**
  - No string-based YAML generation in CLI package.
  - All manifest output uses `yaml.Marshal()`.

---

### [F03] No Validation of Generated Manifests (Severity: Blocker)

* **Location:** `internal/cli/add_dataset.go:687-708`, all `write*Manifest` functions.
* **Problem:** Generated YAML is written to disk without validating against the JSON schema that `bino build` uses.
* **Why it matters:** Invalid manifests pass `bino add` but fail `bino build`, confusing users.
* **Evidence:**
  ```go
  // cli/add_dataset.go:687
  manifest := RenderDataSetManifest(data)
  // No call to spec.ValidateDocument([]byte(manifest))
  ```
* **Fix proposal:**
  1. After marshaling, call `schema.Validate(yamlBytes)`.
  2. Fail fast with actionable error if invalid.
* **Acceptance criteria:**
  - `bino add` validates output before writing.
  - Invalid manifests are rejected with schema error messages.

---

### [F04] DataSource Type Defined Twice (Severity: Major)

* **Location:** `internal/cli/add.go:353-364` (`DataSourceType` enum), `internal/report/datasource/spec.go` (string switch cases).
* **Problem:** Allowed datasource types are defined as an `int` enum in CLI and as implicit strings in datasource package.
* **Why it matters:** Adding a new datasource type requires changes in both places. No compile-time check that they match.
* **Evidence:**
  ```go
  // cli/add.go:353
  type DataSourceType int
  const (
      DataSourceTypeCSV DataSourceType = iota
      // ...
  )

  // datasource/spec.go (implicit)
  switch s.Type {
  case "csv":
  case "parquet":
  ```
* **Fix proposal:**
  1. Define `type DataSourceType string` in `schema/types.go`.
  2. Export constants: `const DataSourceTypeCSV DataSourceType = "csv"`.
  3. Use this type in both CLI and datasource packages.
* **Acceptance criteria:**
  - Single definition of allowed DataSource types.
  - Adding a new type requires change in one file.

---

### [F05] Write Function Boilerplate Repeated 16 Times (Severity: Major)

* **Location:** `internal/cli/add_dataset.go:677-709`, `add_report.go:751-778`, `add_datasource.go:689-720`, etc.
* **Problem:** Each add command has a nearly identical `write*Manifest()` function with the same error handling, path resolution, and file writing logic.
* **Why it matters:** Bug fixes or improvements must be applied to 16 files. Easy to miss one.
* **Evidence:**
  ```go
  // Identical pattern in every file:
  if err := ValidateName(data.Name); err != nil { return ConfigError(err) }
  manifest := Render*Manifest(data)
  absPath := outputPath
  if !filepath.IsAbs(outputPath) { absPath = filepath.Join(workdir, outputPath) }
  if appendMode { AppendToManifest(...) } else { WriteManifest(...) }
  ```
* **Fix proposal:**
  1. Create generic `WriteManifest[T any](cmd, workdir string, data T, marshal func(T) ([]byte, error), path string, append bool) error`.
  2. Each command calls this with its specific marshal function.
* **Acceptance criteria:**
  - Single implementation of manifest file writing.
  - Commands pass data and get file I/O for free.

---

### [F06] Prompt Flow Duplication (Severity: Major)

* **Location:** All `add_*.go` files — each implements name prompt, description prompt, constraints prompt, output location prompt.
* **Problem:** ~500 lines of prompt logic repeated per command.
* **Why it matters:** UX changes (e.g., adding "undo" support) require 16 parallel edits.
* **Evidence:**
  ```go
  // add_dataset.go:184-203 — name prompting
  // add_report.go:164-173 — same pattern
  // add_datasource.go — same pattern
  ```
* **Fix proposal:**
  1. Create `internal/cli/prompts/wizard.go` with composable prompt steps.
  2. Commands configure wizard with kind-specific options.
* **Acceptance criteria:**
  - Common prompts (name, description, constraints, output) defined once.
  - Kind-specific prompts composed on top.

---

### [F07] No Round-Trip Tests (Severity: Major)

* **Location:** Test files in `internal/cli/`, `internal/report/config/`.
* **Problem:** No test generates YAML via the add commands and parses it back to verify correctness.
* **Why it matters:** Schema drift between generator and parser is undetectable until users report bugs.
* **Evidence:** Searched all `*_test.go` files — no test calls `RenderDataSetManifest()` and then `config.LoadDir()` or `spec.ValidateDocument()`.
* **Fix proposal:**
  1. Add `internal/schema/roundtrip_test.go`.
  2. For each manifest kind, test: create struct → marshal → parse → compare.
* **Acceptance criteria:**
  - Round-trip test exists for all 15 manifest kinds.
  - Tests run in CI.

---

### [F08] Constraint Syntax Not Validated on Generation (Severity: Major)

* **Location:** `internal/cli/add_prompts.go` (constraint builder), `internal/report/spec/constraints.go` (parser).
* **Problem:** The add command builds constraint strings without parsing them to verify syntax. `spec.ParseConstraint()` is only called during build.
* **Why it matters:** User can create invalid constraints like `labels.foo = bar` (spaces around `=`) that fail at build time.
* **Evidence:**
  ```go
  // add_prompts.go — builds string, no validation
  constraint := fmt.Sprintf("%s %s %s", field, op, value)
  ```
* **Fix proposal:**
  1. After building constraint string, call `spec.ParseConstraint(constraint)`.
  2. If parse fails, show error and re-prompt.
* **Acceptance criteria:**
  - Invalid constraints rejected by add command.
  - Error message shows expected syntax.

---

### [F09] `render/spec.go` Defines Additional Schema Types (Severity: Major)

* **Location:** `internal/report/render/spec.go`.
* **Problem:** This file defines `ChartStructureSpec`, `TableSpec`, `TextSpec`, etc. for HTML rendering. These are additional schema representations beyond `cli/` and `spec/`.
* **Why it matters:** Third place where schema is defined. Changes to chart fields need updates here too.
* **Evidence:**
  ```go
  // render/spec.go (hypothetical based on project structure)
  type ChartStructureSpec struct {
      Dataset    string
      Dimensions []string
      Measures   []MeasureSpec
      // ...
  }
  ```
* **Fix proposal:**
  1. Unify with `schema.ChartStructureSpec`.
  2. Render package should import schema types, not define its own.
* **Acceptance criteria:**
  - `render` package imports from `schema`, no local type definitions.

---

### [F10] Error Wrapping Inconsistent (Severity: Minor)

* **Location:** Throughout codebase.
* **Problem:** Some errors are wrapped with context (`fmt.Errorf("...: %w", err)`), others return raw `err`.
* **Why it matters:** Debugging is harder when errors lack context about where they occurred.
* **Evidence:**
  ```go
  // Good (config/inline.go:32)
  return nil, fmt.Errorf("%s %q: %w", doc.Kind, doc.Name, err)

  // Bad (cli/add_dataset.go:81)
  return ConfigError(err) // No context about what failed
  ```
* **Fix proposal:**
  1. Establish convention: always wrap with operation context.
  2. Add `errorlint` to golangci-lint to enforce.
* **Acceptance criteria:**
  - All error returns include operation context.
  - Linter enforces error wrapping.

---

### [F11] No `.golangci.yml` Configuration (Severity: Minor)

* **Location:** Repository root.
* **Problem:** No linter configuration committed. CI may not run linting, or uses defaults.
* **Why it matters:** Code style and error handling issues slip through.
* **Fix proposal:**
  1. Add `.golangci.yml` with recommended linters (see Tooling section).
  2. Add lint job to CI workflow.
* **Acceptance criteria:**
  - `.golangci.yml` exists with sensible defaults.
  - CI fails on lint errors.

---

### [F12] `quoteYAMLIfNeeded` Is Fragile (Severity: Minor)

* **Location:** `internal/cli/add_prompts.go` (or similar utility file).
* **Problem:** Custom function to decide when to quote YAML strings. Easy to get wrong for edge cases (colons, special chars, multiline).
* **Why it matters:** Strings like `"yes"`, `"no"`, `"true"` need quoting; so do strings with `:` or `#`.
* **Evidence:**
  ```go
  // Manual quoting logic
  func quoteYAMLIfNeeded(s string) string { ... }
  ```
* **Fix proposal:**
  1. Use `yaml.Marshal()` which handles quoting automatically.
  2. Delete `quoteYAMLIfNeeded()`.
* **Acceptance criteria:**
  - No manual YAML quoting.
  - `yaml.Marshal()` handles all cases.

---

## Special Focus: Schema Unification Guidance

### Where Canonical Schema Types Should Live

**Recommendation:** `internal/schema/`

```
internal/schema/
├── types.go           # All manifest kind structs
├── document.go        # Document envelope, parsing, marshaling
├── validate.go        # JSON schema + semantic validation
├── migrate.go         # Future: version migrations
└── schema_test.go     # Round-trip tests
```

### How to Avoid "Builder Structs" vs "Parser Structs"

1. **One struct per kind** with both `yaml` and `json` tags:
   ```go
   type DataSetSpec struct {
       Query        *QueryField     `yaml:"query,omitempty" json:"query,omitempty"`
       PRQL         *QueryField     `yaml:"prql,omitempty" json:"prql,omitempty"`
       Source       *DataSourceRef  `yaml:"source,omitempty" json:"source,omitempty"`
       Dependencies []DataSourceRef `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
   }
   ```

2. **Custom marshal/unmarshal for complex fields** (e.g., `QueryField` that can be scalar or `$file: path`):
   ```go
   func (q *QueryField) UnmarshalYAML(node *yaml.Node) error {
       if node.Kind == yaml.ScalarNode {
           q.Inline = node.Value
           return nil
       }
       // Handle $file syntax
       var m map[string]string
       if err := node.Decode(&m); err != nil {
           return err
       }
       if f, ok := m["$file"]; ok {
           q.File = f
       }
       return nil
   }
   ```

3. **CLI populates struct, then marshals:**
   ```go
   spec := &schema.DataSetSpec{
       Query: &schema.QueryField{Inline: userInput},
   }
   doc := &schema.Document{
       APIVersion: "bino.bi/v1alpha1",
       Kind:       "DataSet",
       Metadata:   schema.Metadata{Name: name},
   }
   doc.SetSpec(spec)

   yamlBytes, _ := yaml.Marshal(doc)
   ```

### How to Enforce Round-Trip Stability

1. **Golden file tests:** Check that parsing and re-marshaling a file produces semantically equivalent output.

2. **Property-based testing:** Generate random valid specs, marshal, parse, compare.

3. **CI gate:** Round-trip tests must pass before merge.

### How to Handle Schema Versions

Current state: `apiVersion: bino.bi/v1alpha1` exists but is not enforced.

**Recommendation for future evolution:**

1. **Detect version on parse:**
   ```go
   func ParseDocument(data []byte) (*Document, error) {
       var envelope struct {
           APIVersion string `yaml:"apiVersion"`
       }
       yaml.Unmarshal(data, &envelope)

       switch envelope.APIVersion {
       case "bino.bi/v1alpha1":
           return parseV1Alpha1(data)
       case "bino.bi/v1":
           return parseV1(data)
       default:
           return nil, fmt.Errorf("unknown apiVersion: %s", envelope.APIVersion)
       }
   }
   ```

2. **Migration functions:**
   ```go
   func MigrateV1Alpha1ToV1(doc *DocumentV1Alpha1) *DocumentV1 { ... }
   ```

3. **Write always uses latest version** (or version specified by user).

---

## Summary of Recommended Actions

| Priority | Action | Effort | Impact |
|----------|--------|--------|--------|
| P0 | Create `internal/schema/` with canonical types | 2-3 days | Eliminates schema drift |
| P0 | Refactor CLI to marshal structs instead of string building | 2-3 days | Eliminates YAML errors |
| P0 | Add validation of generated manifests | 1 day | Catches errors early |
| P1 | Add round-trip tests | 1 day | Prevents regressions |
| P1 | Consolidate write functions | 1 day | Reduces maintenance |
| P1 | Unify DataSourceType | 0.5 days | Single source of truth |
| P2 | Add golangci-lint config | 0.5 days | Code quality |
| P2 | Consolidate prompt flows | 2 days | UX consistency |
| P3 | Improve error handling | 1 day | Better debugging |

Total estimated effort: ~2 weeks for P0+P1, ~1 additional week for P2+P3.
