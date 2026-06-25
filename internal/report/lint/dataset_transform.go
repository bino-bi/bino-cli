package lint

import (
	"context"

	"bino.bi/bino/internal/report/dataset"
)

// datasetTransformInvalid surfaces the semantic checks on a DataSet's
// declarative transforms (filter / groupBy / indexColumns) that the JSON schema
// cannot express. It delegates to dataset.ValidateSpec, which returns a hard
// error (op/value mismatch, duplicate aggregate "as", hash-without-of,
// duplicate/colliding index names, ...) and/or non-fatal warnings (e.g. lenient
// naming, a nondeterministic first/last without orderBy). Both the error and the
// warnings are reported under this single rule ID; a query-only DataSet (no
// transforms) produces nothing, so existing datasets are never flagged.
var datasetTransformInvalid = Rule{
	ID:          "dataset-transform-invalid",
	Name:        "DataSet Transform Invalid",
	Description: "DataSet filter/groupBy/indexColumns transforms must be semantically coherent (op/value match, unique aggregate and index names, hash requires 'of', ...). Reports both hard errors and lenient warnings the JSON schema cannot express.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "DataSet" {
				continue
			}

			warnings, err := dataset.ValidateSpec(doc.Raw)
			if err != nil {
				findings = append(findings, Finding{
					RuleID:  "dataset-transform-invalid",
					Message: err.Error(),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec",
				})
			}
			for _, w := range warnings {
				findings = append(findings, Finding{
					RuleID:  "dataset-transform-invalid",
					Message: w,
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec",
				})
			}
		}

		return findings
	},
}
