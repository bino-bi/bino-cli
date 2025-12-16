package lint

// DefaultRules returns the default set of lint rules.
// Add new rules here to have them run automatically during builds.
//
// Each rule should have a unique ID following the pattern "category-description"
// (e.g., "naming-lowercase", "reference-undefined-dataset").
func DefaultRules() []Rule {
	return []Rule{
		// Add rules here as they are implemented.
		// Example:
		// missingDescriptionRule,
		// duplicateNameRule,
	}
}
