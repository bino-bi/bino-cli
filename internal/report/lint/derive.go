package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// reservedNamePrefixes are prefixes of DataSource/DataSet names the CLI keeps
// for itself: bino_ for session functions and macros (bino_shift), _bino_ for
// the views it creates while executing a dataset.
var reservedNamePrefixes = []string{"bino_", "_bino_"}

// datasetDeriveConflict ensures a previous-period slot is declared in either
// 'derive' or 'assert', never both.
var datasetDeriveConflict = Rule{
	ID:          "dataset-derive-conflict",
	Name:        "DataSet Derive Conflict",
	Description: "A previous-period slot may be declared in 'derive' or 'assert', not both.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "DataSet" {
				continue
			}

			var payload struct {
				Spec struct {
					Derive map[string]json.RawMessage `json:"derive"`
					Assert map[string]json.RawMessage `json:"assert"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue
			}

			var both []string
			for slot := range payload.Spec.Derive {
				if _, ok := payload.Spec.Assert[slot]; ok {
					both = append(both, slot)
				}
			}
			sort.Strings(both)
			for _, slot := range both {
				findings = append(findings, Finding{
					RuleID:  "dataset-derive-conflict",
					Message: fmt.Sprintf("slot %s is declared in both 'derive' and 'assert'; derive it or assert it, not both", slot),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.assert." + slot,
				})
			}
		}

		return findings
	},
}

// reservedNamePrefix ensures DataSource and DataSet names stay clear of the
// prefixes the CLI reserves for its own session objects.
var reservedNamePrefix = Rule{
	ID:          "reserved-name-prefix",
	Name:        "Reserved Name Prefix",
	Description: "DataSource and DataSet names must not start with 'bino_' or '_bino_'; the CLI reserves them for its session functions and views.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "DataSource" && doc.Kind != "DataSet" {
				continue
			}
			if doc.Labels["bino.bi/generated"] == "true" {
				continue
			}
			for _, prefix := range reservedNamePrefixes {
				if !strings.HasPrefix(doc.Name, prefix) {
					continue
				}
				findings = append(findings, Finding{
					RuleID:  "reserved-name-prefix",
					Message: fmt.Sprintf("%s name %q starts with the reserved prefix %q; choose another name", doc.Kind, doc.Name, prefix),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "metadata.name",
				})
				break
			}
		}

		return findings
	},
}
