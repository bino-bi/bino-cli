// Package dataset provides execution and caching for DataSet manifests.
//
// A DataSet executes a SQL query against DuckDB, referencing DataSource
// manifests which are registered as views. Results are cached under .bino/cache/datasets/
// in the working directory and invalidated when the dataset definition changes
// or when any dependent datasource files are modified.
//
// DataSources are materialized as DuckDB views via datasource.RegisterViews,
// so DataSet queries can simply SELECT FROM <datasource_name>.
package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/filehash"
	reportspec "bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/runtimecfg"
	"bino.bi/bino/pkg/duckdb"
)

// Result captures the evaluated rows for a dataset manifest.
type Result struct {
	Name string
	Data json.RawMessage
}

// Warning represents a non-fatal issue encountered during dataset execution.
type Warning struct {
	DataSet string
	Message string
}

func (w Warning) String() string {
	return fmt.Sprintf("dataset %s: %s", w.DataSet, w.Message)
}

// ExecuteOptions configures dataset execution behavior.
type ExecuteOptions struct {
	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger duckdb.QueryLogger
	// QueryExecLogger is called for each query execution with detailed metadata. May be nil.
	QueryExecLogger duckdb.QueryExecLogger
	// EmbedOptions configures CSV embedding for build logs.
	EmbedOptions buildlog.EmbedOptions
	// DataValidation controls how data validation errors are handled.
	// Default is DataValidationOff for backwards compatibility.
	DataValidation DataValidationMode
	// DataValidationSampleSize limits how many rows are validated.
	// Default (0) uses GetDataValidationSampleSize() which reads from env.
	DataValidationSampleSize int

	// ContinueOnQueryError downgrades dataset query execution failures to
	// warnings and continues with the remaining datasets. Default (false)
	// fails execution on the first query error so a broken query can never
	// produce a green build. Dev surfaces (preview, lint, lsp, daemon) set
	// this so one broken query doesn't abort their diagnostics loop.
	ContinueOnQueryError bool

	// Session is an optional pre-existing DuckDB session to reuse.
	// When set, dataset execution skips opening a new session and reuses this one.
	// The caller is responsible for closing the session.
	// Extension loading and view registration are idempotent on a reused session.
	Session *duckdb.Session
}

// shiftMacroRevision is the bino_shift revision hashed into the cache digest
// of declaring datasets; a variable so tests can bump it.
var shiftMacroRevision = duckdb.ShiftMacroRevision

// dataSetSpec mirrors the new minimal DataSet spec structure.
type dataSetSpec struct {
	Query        reportspec.QueryField `json:"query"`
	Prql         reportspec.QueryField `json:"prql"`
	Source       string                `json:"source"` // Direct DataSource pass-through (mutually exclusive with query/prql)
	Dependencies []string              `json:"dependencies"`

	Derive map[string]reportspec.ShiftDeclaration `json:"derive"`
	Assert map[string]reportspec.ShiftDeclaration `json:"assert"`
}

// declares reports whether the dataset declares any derived or asserted slot.
func (s dataSetSpec) declares() bool {
	return len(s.Derive) > 0 || len(s.Assert) > 0
}

