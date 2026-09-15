package dataset

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
	"bino.bi/bino/pkg/duckdb"
)

// ViewPrefix starts the name of every view the CLI creates while executing a
// dataset. DataSource and DataSet names must not start with it (see the
// reserved-name-prefix lint rule and the check in executeDataSets).
const ViewPrefix = "_bino_ds_"

// ShiftDeclaration is the derive/assert entry of a DataSet spec.
type ShiftDeclaration = reportspec.ShiftDeclaration

// Compiled is the executable form of a DataSet: the statements to run first
// and the final SELECT. Setup is empty for SQL and source datasets without
// derive/assert, so their query text is exactly what runs today.
type Compiled struct {
	// Setup holds statements to run before Query, in order: the view that
	// materializes the author's query and one view per intermediate derive layer.
	Setup []string
	// Query is the final SELECT.
	Query string
	// Prql reports that the prql extension must be loaded before Setup and Query run.
	Prql bool
	// View is the name of the base view when Setup is non-empty.
	View string
	// Derive and Assert are the validated declarations, keyed by pp slot.
	Derive map[string]reportspec.ShiftDeclaration
	Assert map[string]reportspec.ShiftDeclaration
}

// Declares reports whether the dataset declares any derived or asserted slot.
func (c Compiled) Declares() bool {
	return len(c.Derive) > 0 || len(c.Assert) > 0
}

var (
	slotNames = func() map[string]bool {
		m := map[string]bool{}
		for _, c := range standardColumns {
			if c.Group == "Measures" {
				m[c.Name] = true
			}
		}
		return m
	}()
	shiftPattern = regexp.MustCompile(`^[1-9]\d* (day|week|month|quarter|year)$`)
	grains       = map[string]bool{"day": true, "week": true, "month": true, "quarter": true, "year": true}
)

// Compile resolves a DataSet document into the statements that produce its rows.
//
//   - spec.source        -> SELECT * FROM "<source>"
//   - spec.query / prql  -> inline text or the $file contents, read relative to
//     the manifest's directory; @inline(N) references are rewritten
//
// PRQL is always materialized as a view in the prql extension's delimited form,
// CREATE OR REPLACE VIEW ... AS (| <prql> |), because bare PRQL cannot be used
// as a subquery. With derive/assert declared the author's query becomes a view
// too, and the final query wraps it in one bino_shift call per derived slot.
// Nothing the author typed is interpolated into generated text: slot names come
// from a closed set, shift and grain are validated against fixed patterns, and
// the view name is chosen here.
func Compile(doc config.Document) (Compiled, error) {
	spec, err := parseDataSetSpec(doc.Raw)
	if err != nil {
		return Compiled{}, fmt.Errorf("parse dataset spec: %w", err)
	}
	return compileSpec(doc, spec)
}

func compileSpec(doc config.Document, spec dataSetSpec) (Compiled, error) {
	baseDir := filepath.Dir(doc.File)

	var query string
	var err error
	prql := false
	switch {
	case spec.Source != "":
		query = fmt.Sprintf("SELECT * FROM %q", spec.Source)
	case !spec.Prql.IsEmpty():
		prql = true
		query, err = spec.Prql.ResolveQuery(baseDir)
		if err != nil {
			return Compiled{}, fmt.Errorf("resolve prql: %w", err)
		}
	default:
		query, err = spec.Query.ResolveQuery(baseDir)
		if err != nil {
			return Compiled{}, fmt.Errorf("resolve query: %w", err)
		}
	}
	if query == "" {
		return Compiled{}, fmt.Errorf("no query, prql, or source specified")
	}

	if HasInlineRefs(query) {
		query, err = RewriteInlineRefs(query, spec.Dependencies)
		if err != nil {
			return Compiled{}, fmt.Errorf("rewrite inline refs: %w", err)
		}
	}

	if err := validateDeclarations(spec.Derive, spec.Assert); err != nil {
		return Compiled{}, err
	}

	c := Compiled{Query: query, Prql: prql, Derive: spec.Derive, Assert: spec.Assert}
	if !prql && !c.Declares() {
		return c, nil
	}

	c.View = ViewPrefix + doc.Name
	if prql {
		body := strings.TrimRight(strings.TrimSpace(query), "; \t\r\n")
		c.Setup = append(c.Setup, fmt.Sprintf("CREATE OR REPLACE VIEW %q AS (| %s |)", c.View, body))
	} else {
		c.Setup = append(c.Setup, fmt.Sprintf("CREATE OR REPLACE VIEW %q AS (%s)", c.View, query))
	}
	c.Query = fmt.Sprintf("SELECT * FROM %q", c.View)

	// One bino_shift layer per derived slot, in slot order. Every layer but the
	// last becomes a view (a macro argument must be a name); the last is the query.
	prev := c.View
	slots := sortedSlots(c.Derive)
	for i, slot := range slots {
		d := c.Derive[slot]
		layer := fmt.Sprintf("SELECT * EXCLUDE (shifted), shifted AS %s FROM %s('%s', '%s', '%s', '%s')",
			slot, duckdb.ShiftMacroName, prev, d.From, d.Shift, d.Grain)
		if i == len(slots)-1 {
			c.Query = layer
			break
		}
		name := c.View + "__" + slot
		c.Setup = append(c.Setup, fmt.Sprintf("CREATE OR REPLACE VIEW %q AS (%s)", name, layer))
		prev = name
	}
	return c, nil
}

