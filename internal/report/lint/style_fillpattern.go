package lint

import (
	"context"
	"encoding/json"
)

// styleFillpatternDeprecated notes the old name of the hatched fill pattern.
//
// The schema accepts 'fc' so that existing styles keep validating, and schema
// validation cannot attach a note to one enum value. In time and structure
// charts, template engines before v1.0.0-next.28 know only 'fc', so the message
// names the engine that 'hatched' needs.
var styleFillpatternDeprecated = Rule{
	ID:   "style-fillpattern-deprecated",
	Name: "Fill Pattern 'fc' Deprecated",
	Description: "The fill pattern 'fc' in a ComponentStyle is the deprecated name of 'hatched' " +
		"(template engine v1.0.0-next.28 or later).",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			if doc.Kind != "ComponentStyle" {
				continue
			}
			var payload struct {
				Spec struct {
					Content any `json:"content"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation reports malformed documents.
			}
			content := payload.Spec.Content
			// Content written as a JSON string has no key positions, so it gets
			// one finding that points at the string.
			text, isString := content.(string)
			if isString {
				if err := json.Unmarshal([]byte(text), &content); err != nil {
					continue
				}
			}
			reported := false
			walkNodes(content, "spec.content", func(node map[string]any, path string) {
				if reported || stringField(node, "fillpattern") != "fc" {
					return
				}
				path = joinLintPath(path, "fillpattern")
				if isString {
					path, reported = "spec.content", true
				}
				findings = append(findings, Finding{
					RuleID:   "style-fillpattern-deprecated",
					Message:  "fillpattern 'fc' is deprecated; write 'hatched' (template engine v1.0.0-next.28 or later)",
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     path,
					Severity: "info",
				})
			})
		}
		return findings
	},
}
