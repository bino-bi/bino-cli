// Package datacheck runs the checks behind `bino lint --with-data`: every
// DataSet query runs with LIMIT 0, the columns the components read are
// compared with the query results, and local CSV files are sniffed. It finds
// the data problems that otherwise only fail the build.
package datacheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/pkg/duckdb"
)

// Result holds what the checks found before ctx ended.
type Result struct {
	Findings []lint.Finding
	// Unchecked names the DataSets ("name") and DataSources ("$name") the
	// checks did not reach because ctx ended.
	Unchecked []string
	// FailedDataSets are the DataSets with a dataset-sql finding.
	FailedDataSets []string
	// FailedSources are the DataSources whose view error a dataset-sql
	// finding already names.
	FailedSources []string
}

// Run checks the data of docs, once per document set of Variants. Findings
// that repeat across sets are reported once. It stops when ctx ends and
// returns what it found so far; only a failure to start DuckDB is an error.
func Run(ctx context.Context, docs []config.Document, kindProvider config.KindProvider) (Result, error) {
	sets, _ := Variants(docs)
	var merged Result
	failed := map[string]bool{}
	sources := map[string]bool{}
	for _, set := range sets {
		res, err := RunSet(ctx, set, kindProvider)
		if err != nil {
			return Result{}, err
		}
		merged.Findings = append(merged.Findings, res.Findings...)
		for _, n := range res.Unchecked {
			if !slices.Contains(merged.Unchecked, n) {
				merged.Unchecked = append(merged.Unchecked, n)
			}
		}
		for _, n := range res.FailedDataSets {
			failed[n] = true
		}
		for _, n := range res.FailedSources {
			sources[n] = true
		}
	}
	merged.Findings = Dedupe(merged.Findings)
	merged.FailedDataSets = sortedKeys(failed)
	merged.FailedSources = sortedKeys(sources)
	return merged, nil
}

// artefactKinds are the kinds that constraints never filter out.
var artefactKinds = map[string]bool{
	"ReportArtefact":     true,
	"ScreenshotArtefact": true,
	"DocumentArtefact":   true,
	"LiveReportArtefact": true,
}

// Variants returns the document sets the data checks run on, so two
// variants of one DataSet never meet. Without constraints that is docs
// itself. Otherwise there is one set per constraint context the build and
// serve use, without repeats:
//   - each ReportArtefact in build mode, ArtefactKind "report";
//   - each ScreenshotArtefact in build mode, ArtefactKind "screenshot"; its
//     build runs every DataSet its constraints keep, so the set does too;
//   - each ReportArtefact a LiveReportArtefact route serves, in serve mode,
//     ArtefactKind "report", as serve renders the route.
//
// A DocumentArtefact gets no set: its build applies no constraint filter and
// only runs the DataSets its markdown reaches, so those are checked through
// the other sets or named as not checked. A document whose constraints
// cannot be evaluated is left out of a set. A project with constraints and
// none of these contexts uses the documents introspection would pick.
// uncovered names the DataSets ("name") and DataSources ("$name") that no
// set contains.
func Variants(docs []config.Document) (sets [][]config.Document, uncovered []string) {
	constrained := false
	for _, d := range docs {
		if len(d.Constraints) > 0 {
			constrained = true
		}
	}
	if !constrained {
		return [][]config.Document{docs}, nil
	}
	ctxs := constraintContexts(docs)
	covered := map[int]bool{}
	if len(ctxs) == 0 {
		resolved, _ := config.ResolveForIntrospection(docs)
		for i, d := range docs {
			for _, r := range resolved {
				if r.File == d.File && r.Position == d.Position {
					covered[i] = true
				}
			}
		}
		sets = [][]config.Document{resolved}
	}
	seen := map[string]bool{}
	for _, cctx := range ctxs {
		var set []config.Document
		var key strings.Builder
		for i, d := range docs {
			if !artefactKinds[d.Kind] && len(d.Constraints) > 0 {
				match, err := spec.EvaluateParsedConstraintsWithContext(d.Constraints, cctx, d.Kind, d.Name)
				if err != nil || !match {
					continue
				}
			}
			set = append(set, d)
			covered[i] = true
			fmt.Fprintf(&key, "%d,", i)
		}
		if !seen[key.String()] {
			seen[key.String()] = true
			sets = append(sets, set)
		}
	}
	for i, d := range docs {
		name := d.Name
		switch d.Kind {
		case "DataSet":
		case "DataSource":
			name = "$" + name
		default:
			continue
		}
		if !covered[i] && !slices.Contains(uncovered, name) {
			uncovered = append(uncovered, name)
		}
	}
	return sets, uncovered
}

