// Package datasource provides data collection from various sources (files, S3, databases).
//
// # Context and Cancellation
//
// All public functions accept context.Context and respect cancellation:
//   - Collect() propagates context to DuckDB session and individual queries
//   - RegisterViews() creates DuckDB views for all DataSources
//   - Context cancellation stops in-flight database operations
//
// DataSources are conceptually DuckDB views. When collected, each DataSource
// is registered as a view and then queried via SELECT * FROM <name>.
// Inline sources are handled specially - their data is returned directly
// without going through DuckDB.
package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// Result captures the evaluated rows for a datasource manifest.
type Result struct {
	Name string
	Data json.RawMessage
	// DuckDBExpression is a SQL expression to register as a view directly.
	// When set, Data is empty and the caller should create a view from this expression.
	DuckDBExpression string
}

// PluginCollectResult holds the response from a plugin datasource collection.
type PluginCollectResult struct {
	JSONRows         []byte
	ColumnTypes      map[string]string
	Ephemeral        bool
	DuckDBExpression string // SQL expression to register as view directly (overrides JSONRows)
}

// PluginCollector can collect data from a plugin-provided datasource.
type PluginCollector interface {
	CollectDataSource(ctx context.Context, name string, rawSpec []byte, env map[string]string, projectRoot string) (*PluginCollectResult, error)
}

// PluginRouter finds the plugin responsible for a given datasource kind.
type PluginRouter interface {
	DataSourcePlugin(kindName string) (PluginCollector, bool)
}

// CollectOptions configures the Collect function.
type CollectOptions struct {
	// PluginRouter routes plugin datasource kinds to the owning plugin.
	// May be nil when no plugins are loaded.
	PluginRouter PluginRouter

	// ProjectRoot is the project root directory, passed to plugins.
	ProjectRoot string
}

// Collect evaluates all DataSource documents. Inline sources are returned
// directly, while external sources (files, databases) are registered as
// DuckDB views and queried via SELECT * FROM <name>.
//
// Non-fatal errors are returned as diagnostics so the preview can stay responsive.
//
// Context cancellation is checked:
//   - At function entry
//   - Before opening the DuckDB session
//   - Before each query execution
//
// The DuckDB session is closed when the function returns, regardless of
// whether it completed normally or was canceled.
func Collect(ctx context.Context, docs []config.Document, opts *CollectOptions) ([]Result, []Diagnostic, error) {
	// Check for cancellation at entry
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	var (
		diags        []Diagnostic
		results      []Result
		externalDocs []datasourceItem
		needsDuckDB  bool
	)

	// Plugin expression results that need DuckDB to resolve.
	type pluginExprResult struct {
		name       string
		expression string
	}
	var pluginExprResults []pluginExprResult

	// Collect all DataSource documents and separate inline from external
	for _, doc := range docs {
		// Route plugin datasource kinds to their owning plugin.
		if doc.Kind != "DataSource" {
			if opts != nil && opts.PluginRouter != nil {
				if p, ok := opts.PluginRouter.DataSourcePlugin(doc.Kind); ok {
					result, err := collectFromPlugin(ctx, p, doc, opts.ProjectRoot)
					if err != nil {
						diags = append(diags, diagnostic(doc.Name, "plugin", err))
						continue
					}
					if result.DuckDBExpression != "" {
						// Plugin returned a SQL expression — needs DuckDB to resolve.
						needsDuckDB = true
						pluginExprResults = append(pluginExprResults, pluginExprResult{
							name:       result.Name,
							expression: result.DuckDBExpression,
						})
					} else {
						results = append(results, result)
					}
				}
			}
			continue
		}

		spec, err := parseSpec(doc.Raw)
		if err != nil {
			diags = append(diags, diagnostic(doc.Name, "spec", err))
			continue
		}

		baseDir := filepath.Dir(doc.File)
		spec.Name = doc.Name
		spec.BaseDir = baseDir

		// Handle inline sources directly - no DuckDB needed
		if spec.Type == sourceTypeInline {
			data, err := spec.inlinePayload()
			if err != nil {
				diags = append(diags, diagnostic(doc.Name, "inline", err))
				continue
			}
			results = append(results, Result{Name: doc.Name, Data: data})
			continue
		}

		// External source - will need DuckDB
		needsDuckDB = true
		externalDocs = append(externalDocs, datasourceItem{
			name: doc.Name,
			spec: spec,
			doc:  doc,
		})
	}

	// If no external sources, we're done
	if !needsDuckDB {
		return results, diags, nil
	}

	// Check for cancellation before opening database session
	if err := ctx.Err(); err != nil {
		return results, diags, err
	}

	dbOpts, err := duckdb.DefaultOptions()
	if err != nil {
		return results, diags, fmt.Errorf("duckdb options: %w", err)
	}

	session, err := duckdb.OpenSession(ctx, dbOpts)
	if err != nil {
		return results, diags, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close()

	// Register external DataSources as views
	// Note: inline sources are handled directly above and don't need views here
	viewDiags, err := RegisterViews(ctx, session, docs, nil)
	if err != nil {
		return results, append(diags, viewDiags...), err
	}

	// Track which datasources failed view creation
	failedViews := make(map[string]bool)
	for _, d := range viewDiags {
		failedViews[d.Datasource] = true
	}
	diags = append(diags, viewDiags...)

	// Query each view to get the data (skip ones that failed to create)
	db := session.DB()

	for _, ds := range externalDocs {
		// Skip if view creation failed
		if failedViews[ds.name] {
			continue
		}

		// Check for cancellation before each query
		if err := ctx.Err(); err != nil {
			return results, diags, err
		}

		data, err := QueryView(ctx, db, session, ds.name)
		if err != nil {
			diags = append(diags, diagnostic(ds.name, "query", err))
			continue
		}

		results = append(results, Result{Name: ds.name, Data: data})
	}

	// Register and query plugin DuckDB expression views.
	for _, pe := range pluginExprResults {
		if err := ctx.Err(); err != nil {
			return results, diags, err
		}
		viewSQL := fmt.Sprintf("CREATE OR REPLACE VIEW %q AS SELECT * FROM (%s)", pe.name, pe.expression) //nolint:gosec // G201: expression is from trusted plugin, not user input
		if _, err := db.ExecContext(ctx, viewSQL); err != nil {
			diags = append(diags, diagnostic(pe.name, "plugin-expression", err))
			continue
		}
		data, err := QueryView(ctx, db, session, pe.name)
		if err != nil {
			diags = append(diags, diagnostic(pe.name, "plugin-expression-query", err))
			continue
		}
		results = append(results, Result{Name: pe.name, Data: data})
	}

	return results, diags, nil
}

type datasourceItem struct {
	name string
	spec sourceSpec
	doc  config.Document
}

// collectFromPlugin calls a plugin's CollectDataSource RPC and returns the result.
func collectFromPlugin(ctx context.Context, p PluginCollector, doc config.Document, projectRoot string) (Result, error) {
	var parsed struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &parsed); err != nil {
		return Result{}, fmt.Errorf("extracting spec: %w", err)
	}

	collectResult, err := p.CollectDataSource(ctx, doc.Name, parsed.Spec, nil, projectRoot)
	if err != nil {
		return Result{}, err
	}

	// If the plugin returned a DuckDB expression, execute it to get rows.
	if collectResult.DuckDBExpression != "" {
		return Result{
			Name:             doc.Name,
			DuckDBExpression: collectResult.DuckDBExpression,
		}, nil
	}

	return Result{
		Name: doc.Name,
		Data: json.RawMessage(collectResult.JSONRows),
	}, nil
}
