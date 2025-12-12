package config

import "fmt"

var uniqueNameKinds = map[string]struct{}{
	"DataSource":           {},
	"DataSet":              {},
	"ConnectionSecret":     {},
	"Asset":                {},
	"LayoutPage":           {},
	"LayoutCard":           {},
	"ChartStructure":       {},
	"ChartTime":            {},
	"Table":                {},
	"Text":                 {},
	"SigningProfile":       {},
	"ComponentStyle":       {},
	"Internationalization": {},
}

// ValidateDocuments performs basic validation on loaded documents.
// NOTE: Name uniqueness is no longer enforced globally. Instead, names must be
// unique per ReportArtefact after applying metadata.constraints filtering.
// This validation now only checks for ReportArtefact name uniqueness (which
// is still required globally) and basic document integrity.
func ValidateDocuments(docs []Document) error {
	// ReportArtefact names must still be globally unique
	artefactNames := make(map[string]Document)
	for _, doc := range docs {
		if doc.Kind != "ReportArtefact" {
			continue
		}
		if doc.Name == "" {
			continue
		}
		if existing, found := artefactNames[doc.Name]; found {
			return fmt.Errorf(
				"%s #%d (ReportArtefact) reuses name %q already defined by %s #%d",
				doc.File,
				doc.Position,
				doc.Name,
				existing.File,
				existing.Position,
			)
		}
		artefactNames[doc.Name] = doc
	}
	return nil
}

// ValidateArtefactNames checks that all included documents for a specific artefact
// have unique names per kind. This should be called after constraint filtering.
// Returns an error if duplicate names are found within the same kind.
func ValidateArtefactNames(artefactName string, docs []Document) error {
	// Group by kind, then check name uniqueness within each kind
	byKind := make(map[string]map[string]Document)

	for _, doc := range docs {
		if _, tracked := uniqueNameKinds[doc.Kind]; !tracked {
			continue
		}
		if doc.Name == "" {
			continue
		}

		if byKind[doc.Kind] == nil {
			byKind[doc.Kind] = make(map[string]Document)
		}

		if existing, found := byKind[doc.Kind][doc.Name]; found {
			return fmt.Errorf(
				"artefact %q: duplicate %s name %q - defined in %s #%d and %s #%d (after applying constraints)",
				artefactName,
				doc.Kind,
				doc.Name,
				existing.File,
				existing.Position,
				doc.File,
				doc.Position,
			)
		}
		byKind[doc.Kind][doc.Name] = doc
	}

	return nil
}