// constraintContexts returns the contexts Variants filters with, in
// document order.
func constraintContexts(docs []config.Document) []*spec.ConstraintContext {
	reports := map[string]config.Document{}
	for _, d := range docs {
		if d.Kind == "ReportArtefact" {
			if _, dup := reports[d.Name]; !dup {
				reports[d.Name] = d
			}
		}
	}
	newContext := func(d config.Document, mode spec.Mode, kind string) *spec.ConstraintContext {
		specMap, err := spec.ToMap(d.Raw)
		if err != nil {
			return nil
		}
		return &spec.ConstraintContext{Labels: d.Labels, Spec: specMap, Mode: mode, ArtefactKind: kind}
	}
	var out []*spec.ConstraintContext
	add := func(c *spec.ConstraintContext) {
		if c != nil {
			out = append(out, c)
		}
	}
	for _, d := range docs {
		switch d.Kind {
		case "ReportArtefact":
			add(newContext(d, spec.ModeBuild, "report"))
		case "ScreenshotArtefact":
			add(newContext(d, spec.ModeBuild, "screenshot"))
		case "LiveReportArtefact":
			var live struct {
				Spec config.LiveReportArtefactSpec `json:"spec"`
			}
			if json.Unmarshal(d.Raw, &live) != nil {
				continue
			}
			for _, route := range sortedKeys(live.Spec.Routes) {
				if r, ok := reports[live.Spec.Routes[route].Artifact]; ok {
					add(newContext(r, spec.ModeServe, "report"))
				}
			}
		}
	}
	return out
}