// Execute evaluates all DataSet documents, using cached results when available.
// Results are cached under workdir/.bino/cache/datasets/ and invalidated when the
// dataset definition (query or dependencies) changes, or when any dependent
// datasource files are modified.
//
// DataSources are registered as DuckDB views via datasource.RegisterViews,
// so DataSet queries can simply `SELECT * FROM <datasource_name>`.
// The dependencies field is used for validation and caching, not for SQL wiring.
//
// Ephemeral datasources (databases, URLs, files outside workdir) always skip
// the cache and are refetched on every build. This ensures data freshness for
// sources that may change without manifest modifications.
//
// The opts parameter allows configuring execution behavior, such as SQL query logging.
// Pass nil for default behavior (no logging).
func Execute(ctx context.Context, workdir string, docs []config.Document, opts *ExecuteOptions) ([]Result, []Warning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	cacheDir := filepath.Join(workdir, ".bino", "cache", "datasets")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create cache dir: %w", err)
	}

	// Build index of DataSource documents by name for dependency lookup
	dataSourceIndex := make(map[string]config.Document)
	for _, doc := range docs {
		if doc.Kind == "DataSource" {
			dataSourceIndex[doc.Name] = doc
		}
	}

	// Collect DataSet documents
	var dataSetDocs []config.Document
	for _, doc := range docs {
		if doc.Kind == "DataSet" {
			dataSetDocs = append(dataSetDocs, doc)
		}
	}

	if len(dataSetDocs) == 0 {
		return nil, nil, nil
	}

	// cacheCheckResult holds the result of checking cache for a single dataset
	type cacheCheckResult struct {
		doc       config.Document
		spec      dataSetSpec
		cached    bool
		data      json.RawMessage
		cachePath string
		warnings  []Warning
	}

	// Check cache for each dataset in parallel
	resultCh := make(chan cacheCheckResult, len(dataSetDocs))
	var wg sync.WaitGroup

	for _, doc := range dataSetDocs {
		wg.Go(func() {
			result := cacheCheckResult{doc: doc}

			spec, err := parseDataSetSpec(doc.Raw)
			if err != nil {
				result.warnings = append(result.warnings, Warning{DataSet: doc.Name, Message: fmt.Sprintf("parse spec: %v", err)})
				resultCh <- result
				return
			}
			result.spec = spec

			// Check if any dependency is ephemeral - if so, skip cache entirely
			hasEphemeralDep := false
			for _, depName := range spec.Dependencies {
				depDoc, ok := dataSourceIndex[depName]
				if !ok {
					continue
				}
				if filehash.IsEphemeralSource(depDoc, workdir) {
					hasEphemeralDep = true
					break
				}
			}

			if hasEphemeralDep {
				// Skip cache for datasets with ephemeral dependencies
				result.cachePath = "" // No caching for ephemeral sources
				resultCh <- result
				return
			}

			// Compute cache key including datasource file hashes
			digest, depWarnings := computeDigestWithDeps(doc, spec, dataSourceIndex)
			result.warnings = append(result.warnings, depWarnings...)

			cachePath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s.json", doc.Name, digest[:16]))
			result.cachePath = cachePath

			// Try reading from cache
			if data, err := os.ReadFile(cachePath); err == nil {
				result.cached = true
				result.data = data
			}

			resultCh <- result
		})
	}

	// Wait for all cache checks to complete and close the channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	var (
		results  []Result
		warnings []Warning
		toRun    []dataSetJob
	)

	// Determine validation sample size for cached results
	validationSampleSize := 0
	if opts != nil && opts.DataValidation != DataValidationOff && opts.DataValidation != "" {
		if opts.DataValidationSampleSize > 0 {
			validationSampleSize = opts.DataValidationSampleSize
		} else {
			validationSampleSize = GetDataValidationSampleSize()
		}
	}

	for result := range resultCh {
		warnings = append(warnings, result.warnings...)

		if result.spec.Query.IsEmpty() && result.spec.Prql.IsEmpty() && result.spec.Source == "" {
			// No spec parsed (error case) - must have query, prql, or source
			continue
		}

		if result.cached {
			// Validate cached data if validation is enabled
			if validationSampleSize > 0 {
				validationResult := ValidateRows(result.doc.Name, result.data, validationSampleSize)
				if !validationResult.Valid {
					validationWarnings := DataValidationResultToWarnings(validationResult)
					warnings = append(warnings, validationWarnings...)

					// In fail mode, return error immediately
					if opts != nil && opts.DataValidation == DataValidationFail {
						return nil, warnings, fmt.Errorf("data validation failed for %s: %d error(s)", result.doc.Name, len(validationResult.Errors))
					}
				}
			}
			warnings = append(warnings, allNullDerivedSlots(result.doc.Name, result.data, result.spec.Derive)...)
			results = append(results, Result{Name: result.doc.Name, Data: result.data})
			continue
		}

		toRun = append(toRun, dataSetJob{
			doc:       result.doc,
			spec:      result.spec,
			cachePath: result.cachePath,
		})
	}

	if len(toRun) == 0 {
		return results, warnings, nil
	}

	// Execute datasets that weren't cached
	execResults, execWarnings, err := executeDataSets(ctx, workdir, toRun, docs, opts)
	if err != nil {
		return results, append(warnings, execWarnings...), err
	}

	results = append(results, execResults...)
	warnings = append(warnings, execWarnings...)
	return results, warnings, nil
}

type dataSetJob struct {
	doc       config.Document
	spec      dataSetSpec
	cachePath string
}

func parseDataSetSpec(raw json.RawMessage) (dataSetSpec, error) {
	var payload struct {
		Spec dataSetSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return dataSetSpec{}, err
	}
	return payload.Spec, nil
}

