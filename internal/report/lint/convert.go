package lint

import "bino.bi/bino/internal/report/config"

// ToLintFindings converts lint findings to build log entries.
// Returns the rule ID, message, file, doc index, path, line, and column for each finding.
// This is a convenience function for the CLI layer.
type FindingData struct {
	RuleID  string
	Message string
	File    string
	DocIdx  int
	Path    string
	Line    int
	Column  int
}

// ToFindingData converts a Finding to FindingData for use by the buildlog package.
func (f Finding) ToFindingData() FindingData {
	return FindingData(f)
}

// DocumentsFromConfig converts loaded manifest documents into the linter's
// document representation.
func DocumentsFromConfig(docs []config.Document) []Document {
	lintDocs := make([]Document, 0, len(docs))
	for _, d := range docs {
		lintDocs = append(lintDocs, Document{
			File:        d.File,
			Position:    d.Position,
			Kind:        d.Kind,
			Name:        d.Name,
			Labels:      d.Labels,
			Constraints: d.Constraints,
			Raw:         d.Raw,
		})
	}
	return lintDocs
}
