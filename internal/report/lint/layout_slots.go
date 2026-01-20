package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"bino.bi/bino/internal/report/spec"
)

// Predefined slot counts for standard layout types.
var predefinedSlotCounts = map[string]int{
	"full":             1,
	"split-horizontal": 2,
	"split-vertical":   2,
	"2x2":              4,
	"3x3":              9,
	"4x4":              16,
	"1-over-2":         3,
	"1-over-3":         4,
	"2-over-1":         3,
	"3-over-1":         4,
}

// Supported child kinds that can be rendered (mirrors render package).
var renderableChildKinds = map[string]bool{
	"LayoutCard":     true,
	"Text":           true,
	"Table":          true,
	"ChartStructure": true,
	"ChartTime":      true,
	"ChartTree":      true,
	"Image":          true,
}

// layoutChildSpec mirrors the child structure from render/spec.go for parsing.
type layoutChildSpec struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name        string `json:"name"`
		Constraints []any  `json:"constraints"` // Supports string or object format
	} `json:"metadata"`
	Ref      string          `json:"ref,omitempty"`
	Optional bool            `json:"optional,omitempty"`
	Spec     json.RawMessage `json:"spec,omitempty"`
}

// layoutPageSpecForLint extracts layout-related fields from a LayoutPage.
type layoutPageSpecForLint struct {
	PageLayout         string            `json:"pageLayout"`
	PageCustomTemplate string            `json:"pageCustomTemplate"`
	PageFormat         string            `json:"pageFormat"`
	Children           []layoutChildSpec `json:"children"`
}

// layoutCardSpecForLint extracts layout-related fields from a LayoutCard.
type layoutCardSpecForLint struct {
	CardLayout         string            `json:"cardLayout"`
	CardCustomTemplate string            `json:"cardCustomTemplate"`
	Children           []layoutChildSpec `json:"children"`
}

// expectedSlots computes the expected number of slots for a layout.
// For predefined layouts, it uses the fixed mapping.
// For custom-template, it counts distinct area tokens in the template.
func expectedSlots(layout, customTemplate string) int {
	layout = strings.TrimSpace(strings.ToLower(layout))

	// Check predefined layouts first
	if count, ok := predefinedSlotCounts[layout]; ok {
		return count
	}

	// For custom-template, parse the template
	if layout == "custom-template" {
		return countDistinctAreaTokens(customTemplate)
	}

	// Unknown layout - default to 1 (full)
	return 1
}

// countDistinctAreaTokens parses a CSS grid-template-areas string and counts
// distinct named area tokens (excluding "." which represents empty cells).
//
// Examples:
//
//	"a a" / "b c" => 3 (a, b, c)
//	"aa aa" / "bb bb" => 2 (aa, bb)
//	"header header" / "sidebar main" / "footer footer" => 4 (header, sidebar, main, footer)
func countDistinctAreaTokens(template string) int {
	if strings.TrimSpace(template) == "" {
		return 0
	}

	// Normalize the template: remove backslashes, quotes, and newlines
	// Split by rows (newlines or escaped newlines)
	normalized := strings.ReplaceAll(template, "\\", " ")
	normalized = strings.ReplaceAll(normalized, "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\r", " ")

	// Remove quotes
	normalized = strings.ReplaceAll(normalized, "\"", " ")
	normalized = strings.ReplaceAll(normalized, "'", " ")

	// Split by whitespace and collect unique tokens
	tokenPattern := regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)
	tokens := tokenPattern.FindAllString(normalized, -1)

	unique := make(map[string]struct{})
	for _, token := range tokens {
		// Skip "." which represents empty cells in grid-template-areas
		if token != "." {
			unique[token] = struct{}{}
		}
	}

	return len(unique)
}

