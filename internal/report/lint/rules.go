package lint

import (
	"context"
	"encoding/json"
	"strings"

	"bino.bi/bino/internal/report/spec"
)

// DefaultRules returns the default set of lint rules.
// Add new rules here to have them run automatically during builds.
//
// Each rule should have a unique ID following the pattern "category-description"
// (e.g., "naming-lowercase", "reference-undefined-dataset").
func DefaultRules() []Rule {
	return []Rule{
		reportArtefactRequired,
		artefactLayoutPageRequired,
		textContentRequired,
		datasetRequired,
		pageLayoutSlotsUsed,
		cardLayoutSlotsUsed,
	}
}

// Constants for page format matching (mirrors render package).
const defaultLayoutPageFormat = "xga"

// reportArtefactRequired ensures at least one ReportArtefact document exists.
var reportArtefactRequired = Rule{
	ID:          "report-artefact-required",
	Name:        "Report Artefact Required",
	Description: "At least one ReportArtefact document must be defined.",
	Check: func(_ context.Context, docs []Document) []Finding {
		for _, doc := range docs {
			if doc.Kind == "ReportArtefact" {
				return nil // Found at least one
			}
		}
		// No ReportArtefact found - create a finding
		// Use the first document's file for location, or empty if no docs
		file := ""
		if len(docs) > 0 {
			file = docs[0].File
		}
		return []Finding{{
			RuleID:  "report-artefact-required",
			Message: "no ReportArtefact document found; at least one is required to build a report",
			File:    file,
		}}
	},
}

// artefactLayoutPageRequired ensures each ReportArtefact has at least one
// matching LayoutPage after applying constraints and format filtering.
// A LayoutPage matches if:
// 1. Its constraints pass for either build OR preview mode (smart: no false-positive for mode-specific pages)
// 2. Its pageFormat matches the artefact's format (or both default to "xga")
var artefactLayoutPageRequired = Rule{
	ID:          "artefact-layoutpage-required",
	Name:        "Artefact LayoutPage Required",
	Description: "Each ReportArtefact must have at least one LayoutPage that matches its constraints and format.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		// Collect artefacts and layout pages
		var artefacts []Document
		var layoutPages []Document
		for _, doc := range docs {
			switch doc.Kind {
			case "ReportArtefact":
				artefacts = append(artefacts, doc)
			case "LayoutPage":
				layoutPages = append(layoutPages, doc)
			}
		}

		// If no artefacts, report-artefact-required rule handles it
		if len(artefacts) == 0 {
			return nil
		}

		// Check each artefact
		for _, artefact := range artefacts {
			artefactFormat := getArtefactFormat(artefact.Raw)
			matchingPages := 0

			for _, page := range layoutPages {
				// Check if page matches artefact in at least one mode (build or preview)
				if pageMatchesArtefact(page, artefact, artefactFormat) {
					matchingPages++
				}
			}

			if matchingPages == 0 {
				findings = append(findings, Finding{
					RuleID:  "artefact-layoutpage-required",
					Message: "no LayoutPage matches this artefact after applying constraints and format filtering",
					File:    artefact.File,
					DocIdx:  artefact.Position,
					Path:    "metadata.name",
				})
			}
		}

		return findings
	},
}

// textContentRequired ensures Text components have a non-empty value.
var textContentRequired = Rule{
	ID:          "text-content-required",
	Name:        "Text Content Required",
	Description: "Text components must have a non-empty 'value' field with content.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		for _, doc := range docs {
			if doc.Kind != "Text" {
				continue
			}

			var payload struct {
				Spec struct {
					Value string `json:"value"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation would catch malformed JSON
			}

			if strings.TrimSpace(payload.Spec.Value) == "" {
				findings = append(findings, Finding{
					RuleID:  "text-content-required",
					Message: "Text component has no content; 'spec.value' is empty or missing",
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.value",
				})
			}
		}

		return findings
	},
}

// datasetRequired ensures Table and Chart* components have a dataset binding.
var datasetRequired = Rule{
	ID:          "dataset-required",
	Name:        "Dataset Required",
	Description: "Table, ChartStructure, and ChartTime components must have a 'dataset' binding.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		kindsRequiringDataset := map[string]bool{
			"Table":          true,
			"ChartStructure": true,
			"ChartTime":      true,
		}

		for _, doc := range docs {
			if !kindsRequiringDataset[doc.Kind] {
				continue
			}

			var payload struct {
				Spec struct {
					Dataset spec.DatasetList `json:"dataset"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation would catch malformed JSON
			}

			if payload.Spec.Dataset.Empty() {
				findings = append(findings, Finding{
					RuleID:  "dataset-required",
					Message: doc.Kind + " component requires a 'spec.dataset' binding to display data",
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.dataset",
				})
			}
		}

		return findings
	},
}

// pageMatchesArtefact checks if a LayoutPage matches an artefact.
// It evaluates constraints for BOTH build and preview modes (matching if either passes)
// and checks format compatibility.
func pageMatchesArtefact(page, artefact Document, artefactFormat string) bool {
	// Build constraint context from artefact
	specMap, err := spec.SpecToMap(artefact.Raw)
	if err != nil {
		return false
	}

	// Check if constraints match in either build or preview mode
	constraintMatch := false
	for _, mode := range []spec.Mode{spec.ModeBuild, spec.ModePreview} {
		ctx := &spec.ConstraintContext{
			Labels: artefact.Labels,
			Spec:   specMap,
			Mode:   mode,
		}

		// If page has no constraints, it always matches
		if len(page.Constraints) == 0 {
			constraintMatch = true
			break
		}

		match, err := spec.EvaluateParsedConstraints(page.Constraints, ctx)
		if err == nil && match {
			constraintMatch = true
			break
		}
	}

	if !constraintMatch {
		return false
	}

	// Check format compatibility
	pageFormat := getPageFormat(page.Raw)
	return layoutPageMatchesFormat(pageFormat, artefactFormat)
}

// getArtefactFormat extracts the format from a ReportArtefact, defaulting to "xga".
func getArtefactFormat(raw json.RawMessage) string {
	var payload struct {
		Spec struct {
			Format string `json:"format"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultLayoutPageFormat
	}
	format := strings.TrimSpace(payload.Spec.Format)
	if format == "" {
		return defaultLayoutPageFormat
	}
	return format
}

// getPageFormat extracts the pageFormat from a LayoutPage, defaulting to "xga".
func getPageFormat(raw json.RawMessage) string {
	var payload struct {
		Spec struct {
			PageFormat string `json:"pageFormat"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultLayoutPageFormat
	}
	format := strings.TrimSpace(payload.Spec.PageFormat)
	if format == "" {
		return defaultLayoutPageFormat
	}
	return format
}

// layoutPageMatchesFormat checks if a page format matches the target format.
// Mirrors the logic from render/components.go.
func layoutPageMatchesFormat(pageFormat, targetFormat string) bool {
	if strings.TrimSpace(targetFormat) == "" {
		return true
	}
	format := strings.TrimSpace(pageFormat)
	if format == "" {
		format = defaultLayoutPageFormat
	}
	return strings.EqualFold(format, targetFormat)
}
