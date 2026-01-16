// Package dataset provides execution and caching for DataSet manifests.
//
// A DataSet executes a SQL query against DuckDB, referencing DataSource
// manifests which are registered as views. Results are cached under .bncache/datasets/
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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/filehash"
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
}

// dataSetSpec mirrors the new minimal DataSet spec structure.
type dataSetSpec struct {
	Query        queryField `json:"query"`
	Prql         queryField `json:"prql"`
	Dependencies []string   `json:"dependencies"`
}

// queryField represents a query that can be either an inline string or a file reference.
// It supports both formats:
//   - Inline: "SELECT * FROM table"
//   - File reference: { "$file": "./queries/sales.sql" }
type queryField struct {
	Inline string // Inline query string
	File   string // Path to external file (from $file)
}

// UnmarshalJSON implements custom unmarshaling for queryField.
// It handles both string values and object values with $file key.
func (q *queryField) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		q.Inline = str
		return nil
	}

	// Try to unmarshal as an object with $file
	var obj struct {
		File string `json:"$file"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		q.File = obj.File
		return nil
	}

	return fmt.Errorf("query must be a string or an object with $file key")
}

// IsEmpty returns true if the query field has no value.
func (q queryField) IsEmpty() bool {
	return q.Inline == "" && q.File == ""
}

// HasFile returns true if the query references an external file.
func (q queryField) HasFile() bool {
	return q.File != ""
}

// ResolveQuery resolves the query content, loading from file if necessary.
// The baseDir parameter is used to resolve relative file paths.
func (q queryField) ResolveQuery(baseDir string) (string, error) {
	if q.Inline != "" {
		return q.Inline, nil
	}
	if q.File == "" {
		return "", nil
	}

	// Resolve the file path relative to the manifest
	filePath := q.File
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(baseDir, filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read query file %s: %w", q.File, err)
	}

	return string(content), nil
}

// Execute evaluates all DataSet documents, using cached results when available.
// Results are cached under workdir/.bncache/datasets/ and invalidated when the
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

	cacheDir := filepath.Join(workdir, ".bncache", "datasets")
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

	for result := range resultCh {
		warnings = append(warnings, result.warnings...)

		if result.spec.Query.IsEmpty() && result.spec.Prql.IsEmpty() {
			// No spec parsed (error case)
			continue
		}

		if result.cached {
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

	duckdbOpts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, nil, fmt.Errorf("duckdb options: %w", err)
	}

	// Wire up query logger if provided
	if opts != nil && opts.QueryLogger != nil {
		duckdbOpts.QueryLogger = opts.QueryLogger
	}
	if opts != nil && opts.QueryExecLogger != nil {
		duckdbOpts.QueryExecLogger = opts.QueryExecLogger
	}

	session, err := duckdb.OpenSession(ctx, duckdbOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close()

	var (
		results  []Result
		warnings []Warning
	)

	// Create temp directory for inline datasource CSV files
	tempDir := filepath.Join(workdir, ".bncache", "datasources")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create datasources temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

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
		if err := session.InstallAndLoadCommunityExtensions(ctx, duckdb.CommunityExtensions()); err != nil {
			return nil, nil, fmt.Errorf("load prql extension: %w", err)
		}
	}

	// Execute each dataset query directly (views are already available)
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return results, warnings, err
		}

		data, err := executeDataSet(ctx, session, job, opts)
		if err != nil {
			warnings = append(warnings, Warning{DataSet: job.doc.Name, Message: fmt.Sprintf("execute: %v", err)})
			continue
		}

		// Write to cache (skip for ephemeral sources where cachePath is empty)
		if job.cachePath != "" {
			if err := os.WriteFile(job.cachePath, data, 0o644); err != nil {
				warnings = append(warnings, Warning{DataSet: job.doc.Name, Message: fmt.Sprintf("cache write: %v", err)})
			}
		}

		results = append(results, Result{Name: job.doc.Name, Data: data})
	}

	return results, warnings, nil
}

func executeDataSet(ctx context.Context, session *duckdb.Session, job dataSetJob, opts *ExecuteOptions) (json.RawMessage, error) {
	db := session.DB()

	// Execute the query directly - DataSources are already registered as views
	// Use PRQL if provided, otherwise use SQL query.
	// PRQL is sent directly to DuckDB which compiles it via the prql extension.
	//
	// Resolve query content from $file reference if needed.
	baseDir := filepath.Dir(job.doc.File)

	var query string
	var err error

	if !job.spec.Prql.IsEmpty() {
		query, err = job.spec.Prql.ResolveQuery(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve prql: %w", err)
		}
	} else {
		query, err = job.spec.Query.ResolveQuery(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve query: %w", err)
		}
	}

	if query == "" {
		return nil, fmt.Errorf("no query or prql specified")
	}

	// Log the query before execution
	session.LogQuery(query)

	// Record timing for query execution metadata
	startTime := time.Now()

	// Execute query
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		// Log failed query execution if logger is available
		logQueryExecError(session, query, job.doc.Name, startTime, err)
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	// Serialize to JSON array and capture rows for metadata
	data, columns, rowStrings, err := rowsToJSONWithMeta(rows)
	if err != nil {
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

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Columns() ([]string, error)
}

// rowsToJSONWithMeta serializes rows to JSON and also returns column names and rows as strings
// for CSV embedding in build logs.
func rowsToJSONWithMeta(rows rowScanner) (json.RawMessage, []string, [][]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, nil, err
	}

	var results []map[string]any
	var rowStrings [][]string
	values := make([]any, len(cols))
	valuePtrs := make([]any, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
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

	if results == nil {
		results = []map[string]any{}
	}

	data, err := json.Marshal(results)
	return data, cols, rowStrings, err
}

// valueToString converts a value to its string representation for CSV building.
func valueToString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