// pageLayoutSlotsUsed validates that LayoutPage children count matches expected slots.
var pageLayoutSlotsUsed = Rule{
	ID:          "page-layout-slots-used",
	Name:        "Page Layout Slots Used",
	Description: "LayoutPage children count must match the expected slots for its pageLayout.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		// Collect artefacts and pages
		var artefacts []Document
		pages := make(map[string]Document) // name -> doc
		cards := make(map[string]Document) // name -> doc
		components := make(map[string]Document)

		for _, doc := range docs {
			switch doc.Kind {
			case "ReportArtefact":
				artefacts = append(artefacts, doc)
			case "LayoutPage":
				pages[doc.Name] = doc
			case "LayoutCard":
				cards[doc.Name] = doc
			case "Text", "Table", "ChartStructure", "ChartTime", "ChartTree", "Image":
				components[doc.Name] = doc
			}
		}

		// If no artefacts, skip (report-artefact-required handles this)
		if len(artefacts) == 0 {
			return nil
		}

		// Track which (artefact, page) pairs we've checked to avoid duplicates
		checked := make(map[string]struct{})

		// For each artefact, check matching pages
		for _, artefact := range artefacts {
			artefactFormat := getArtefactFormat(artefact.Raw)

			for _, page := range pages {
				// Check if page matches this artefact
				if !pageMatchesArtefact(page, artefact, artefactFormat) {
					continue
				}

				// Avoid duplicate findings for same page/artefact combo
				key := fmt.Sprintf("%s:%s", artefact.Name, page.Name)
				if _, ok := checked[key]; ok {
					continue
				}
				checked[key] = struct{}{}

				// Parse page spec
				var pageSpec struct {
					Spec layoutPageSpecForLint `json:"spec"`
				}
				if err := json.Unmarshal(page.Raw, &pageSpec); err != nil {
					continue
				}

				// Get expected slots
				expected := expectedSlots(pageSpec.Spec.PageLayout, pageSpec.Spec.PageCustomTemplate)

				// Count effective children after constraints
				specMap, err := spec.SpecToMap(artefact.Raw)
				if err != nil {
					continue
				}

				// Check both build and preview modes, use the one that matches the page
				var effectiveCount int
				var childFindings []Finding

				for _, mode := range []spec.Mode{spec.ModeBuild, spec.ModePreview} {
					ctx := &spec.ConstraintContext{
						Labels: artefact.Labels,
						Spec:   specMap,
						Mode:   mode,
					}

					count, cf := countEffectiveChildren(
						pageSpec.Spec.Children,
						ctx,
						page,
						artefact.Name,
						cards,
						components,
						"spec.children",
					)

					// Use the higher count (most permissive mode)
					if count > effectiveCount {
						effectiveCount = count
						childFindings = cf
					}
				}

				// Add warnings for missing refs, etc.
				findings = append(findings, childFindings...)

				// Check slot count
				if effectiveCount != expected {
					msg := fmt.Sprintf(
						"pageLayout %q expects %d slot(s) but page has %d effective children (for artefact %q)",
						pageSpec.Spec.PageLayout, expected, effectiveCount, artefact.Name,
					)
					findings = append(findings, Finding{
						RuleID:  "page-layout-slots-used",
						Message: msg,
						File:    page.File,
						DocIdx:  page.Position,
						Path:    "spec.children",
					})
				}
			}
		}

		return findings
	},
}

// cardLayoutSlotsUsed validates that LayoutCard children count matches expected slots.
var cardLayoutSlotsUsed = Rule{
	ID:          "card-layout-slots-used",
	Name:        "Card Layout Slots Used",
	Description: "LayoutCard children count must match the expected slots for its cardLayout.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		// Collect artefacts, pages, and cards
		var artefacts []Document
		pages := make(map[string]Document)
		cards := make(map[string]Document)
		components := make(map[string]Document)

		for _, doc := range docs {
			switch doc.Kind {
			case "ReportArtefact":
				artefacts = append(artefacts, doc)
			case "LayoutPage":
				pages[doc.Name] = doc
			case "LayoutCard":
				cards[doc.Name] = doc
			case "Text", "Table", "ChartStructure", "ChartTime", "ChartTree", "Image":
				components[doc.Name] = doc
			}
		}

		// If no artefacts, skip
		if len(artefacts) == 0 {
			return nil
		}

		// Track checked (artefact, card) pairs
		checked := make(map[string]struct{})

		// For each artefact, find matching pages, then validate cards used in those pages
		for _, artefact := range artefacts {
			artefactFormat := getArtefactFormat(artefact.Raw)

			specMap, err := spec.SpecToMap(artefact.Raw)
			if err != nil {
				continue
			}

			for _, page := range pages {
				if !pageMatchesArtefact(page, artefact, artefactFormat) {
					continue
				}

				// Parse page to find card references
				var pageSpec struct {
					Spec layoutPageSpecForLint `json:"spec"`
				}
				if err := json.Unmarshal(page.Raw, &pageSpec); err != nil {
					continue
				}

				// Collect cards referenced/inlined in this page
				cardRefs := collectCardRefs(pageSpec.Spec.Children, cards)

				// Validate each card
				for _, cardDoc := range cardRefs {
					key := fmt.Sprintf("%s:%s", artefact.Name, cardDoc.Name)
					if _, ok := checked[key]; ok {
						continue
					}
					checked[key] = struct{}{}

					// Parse card spec
					var cardSpec struct {
						Spec layoutCardSpecForLint `json:"spec"`
					}
					if err := json.Unmarshal(cardDoc.Raw, &cardSpec); err != nil {
						continue
					}

					// Get expected slots
					expected := expectedSlots(cardSpec.Spec.CardLayout, cardSpec.Spec.CardCustomTemplate)

					// Count effective children
					var effectiveCount int
					var childFindings []Finding

					for _, mode := range []spec.Mode{spec.ModeBuild, spec.ModePreview} {
						ctx := &spec.ConstraintContext{
							Labels: artefact.Labels,
							Spec:   specMap,
							Mode:   mode,
						}

						count, cf := countEffectiveChildren(
							cardSpec.Spec.Children,
							ctx,
							cardDoc,
							artefact.Name,
							cards,
							components,
							"spec.children",
						)

						if count > effectiveCount {
							effectiveCount = count
							childFindings = cf
						}
					}

					findings = append(findings, childFindings...)

					if effectiveCount != expected {
						msg := fmt.Sprintf(
							"cardLayout %q expects %d slot(s) but card has %d effective children (for artefact %q)",
							cardSpec.Spec.CardLayout, expected, effectiveCount, artefact.Name,
						)
						findings = append(findings, Finding{
							RuleID:  "card-layout-slots-used",
							Message: msg,
							File:    cardDoc.File,
							DocIdx:  cardDoc.Position,
							Path:    "spec.children",
						})
					}
				}
			}
		}

		return findings
	},
}

