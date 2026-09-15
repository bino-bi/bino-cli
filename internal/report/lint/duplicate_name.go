package lint

import (
	"context"
	"fmt"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// duplicateName reports a document whose kind and name are already used by an
// earlier document when a ReportArtefact includes both after applying constraints,
// in build or preview mode. Rendering that artefact fails the same way
// (config.ValidateArtefactNames).
var duplicateName = Rule{
	ID:          "duplicate-name",
	Name:        "Duplicate Name",
	Description: "Documents of one kind must have unique names within each ReportArtefact after constraints are applied.",
	Check: func(_ context.Context, docs []Document) []Finding {
		type nameKey struct{ kind, name string }
		type docPair struct{ first, later int }

		tracked := config.UniqueNameKinds(nil)
		reported := make(map[docPair]bool)
		var findings []Finding

		for _, artefact := range docs {
			if artefact.Kind != "ReportArtefact" {
				continue
			}
			specMap, err := spec.ToMap(artefact.Raw)
			if err != nil {
				continue
			}

			for _, mode := range []spec.Mode{spec.ModeBuild, spec.ModePreview} {
				constraintCtx := &spec.ConstraintContext{
					Labels:       artefact.Labels,
					Spec:         specMap,
					Mode:         mode,
					ArtefactKind: "report",
				}

				first := make(map[nameKey]int)
				for i, doc := range docs {
					if doc.Kind == "ReportArtefact" || doc.Name == "" {
						continue
					}
					if _, ok := tracked[doc.Kind]; !ok {
						continue
					}
					// A constraint that cannot be evaluated is not a duplicate; the render reports it.
					if match, err := spec.EvaluateParsedConstraints(doc.Constraints, constraintCtx); err != nil || !match {
						continue
					}

					key := nameKey{kind: doc.Kind, name: doc.Name}
					j, seen := first[key]
					if !seen {
						first[key] = i
						continue
					}
					pair := docPair{first: j, later: i}
					if reported[pair] {
						continue
					}
					reported[pair] = true

					other := docs[j]
					findings = append(findings, Finding{
						RuleID:   "duplicate-name",
						Message:  fmt.Sprintf("duplicate %s name %q: also defined in %s #%d; both are included in artefact %q after applying constraints", doc.Kind, doc.Name, other.File, other.Position, artefact.Name),
						File:     doc.File,
						DocIdx:   doc.Position,
						Path:     "metadata.name",
						Severity: "error",
					})
				}
			}
		}

		return findings
	},
}
