package lint

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
	return FindingData{
		RuleID:  f.RuleID,
		Message: f.Message,
		File:    f.File,
		DocIdx:  f.DocIdx,
		Path:    f.Path,
		Line:    f.Line,
		Column:  f.Column,
	}
}