func validateDeclarations(derive, assert map[string]reportspec.ShiftDeclaration) error {
	for _, kind := range []struct {
		name string
		m    map[string]reportspec.ShiftDeclaration
	}{{"derive", derive}, {"assert", assert}} {
		for _, slot := range sortedSlots(kind.m) {
			d := kind.m[slot]
			if !strings.HasPrefix(slot, "pp") || !slotNames[slot] {
				return fmt.Errorf("%s: %q is not a previous-period slot (pp1..pp4)", kind.name, slot)
			}
			if !slotNames[d.From] {
				return fmt.Errorf("%s %s: from %q is not a scenario slot", kind.name, slot, d.From)
			}
			if !shiftPattern.MatchString(d.Shift) {
				return fmt.Errorf("%s %s: shift %q must be '<n> <day|week|month|quarter|year>'", kind.name, slot, d.Shift)
			}
			if !grains[d.Grain] {
				return fmt.Errorf("%s %s: grain %q must be one of day, week, month, quarter, year", kind.name, slot, d.Grain)
			}
		}
	}
	for _, slot := range sortedSlots(derive) {
		if _, ok := assert[slot]; ok {
			return fmt.Errorf("slot %s declared in both derive and assert", slot)
		}
	}
	return nil
}

func sortedSlots(m map[string]reportspec.ShiftDeclaration) []string {
	slots := make([]string, 0, len(m))
	for s := range m {
		slots = append(slots, s)
	}
	sort.Strings(slots)
	return slots
}

// LimitQuery wraps a compiled query so that at most limit rows are returned.
// A trailing semicolon is stripped so the text can be used as a subquery.
func LimitQuery(query string, limit int) string {
	body := strings.TrimRight(strings.TrimSpace(query), "; \t\r\n")
	return fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d", body, limit)
}

// NeedsPrql reports whether any DataSet document uses a PRQL query.
func NeedsPrql(docs []config.Document) bool {
	for _, doc := range docs {
		if doc.Kind != "DataSet" {
			continue
		}
		spec, err := parseDataSetSpec(doc.Raw)
		if err != nil {
			continue
		}
		if !spec.Prql.IsEmpty() {
			return true
		}
	}
	return false
}

// RunSetup executes the compiled setup statements on the session.
func RunSetup(ctx context.Context, session *duckdb.Session, c Compiled) error {
	for _, stmt := range c.Setup {
		session.LogQuery(stmt)
		if _, err := session.DB().ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
	}
	return nil
}

// RegisterDataSetViews compiles every DataSet document and runs its setup, so
// a long-lived session holds the views a compiled query refers to. Failures
// are returned as warnings; the other datasets are still registered.
func RegisterDataSetViews(ctx context.Context, session *duckdb.Session, docs []config.Document) []Warning {
	var warnings []Warning
	for _, doc := range docs {
		if doc.Kind != "DataSet" {
			continue
		}
		c, err := Compile(doc)
		if err != nil {
			warnings = append(warnings, Warning{DataSet: doc.Name, Message: err.Error()})
			continue
		}
		if err := RunSetup(ctx, session, c); err != nil {
			warnings = append(warnings, Warning{DataSet: doc.Name, Message: err.Error()})
		}
	}
	return warnings
}
