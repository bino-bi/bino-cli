package pathutil

import (
	"path/filepath"
	"strings"
)

// DefaultPackageIncludeDirs are the project-relative directories a package
// publishes when [package] declares no explicit include list: every canonical
// manifest folder (projectlayout.CanonicalFolders) plus the "manifests"
// fallback folder, minus reports/. Kept in sync by
// TestDefaultPackageIncludeMatchesLayout in internal/projectlayout.
func DefaultPackageIncludeDirs() []string {
	return []string{
		"components", "datasets", "datasources", "i18n", "manifests",
		"pages", "resources", "scaling", "secrets", "signing", "styles",
	}
}

// excludedPackageDirs are never part of a package, whatever [package].include
// says: mocks/ holds the sample data that makes the package previewable but is
// not shipped, reports/ holds the artefacts that render it, and .bino/ holds
// machine-managed state including installed dependencies.
var excludedPackageDirs = []string{"mocks", "reports", ".bino"}

// IncludeSet answers "is this file part of the package?" for absolute paths.
type IncludeSet struct {
	root    string
	entries []string
}

// IncludeSet resolves the include list against a project root, applying the
// default list when none is declared. Invalid entries are dropped (Validate
// reports them). Returns nil for a nil receiver.
func (p *PackageConfig) IncludeSet(projectRoot string) *IncludeSet {
	if p == nil {
		return nil
	}
	declared := p.Include
	if len(declared) == 0 {
		declared = DefaultPackageIncludeDirs()
	}
	entries := make([]string, 0, len(declared))
	for _, entry := range declared {
		if cleaned, ok := cleanProjectRelative(entry); ok {
			entries = append(entries, cleaned)
		}
	}
	return &IncludeSet{root: projectRoot, entries: entries}
}

// Contains reports whether an absolute file path is inside the include set.
// A nil set contains nothing. It is pure: no filesystem access, so a
// non-existent path is judged by its shape alone.
func (s *IncludeSet) Contains(absPath string) bool {
	if s == nil {
		return false
	}
	rel, err := filepath.Rel(s.root, filepath.Clean(absPath))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	first, _, _ := strings.Cut(rel, "/")
	for _, excluded := range excludedPackageDirs {
		if first == excluded {
			return false
		}
	}
	for _, entry := range s.entries {
		if entry == "." || rel == entry || strings.HasPrefix(rel, entry+"/") {
			return true
		}
	}
	return false
}
