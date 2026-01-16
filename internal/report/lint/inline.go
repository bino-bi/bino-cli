package lint

import (
	"context"
	"encoding/json"
	"strings"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/spec"
)

// inlineRefBounds ensures that @inline(N) references in queries are within bounds.
var inlineRefBounds = Rule{
	ID:          "inline-ref-bounds",
	Name:        "Inline Reference Bounds",
	Description: "@inline(N) references must have valid indices within the dependencies array.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "DataSet" {
				continue
			}

			var payload struct {
				Spec struct {
					Query        spec.QueryField   `json:"query"`
					Prql         spec.QueryField   `json:"prql"`
					Dependencies []json.RawMessage `json:"dependencies"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue
			}

			// Get the query string (from query or prql)
			queryStr := payload.Spec.Query.Inline
			if queryStr == "" {
				queryStr = payload.Spec.Prql.Inline
			}
			if queryStr == "" {
				continue
			}

			// Count inline dependencies (objects without string refs)
			inlineCount := 0
			for _, dep := range payload.Spec.Dependencies {
				// Try to unmarshal as string - if it fails, it's an inline object
				var strRef string
				if json.Unmarshal(dep, &strRef) != nil {
					inlineCount++
				}
			}

			// Validate inline refs
			errs := dataset.ValidateInlineRefs(queryStr, inlineCount)
			for _, err := range errs {
				findings = append(findings, Finding{
					RuleID:  "inline-ref-bounds",
					Message: err.Error(),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.query",
				})
			}
		}

		return findings
	},
}

// datasetSourceExclusive ensures that 'source' is mutually exclusive with 'query' and 'prql'.
var datasetSourceExclusive = Rule{
	ID:          "dataset-source-exclusive",
	Name:        "DataSet Source Exclusive",
	Description: "The 'source' field is mutually exclusive with 'query' and 'prql' in DataSet specs.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "DataSet" {
				continue
			}

			var payload struct {
				Spec struct {
					Query  *json.RawMessage `json:"query"`
					Prql   *json.RawMessage `json:"prql"`
					Source *json.RawMessage `json:"source"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue
			}

			// Check for mutually exclusive fields
			hasQuery := payload.Spec.Query != nil && len(*payload.Spec.Query) > 0 && string(*payload.Spec.Query) != "null"
			hasPrql := payload.Spec.Prql != nil && len(*payload.Spec.Prql) > 0 && string(*payload.Spec.Prql) != "null"
			hasSource := payload.Spec.Source != nil && len(*payload.Spec.Source) > 0 && string(*payload.Spec.Source) != "null"

			if hasSource && hasQuery {
				findings = append(findings, Finding{
					RuleID:  "dataset-source-exclusive",
					Message: "'source' cannot be used together with 'query'; choose one or the other",
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.source",
				})
			}
			if hasSource && hasPrql {
				findings = append(findings, Finding{
					RuleID:  "dataset-source-exclusive",
					Message: "'source' cannot be used together with 'prql'; choose one or the other",
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.source",
				})
			}
		}

		return findings
	},
}

// inlineNamingConflict ensures user-defined names don't start with the reserved '_inline_' prefix.
var inlineNamingConflict = Rule{
	ID:          "inline-naming-conflict",
	Name:        "Inline Naming Conflict",
	Description: "Document names must not start with '_inline_' as this prefix is reserved for generated inline definitions.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			// Skip documents that are already generated (have the generated label)
			if doc.Labels != nil && doc.Labels["bino.bi/generated"] == "true" {
				continue
			}

			if strings.HasPrefix(doc.Name, "_inline_") {
				findings = append(findings, Finding{
					RuleID:  "inline-naming-conflict",
					Message: "name '" + doc.Name + "' uses reserved '_inline_' prefix; choose a different name",
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "metadata.name",
				})
			}
		}

		return findings
	},
}
