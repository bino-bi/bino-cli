package lint

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/spec"
)

// datasetDependencyUndeclared reports DataSources a DataSet query reads without
// listing them in spec.dependencies. The query still runs, because every
// DataSource is a view in every query, but the dataset cache only watches the
// files of the listed dependencies and keeps serving the old rows.
var datasetDependencyUndeclared = Rule{
	ID:          "dataset-dependency-undeclared",
	Name:        "DataSet Dependency Undeclared",
	Description: "A DataSet query must list every DataSource it reads in spec.dependencies; otherwise the dataset cache misses changes to it.",
	Check: func(ctx context.Context, docs []Document) []Finding {
		var findings []Finding

		reads, _ := datasetReads(ctx, docs)
		for _, r := range reads {
			for _, name := range r.reads {
				if slices.Contains(r.deps, name) {
					continue
				}
				findings = append(findings, Finding{
					RuleID:  "dataset-dependency-undeclared",
					Message: fmt.Sprintf("query reads DataSource %q but spec.dependencies does not list it; the dataset cache will not notice when it changes", name),
					File:    r.doc.File,
					DocIdx:  r.doc.Position,
					Path:    "spec.query",
				})
			}
		}

		return findings
	},
}

// datasetDependencyUnused reports spec.dependencies entries naming a DataSource
// the DataSet query never reads.
var datasetDependencyUnused = Rule{
	ID:          "dataset-dependency-unused",
	Name:        "DataSet Dependency Unused",
	Description: "Every DataSource listed in a DataSet's spec.dependencies should be read by its query.",
	Check: func(ctx context.Context, docs []Document) []Finding {
		var findings []Finding

		reads, dataSources := datasetReads(ctx, docs)
		for _, r := range reads {
			for i, dep := range r.deps {
				if dataSources[strings.ToLower(dep)] != dep || slices.Contains(r.reads, dep) {
					continue
				}
				findings = append(findings, Finding{
					RuleID:   "dataset-dependency-unused",
					Message:  fmt.Sprintf("spec.dependencies lists %q but the query never reads it", dep),
					File:     r.doc.File,
					DocIdx:   r.doc.Position,
					Path:     fmt.Sprintf("spec.dependencies.%d", i),
					Severity: "info",
				})
			}
		}

		return findings
	},
}

// datasetRead holds the DataSources a DataSet query reads and the names its
// spec.dependencies lists.
type datasetRead struct {
	doc   Document
	deps  []string
	reads []string
}

// datasetReads parses the SQL query of every DataSet and matches the tables it
// reads against the DataSource names, case-insensitively as DuckDB resolves
// them. It also returns those names, keyed by their lower-case form. DataSets
// whose reads cannot be known are left out: generated inline DataSets, source
// and PRQL DataSets, and SQL DuckDB cannot serialize.
func datasetReads(ctx context.Context, docs []Document) (reads []datasetRead, dataSources map[string]string) {
	dataSources = make(map[string]string)
	for _, doc := range docs {
		if doc.Kind == "DataSource" {
			dataSources[strings.ToLower(doc.Name)] = doc.Name
		}
	}
	if len(dataSources) == 0 {
		return nil, nil
	}

	// One in-memory database, opened only once there is a query to parse.
	var (
		db   *sql.DB
		conn *sql.Conn
	)
	defer func() {
		if conn != nil {
			conn.Close() //nolint:errcheck // teardown of a parse-only in-memory database
		}
		if db != nil {
			db.Close() //nolint:errcheck // teardown of a parse-only in-memory database
		}
	}()

	for _, doc := range docs {
		if doc.Kind != "DataSet" || doc.Labels["bino.bi/generated"] == "true" {
			continue
		}

		var payload struct {
			Spec struct {
				Query        spec.QueryField `json:"query"`
				Dependencies []string        `json:"dependencies"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil || payload.Spec.Query.IsEmpty() {
			continue
		}

		if conn == nil {
			var err error
			if db, err = sql.Open("duckdb", ""); err != nil {
				return nil, nil
			}
			if conn, err = db.Conn(ctx); err != nil {
				return nil, nil
			}
		}

		tables, ok := dataset.ReadTables(ctx, conn, config.Document{File: doc.File, Raw: doc.Raw})
		if !ok {
			continue
		}
		r := datasetRead{doc: doc, deps: payload.Spec.Dependencies}
		for _, table := range tables {
			if name, ok := dataSources[strings.ToLower(table)]; ok {
				r.reads = append(r.reads, name)
			}
		}
		reads = append(reads, r)
	}

	return reads, dataSources
}