// RunSet checks one consistent set of documents, one of Variants.
func RunSet(ctx context.Context, docs []config.Document, kindProvider config.KindProvider) (Result, error) {
	c := &checker{
		docs:         docs,
		src:          newSources(docs),
		columns:      map[string]columnSet{},
		failed:       map[string]bool{},
		usedViewErrs: map[string]bool{},
	}
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		return Result{}, fmt.Errorf("duckdb options: %w", err)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		if ctx.Err() != nil {
			c.skipAll()
			return Result{Unchecked: c.unchecked}, nil
		}
		return Result{}, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session
	c.session = session

	tempDir, err := os.MkdirTemp("", "bino-lint-data-")
	if err != nil {
		return Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	viewDiags, err := datasource.RegisterViews(ctx, session, docs, &datasource.ViewsOptions{TempDir: tempDir})
	if err != nil && ctx.Err() == nil {
		return Result{}, fmt.Errorf("register views: %w", err)
	}
	c.viewErrs = map[string]error{}
	for _, d := range viewDiags {
		c.viewErrs[d.Datasource] = d.Err
	}

	c.checkDataSets(ctx)
	c.checkCSVFiles(ctx)
	if ctx.Err() == nil {
		for _, g := range buildGraphs(ctx, docs, kindProvider) {
			c.checkComponentColumns(ctx, g)
		}
	}

	return Result{
		Findings:       Dedupe(c.findings),
		Unchecked:      c.unchecked,
		FailedDataSets: sortedKeys(c.failed),
		FailedSources:  sortedKeys(c.usedViewErrs),
	}, nil
}

type columnSet map[string]bool

type checker struct {
	docs     []config.Document
	src      *sources
	session  *duckdb.Session
	viewErrs map[string]error
	// columns maps a binding ("name" for a DataSet, "$name" for a
	// DataSource) to its result columns; a missing entry was not probed.
	columns map[string]columnSet
	failed  map[string]bool
	// usedViewErrs are the DataSources whose view error a finding names.
	usedViewErrs map[string]bool
	findings     []lint.Finding
	unchecked    []string
}

func (c *checker) skip(name string) {
	for _, n := range c.unchecked {
		if n == name {
			return
		}
	}
	c.unchecked = append(c.unchecked, name)
}

// skipAll marks every DataSet and csv DataSource as not checked.
func (c *checker) skipAll() {
	for _, doc := range c.docs {
		if doc.Kind == "DataSet" {
			c.skip(doc.Name)
		}
	}
	for _, doc := range c.docs {
		if _, _, ok := datasource.LocalCSV(doc); ok {
			c.skip("$" + doc.Name)
		}
	}
}

// checkDataSets runs every DataSet query with LIMIT 0 and keeps its columns.
func (c *checker) checkDataSets(ctx context.Context) {
	for _, doc := range c.docs {
		if doc.Kind != "DataSet" {
			continue
		}
		if ctx.Err() != nil {
			c.skip(doc.Name)
			continue
		}
		cols, err := dataset.ProbeColumns(ctx, c.session, doc)
		if ctx.Err() != nil {
			c.skip(doc.Name)
			continue
		}
		if err != nil {
			c.failed[doc.Name] = true
			c.findings = append(c.findings, c.sqlFinding(doc, err))
			continue
		}
		set := c.columns[doc.Name]
		if set == nil {
			set = columnSet{}
			c.columns[doc.Name] = set
		}
		for _, col := range cols {
			set[col] = true
		}
	}
}

// sqlFinding turns a failed DataSet query into a finding on the query line
// DuckDB names. A DataSource whose view failed only shows up as a missing
// table, so its own error is added.
func (c *checker) sqlFinding(doc config.Document, err error) lint.Finding {
	f := c.src.queryErrorFinding(doc, err)
	text := strings.ToLower(err.Error())
	for _, name := range sortedKeys(c.viewErrs) {
		if strings.Contains(text, strings.ToLower(name)) {
			f.Message += fmt.Sprintf("; DataSource %q: %v", name, c.viewErrs[name])
			c.usedViewErrs[name] = true
		}
	}
	return f
}

// checkCSVFiles sniffs the local file of every csv DataSource.
func (c *checker) checkCSVFiles(ctx context.Context) {
	for _, doc := range c.docs {
		path, declared, ok := datasource.LocalCSV(doc)
		if !ok {
			continue
		}
		if ctx.Err() != nil {
			c.skip("$" + doc.Name)
			continue
		}
		rep, err := datasource.InspectCSV(ctx, c.session.DB(), path, declared)
		if ctx.Err() != nil {
			c.skip("$" + doc.Name)
			continue
		}
		if err != nil {
			c.findings = append(c.findings, c.src.csvFinding(doc, "", fmt.Sprintf("DuckDB cannot sniff %s: %s", path, firstLine(err.Error()))))
			continue
		}
		for _, issue := range rep.Issues {
			c.findings = append(c.findings, c.src.csvFinding(doc, issue.Option, issue.Message))
		}
	}
}

// bindingColumns returns the columns of a component's dataset binding;
// ok is false when they are unknown (not probed, failed, or no such name).
func (c *checker) bindingColumns(ctx context.Context, binding string) (columnSet, bool) {
	if set, ok := c.columns[binding]; ok {
		return set, set != nil
	}
	name, isSource := strings.CutPrefix(binding, "$")
	if !isSource || ctx.Err() != nil || c.src.doc("DataSource", name) == nil {
		return nil, false
	}
	rows, err := c.session.DB().QueryContext(ctx, fmt.Sprintf("SELECT * FROM %q LIMIT 0", name))
	if err != nil {
		c.columns[binding] = nil
		return nil, false
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		c.columns[binding] = nil
		return nil, false
	}
	set := columnSet{}
	for _, col := range cols {
		set[col] = true
	}
	c.columns[binding] = set
	return set, true
}

// buildGraphs returns the dependency graph of docs. One broken ref fails
// the whole build, so on error each layout is built on its own with the
// other documents, and the layouts that still fail are left out: the
// reference rules report them.
func buildGraphs(ctx context.Context, docs []config.Document, kindProvider config.KindProvider) []*graph.Graph {
	if g, err := graph.Build(ctx, docs, kindProvider); err == nil {
		return []*graph.Graph{g}
	}
	var base, roots []config.Document
	for _, d := range docs {
		if d.Kind == "LayoutPage" || d.Kind == "LayoutCard" {
			roots = append(roots, d)
		} else {
			base = append(base, d)
		}
	}
	var out []*graph.Graph
	if g, err := graph.Build(ctx, base, kindProvider); err == nil {
		out = append(out, g)
	}
	for _, r := range roots {
		withCards := append(slices.Clone(base), r)
		for _, d := range roots {
			if d.Kind == "LayoutCard" && (d.Name != r.Name || d.File != r.File) {
				withCards = append(withCards, d)
			}
		}
		for _, set := range [][]config.Document{withCards, append(slices.Clone(base), r)} {
			if g, err := graph.Build(ctx, set, kindProvider); err == nil {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Dedupe drops repeated findings: a component used by ref is checked both
// on its own and inside its layout, and a document shared by several
// Variants is checked in each of them.
func Dedupe(findings []lint.Finding) []lint.Finding {
	type key struct {
		rule, file, path, msg string
		line, col             int
	}
	seen := map[key]bool{}
	out := findings[:0]
	for _, f := range findings {
		k := key{f.RuleID, f.File, f.Path, f.Message, f.Line, f.Column}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}