// computeDigest computes a simple SHA256 hash of the given data.
// This is used for basic digest computation without dependency tracking.
func computeDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// computeDigestWithDeps computes a cache key that includes both the dataset
// definition and the content hashes of all dependent datasource files.
// This ensures the cache is invalidated when source data files change.
// It also includes hashes of external query files referenced via $file.
func computeDigestWithDeps(doc config.Document, spec dataSetSpec, dataSourceIndex map[string]config.Document) (string, []Warning) {
	var warnings []Warning

	h := sha256.New()
	// Include dataset definition in hash
	h.Write(doc.Raw)

	// A derived/asserted result depends on the bino_shift macro; a macro fix
	// must invalidate it and leave every other dataset's key unchanged.
	if spec.declares() {
		fmt.Fprintf(h, "%s:%d", duckdb.ShiftMacroName, shiftMacroRevision)
	}

	// Include external query file hashes if using $file reference
	baseDir := filepath.Dir(doc.File)
	if spec.Query.HasFile() {
		queryFilePath := spec.Query.File
		if !filepath.IsAbs(queryFilePath) {
			queryFilePath = filepath.Join(baseDir, queryFilePath)
		}
		fileHash, err := filehash.HashFile(queryFilePath)
		if err != nil {
			warnings = append(warnings, Warning{
				DataSet: doc.Name,
				Message: fmt.Sprintf("hash query file %s: %v", spec.Query.File, err),
			})
		} else {
			h.Write([]byte(fileHash))
		}
	}
	if spec.Prql.HasFile() {
		prqlFilePath := spec.Prql.File
		if !filepath.IsAbs(prqlFilePath) {
			prqlFilePath = filepath.Join(baseDir, prqlFilePath)
		}
		fileHash, err := filehash.HashFile(prqlFilePath)
		if err != nil {
			warnings = append(warnings, Warning{
				DataSet: doc.Name,
				Message: fmt.Sprintf("hash prql file %s: %v", spec.Prql.File, err),
			})
		} else {
			h.Write([]byte(fileHash))
		}
	}

	// Collect and hash dependent datasource files
	var depHashes []string
	for _, depName := range spec.Dependencies {
		depDoc, ok := dataSourceIndex[depName]
		if !ok {
			warnings = append(warnings, Warning{
				DataSet: doc.Name,
				Message: fmt.Sprintf("missing dependency: %s", depName),
			})
			continue
		}

		// Hash the datasource's files
		fileHash, err := filehash.HashDataSourceFiles(depDoc)
		if err != nil {
			// Log warning but continue - datasource may be inline or database type
			warnings = append(warnings, Warning{
				DataSet: doc.Name,
				Message: fmt.Sprintf("hash dependency %s: %v", depName, err),
			})
			continue
		}
		if fileHash != "" {
			depHashes = append(depHashes, fileHash)
		}
	}

	// Sort for deterministic ordering
	sort.Strings(depHashes)
	for _, dh := range depHashes {
		h.Write([]byte(dh))
	}

	return hex.EncodeToString(h.Sum(nil)), warnings
}

