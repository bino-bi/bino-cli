package config

import (
	"errors"
	"fmt"
	"strings"

	"bino.bi/bino/internal/report/spec"
)

// IntrospectionMode is the constraint mode used by authoring tools (daemon,
// MCP, LSP, preview data explorer) when they resolve documents without an
// artefact of their own.
const IntrospectionMode = spec.ModePreview

// AmbiguityError is a kind and name defined by several documents where the
// project's ReportArtefacts do not select exactly one of them.
type AmbiguityError struct {
	Kind  string
	Name  string
	Files []string // "file #position" of each candidate document
}

func (a AmbiguityError) Error() string {
	return fmt.Sprintf("%s %q is defined %d times (%s) and the ReportArtefacts do not select exactly one of them in %s mode; introspection refuses to guess. Make the constraints select one definition for every ReportArtefact",
		a.Kind, a.Name, len(a.Files), strings.Join(a.Files, ", "), IntrospectionMode)
}

// Ambiguities lists the names ResolveForIntrospection could not resolve.
type Ambiguities []AmbiguityError

// Lookup returns the ambiguity for a name as authoring tools look it up in
// the resolved docs: "$x" is the DataSource x, a bare "x" is the DataSet x or
// else the DataSource x.
func (as Ambiguities) Lookup(docs []Document, name string) error {
	isDataSource := strings.HasPrefix(name, "$")
	lookup := strings.TrimPrefix(name, "$")
	for _, a := range as {
		if a.Name != lookup {
			continue
		}
		switch {
		case isDataSource && a.Kind == "DataSource", !isDataSource && a.Kind == "DataSet":
			return a
		case !isDataSource && a.Kind == "DataSource" && !hasDocument(docs, "DataSet", lookup):
			return a
		}
	}
	return nil
}

func hasDocument(docs []Document, kind, name string) bool {
	for _, d := range docs {
		if d.Kind == kind && d.Name == name {
			return true
		}
	}
	return false
}

// Explain adds the ambiguities that can explain a query error: a DataSet that
// reads an unresolved DataSource fails with a missing-table error that hides
// the cause. DataSources and DataSets count when the error names them (SQL
// identifiers are case-insensitive); ConnectionSecrets always count, since an
// error never names the secret behind a failed source.
func (as Ambiguities) Explain(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	errs := []error{err}
	for _, a := range as {
		if a.Kind == "ConnectionSecret" || strings.Contains(msg, strings.ToLower(a.Name)) {
			errs = append(errs, a)
		}
	}
	return errors.Join(errs...)
}

// dataKinds are the kinds that decide which rows a query reads.
var dataKinds = map[string]bool{"DataSource": true, "DataSet": true, "ConnectionSecret": true}

// ResolveForIntrospection returns the documents a tool should query when it
// has no artefact of its own. A data document (DataSource, DataSet,
// ConnectionSecret) whose kind and name are defined once is kept as is. When
// several share a kind and name, their constraints are evaluated against every
// ReportArtefact in IntrospectionMode, and the one document all artefacts
// agree on is kept. If artefacts pick different documents, or none, all
// candidates are dropped and reported, so a tool never shows data from a
// definition that the build would exclude. Other kinds are returned unchanged.
func ResolveForIntrospection(docs []Document) ([]Document, Ambiguities) {
	type key struct{ kind, name string }
	groups := map[key][]int{}
	var ctxs []*spec.ConstraintContext
	for i, doc := range docs {
		if doc.Kind == "ReportArtefact" {
			specMap, err := spec.ToMap(doc.Raw)
			if err != nil {
				continue
			}
			ctxs = append(ctxs, &spec.ConstraintContext{
				Labels:       doc.Labels,
				Spec:         specMap,
				Mode:         IntrospectionMode,
				ArtefactKind: "report",
			})
			continue
		}
		if doc.Name != "" && dataKinds[doc.Kind] {
			k := key{doc.Kind, doc.Name}
			groups[k] = append(groups[k], i)
		}
	}

	drop := map[int]bool{}
	var ambiguous Ambiguities
	for i, doc := range docs {
		idx := groups[key{doc.Kind, doc.Name}]
		if len(idx) < 2 || idx[0] != i {
			continue
		}
		selected := map[int]bool{}
		for _, ctx := range ctxs {
			for _, j := range idx {
				// A constraint that cannot be evaluated counts as no match, as in the duplicate-name lint rule.
				if match, err := spec.EvaluateParsedConstraints(docs[j].Constraints, ctx); err == nil && match {
					selected[j] = true
				}
			}
		}
		if len(selected) == 1 {
			for _, j := range idx {
				drop[j] = !selected[j]
			}
			continue
		}
		a := AmbiguityError{Kind: doc.Kind, Name: doc.Name}
		for _, j := range idx {
			drop[j] = true
			a.Files = append(a.Files, fmt.Sprintf("%s #%d", docs[j].File, docs[j].Position))
		}
		ambiguous = append(ambiguous, a)
	}

	if len(drop) == 0 {
		return docs, nil
	}
	kept := make([]Document, 0, len(docs))
	for i, doc := range docs {
		if !drop[i] {
			kept = append(kept, doc)
		}
	}
	return kept, ambiguous
}