// countEffectiveChildren counts children that would actually render after
// applying constraints and checking ref resolution.
// It returns the count and any errors/warnings for missing/invalid refs.
func countEffectiveChildren(
	children []layoutChildSpec,
	ctx *spec.ConstraintContext,
	parentDoc Document,
	artefactName string,
	cards map[string]Document,
	components map[string]Document,
	basePath string,
) (int, []Finding) {
	var count int
	var findings []Finding

	for i, child := range children {
		childPath := fmt.Sprintf("%s[%d]", basePath, i)

		// Check constraints
		if len(child.Metadata.Constraints) > 0 {
			constraints, parseErr := spec.ParseMixedConstraints(child.Metadata.Constraints)
			if parseErr != nil {
				continue // Invalid constraint, skip this child
			}
			match, err := spec.EvaluateParsedConstraints(constraints, ctx)
			if err != nil || !match {
				continue // Constraint doesn't match, skip this child
			}
		}

		// Check if child will render
		if child.Ref != "" {
			// Reference to another document
			refName := child.Ref

			// Check if it's a LayoutPage (invalid - renderer errors on this)
			if _, isPage := findDocByKindAndName("LayoutPage", refName, cards, components); isPage {
				findings = append(findings, Finding{
					RuleID:  "page-layout-slots-used",
					Message: fmt.Sprintf("child ref %q points to a LayoutPage (not allowed in children)", refName),
					File:    parentDoc.File,
					DocIdx:  parentDoc.Position,
					Path:    childPath + ".ref",
				})
				// Don't count, renderer would error
				continue
			}

			// Check if ref exists
			found := false
			if _, ok := cards[refName]; ok {
				found = true
			} else if _, ok := components[refName]; ok {
				found = true
			}

			if !found {
				if child.Optional {
					// Optional ref missing - this is OK, just don't count it
					// No finding needed for optional refs that are gracefully skipped
				} else {
					// Required ref missing - this is an error
					findings = append(findings, Finding{
						RuleID:  "missing-required-reference",
						Message: fmt.Sprintf("required reference %q not found (use optional: true to allow missing refs)", refName),
						File:    parentDoc.File,
						DocIdx:  parentDoc.Position,
						Path:    childPath + ".ref",
					})
				}
				// Don't count - missing ref doesn't consume a slot
				continue
			}

			// Valid ref
			count++
		} else if len(child.Spec) > 0 {
			// Inline spec
			if !renderableChildKinds[child.Kind] {
				findings = append(findings, Finding{
					RuleID:  "page-layout-slots-used",
					Message: fmt.Sprintf("child kind %q is not a renderable component", child.Kind),
					File:    parentDoc.File,
					DocIdx:  parentDoc.Position,
					Path:    childPath + ".kind",
				})
				continue
			}
			count++
		} else {
			// No ref and no spec - this child won't render
			findings = append(findings, Finding{
				RuleID:  "page-layout-slots-used",
				Message: "child has neither ref nor spec; slot will be empty",
				File:    parentDoc.File,
				DocIdx:  parentDoc.Position,
				Path:    childPath,
			})
		}
	}

	return count, findings
}

// findDocByKindAndName checks if a document with given kind and name exists.
func findDocByKindAndName(kind, name string, docMaps ...map[string]Document) (Document, bool) {
	for _, m := range docMaps {
		if doc, ok := m[name]; ok && doc.Kind == kind {
			return doc, true
		}
	}
	return Document{}, false
}

// collectCardRefs finds all LayoutCard documents referenced or inlined in children.
func collectCardRefs(children []layoutChildSpec, cards map[string]Document) []Document {
	var result []Document
	seen := make(map[string]struct{})

	for _, child := range children {
		if child.Kind != "LayoutCard" {
			continue
		}

		if child.Ref != "" {
			if card, ok := cards[child.Ref]; ok {
				if _, alreadySeen := seen[card.Name]; !alreadySeen {
					result = append(result, card)
					seen[card.Name] = struct{}{}
				}
			}
		}
		// For inline cards, we'd need the Document wrapper, but inline cards
		// in children don't have their own Document entry. We handle them
		// separately if needed.
	}

	// Also check standalone card documents that might be used
	for _, card := range cards {
		if _, alreadySeen := seen[card.Name]; !alreadySeen {
			result = append(result, card)
			seen[card.Name] = struct{}{}
		}
	}

	return result
}