func executeDataSets(ctx context.Context, workdir string, jobs []dataSetJob, allDocs []config.Document, opts *ExecuteOptions) ([]Result, []Warning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Reuse a caller-provided session or create a fresh one.
	var session *duckdb.Session
	if opts != nil && opts.Session != nil {
		session = opts.Session
	} else {
		duckdbOpts, err := duckdb.DefaultOptions()
		if err != nil {
			return nil, nil, fmt.Errorf("duckdb options: %w", err)
		}
		if opts != nil && opts.QueryLogger != nil {
			duckdbOpts.QueryLogger = opts.QueryLogger
		}
		if opts != nil && opts.QueryExecLogger != nil {
			duckdbOpts.QueryExecLogger = opts.QueryExecLogger
		}
		s, err := duckdb.OpenSession(ctx, duckdbOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("duckdb open: %w", err)
		}
		defer s.Close() //nolint:errcheck // teardown of an ephemeral in-memory session
		session = s
	}

	var (
		results  []Result
		warnings []Warning
	)

	// Create temp directory for inline datasource CSV files.
	// When reusing a shared session the temp dir must persist (the views reference it),
	// so we only remove it for one-shot sessions.
	tempDir := filepath.Join(workdir, ".bino", "cache", "datasources")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create datasources temp dir: %w", err)
	}
	if opts == nil || opts.Session == nil {
		defer os.RemoveAll(tempDir)
	}

	// Names starting with the view prefix would collide with the views a
	// derive/assert dataset is built on.
	for _, doc := range allDocs {
		if (doc.Kind == "DataSource" || doc.Kind == "DataSet") && strings.HasPrefix(doc.Name, ViewPrefix) {
			return nil, nil, fmt.Errorf("%s %q: names starting with %q are reserved", doc.Kind, doc.Name, ViewPrefix)
		}
	}

	// Register all DataSources as views first
	viewDiags, err := datasource.RegisterViews(ctx, session, allDocs, &datasource.ViewsOptions{
		TempDir: tempDir,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("register views: %w", err)
	}

	// Convert view diagnostics to warnings
	for _, diag := range viewDiags {
		warnings = append(warnings, Warning{
			DataSet: diag.Datasource,
			Message: fmt.Sprintf("datasource: %v", diag.Err),
		})
	}

	// Load PRQL extension if any dataset uses PRQL queries
	hasPrql := false
	for _, job := range jobs {
		if !job.spec.Prql.IsEmpty() {
			hasPrql = true
			break
		}
	}
	if hasPrql {
		if err := session.InstallAndLoadCommunityExtensions(ctx, []string{"prql"}); err != nil {
			return nil, nil, fmt.Errorf("load prql extension: %w", err)
		}
	}

	// Determine validation sample size
	validationSampleSize := 0
	if opts != nil && opts.DataValidation != DataValidationOff && opts.DataValidation != "" {
		if opts.DataValidationSampleSize > 0 {
			validationSampleSize = opts.DataValidationSampleSize
		} else {
			validationSampleSize = GetDataValidationSampleSize()
		}
	}

	// Execute each dataset query directly (views are already available)
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return results, warnings, err
		}

		data, err := executeDataSet(ctx, session, job, opts)
		if err != nil {
			if opts != nil && opts.ContinueOnQueryError {
				warnings = append(warnings, Warning{DataSet: job.doc.Name, Message: fmt.Sprintf("execute: %v", err)})
				continue
			}
			return results, warnings, fmt.Errorf("dataset %s: %w", job.doc.Name, err)
		}

		// Validate data if enabled
		if validationSampleSize > 0 {
			validationResult := ValidateRows(job.doc.Name, data, validationSampleSize)
			if !validationResult.Valid {
				validationWarnings := DataValidationResultToWarnings(validationResult)
				warnings = append(warnings, validationWarnings...)

				// In fail mode, return error immediately
				if opts != nil && opts.DataValidation == DataValidationFail {
					return results, warnings, fmt.Errorf("data validation failed for %s: %d error(s)", job.doc.Name, len(validationResult.Errors))
				}
			}
		}

		warnings = append(warnings, allNullDerivedSlots(job.doc.Name, data, job.spec.Derive)...)

		// Write to cache (skip for ephemeral sources where cachePath is empty)
		if job.cachePath != "" {
			if err := os.WriteFile(job.cachePath, data, 0o644); err != nil { //nolint:gosec // G306: cache files need standard read perms
				warnings = append(warnings, Warning{DataSet: job.doc.Name, Message: fmt.Sprintf("cache write: %v", err)})
			}
		}

		results = append(results, Result{Name: job.doc.Name, Data: data})
	}

	return results, warnings, nil
}

func executeDataSet(ctx context.Context, session *duckdb.Session, job dataSetJob, _ *ExecuteOptions) (json.RawMessage, error) {
	db := session.DB()

	// Resolve the dataset (source / prql / query, $file, @inline) into the
	// statements to run. DataSources are already registered as views.
	compiled, err := compileSpec(job.doc, job.spec)
	if err != nil {
		return nil, err
	}

	// Enforce the per-query duration limit (BNR_MAX_QUERY_DURATION_MS, 0 = unlimited).
	cfg := runtimecfg.Current()
	queryCtx := ctx
	if cfg.MaxQueryDuration > 0 {
		var cancel context.CancelFunc
		queryCtx, cancel = context.WithTimeout(ctx, cfg.MaxQueryDuration)
		defer cancel()
	}

	// Setup: the view(s) a PRQL or derive/assert dataset is built on.
	for _, stmt := range compiled.Setup {
		session.LogQuery(stmt)
		setupStart := time.Now()
		if _, err := db.ExecContext(queryCtx, stmt); err != nil {
			err = describeQueryLimitError(err, cfg.MaxQueryDuration)
			logQueryExecError(session, stmt, job.doc.Name, setupStart, err)
			return nil, fmt.Errorf("setup: %w", err)
		}
		session.LogQueryExec(duckdb.QueryExecMeta{
			Query:      stmt,
			QueryType:  "dataset_setup",
			Dataset:    job.doc.Name,
			StartTime:  setupStart,
			DurationMs: time.Since(setupStart).Milliseconds(),
		})
	}

	// Declared expectations are checked on every row before the query runs.
	if compiled.Declares() {
		if err := runDeclarationChecks(queryCtx, db, compiled); err != nil {
			return nil, fmt.Errorf("check: %w", describeQueryLimitError(err, cfg.MaxQueryDuration))
		}
	}

	query := compiled.Query

	// Log the query before execution
	session.LogQuery(query)

	// Record timing for query execution metadata
	startTime := time.Now()

	// Execute query
	rows, err := db.QueryContext(queryCtx, query)
	if err != nil {
		err = describeQueryLimitError(err, cfg.MaxQueryDuration)
		// Log failed query execution if logger is available
		logQueryExecError(session, query, job.doc.Name, startTime, err)
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	// Serialize to JSON array and capture rows for metadata.
	// Enforces the row limit (BNR_MAX_QUERY_ROWS, 0 = unlimited).
	data, columns, rowStrings, err := rowsToJSONWithMeta(rows, cfg.MaxQueryRows)
	if err != nil {
		err = describeQueryLimitError(err, cfg.MaxQueryDuration)
		logQueryExecError(session, query, job.doc.Name, startTime, err)
		return nil, fmt.Errorf("serialize: %w", err)
	}

	// Calculate duration and emit query execution metadata
	durationMs := time.Since(startTime).Milliseconds()

	// Emit structured query execution metadata if logger is available
	session.LogQueryExec(duckdb.QueryExecMeta{
		Query:      query,
		QueryType:  "dataset_query",
		Dataset:    job.doc.Name,
		StartTime:  startTime,
		DurationMs: durationMs,
		RowCount:   len(rowStrings),
		Columns:    columns,
		Rows:       rowStrings,
	})

	return data, nil
}

// logQueryExecError logs a failed query execution if the logger is available.
func logQueryExecError(session *duckdb.Session, query, datasetName string, startTime time.Time, err error) {
	if session == nil {
		return
	}
	session.LogQueryExec(duckdb.QueryExecMeta{
		Query:      query,
		QueryType:  "dataset_query",
		Dataset:    datasetName,
		StartTime:  startTime,
		DurationMs: time.Since(startTime).Milliseconds(),
		Error:      err.Error(),
	})
}

// describeQueryLimitError attributes a deadline-exceeded error to the
// configured query duration limit so users learn which knob to turn.
func describeQueryLimitError(err error, maxDuration time.Duration) error {
	if maxDuration > 0 && errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("exceeded max query duration %s (BNR_MAX_QUERY_DURATION_MS): %w", maxDuration, err)
	}
	return err
}

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Columns() ([]string, error)
	Err() error
}

// rowsToJSONWithMeta serializes rows to JSON and also returns column names and rows as strings
// for CSV embedding in build logs. maxRows bounds the result size (0 = unlimited);
// exceeding it is an error so a capped result can never silently ship as complete data.
func rowsToJSONWithMeta(rows rowScanner, maxRows int) (data json.RawMessage, columns []string, rowStrings [][]string, err error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, nil, err
	}

	var results []map[string]any
	values := make([]any, len(cols))
	valuePtrs := make([]any, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if maxRows > 0 && len(results) >= maxRows {
			return nil, nil, nil, fmt.Errorf("query returned more than %d rows (BNR_MAX_QUERY_ROWS)", maxRows)
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, nil, err
		}
		row := make(map[string]any, len(cols))
		rowStr := make([]string, len(cols))
		for i, col := range cols {
			row[col] = values[i]
			rowStr[i] = valueToString(values[i])
		}
		results = append(results, row)
		rowStrings = append(rowStrings, rowStr)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	if results == nil {
		results = []map[string]any{}
	}

	data, err = json.Marshal(results)
	return data, cols, rowStrings, err
}

// valueToString converts a value to its string representation for CSV building.
func valueToString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
