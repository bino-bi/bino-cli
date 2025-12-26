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

// ValidateLiveArtefact validates a LiveReportArtefact spec.
// It checks:
//   - Root route "/" is present
//   - All route paths are unique and valid
//   - All referenced artefacts exist
func ValidateLiveArtefact(live LiveArtefact, artefacts []Artefact) error {
	spec := live.Spec

	// Check for mandatory root route
	if _, ok := spec.Routes["/"]; !ok {
		return fmt.Errorf("LiveReportArtefact %q: missing mandatory root route \"/\"", live.Document.Name)
	}

	// Build set of valid artefact names
	artefactNames := make(map[string]struct{}, len(artefacts))
	for _, a := range artefacts {
		artefactNames[a.Document.Name] = struct{}{}
	}

	// Validate each route
	for path, route := range spec.Routes {
		// Validate route path format
		if path == "" {
			return fmt.Errorf("LiveReportArtefact %q: empty route path", live.Document.Name)
		}
		if path[0] != '/' {
			return fmt.Errorf("LiveReportArtefact %q: route path %q must start with \"/\"", live.Document.Name, path)
		}

		// Validate referenced artefact exists
		if route.Artefact == "" {
			return fmt.Errorf("LiveReportArtefact %q: route %q has empty artefact reference", live.Document.Name, path)
		}
		if _, ok := artefactNames[route.Artefact]; !ok {
			return fmt.Errorf("LiveReportArtefact %q: route %q references unknown ReportArtefact %q", live.Document.Name, path, route.Artefact)
		}

		// Validate query param names are unique within this route
		paramNames := make(map[string]struct{}, len(route.QueryParams))
		for _, p := range route.QueryParams {
			if p.Name == "" {
				return fmt.Errorf("LiveReportArtefact %q: route %q has query param with empty name", live.Document.Name, path)
			}
			if _, ok := paramNames[p.Name]; ok {
				return fmt.Errorf("LiveReportArtefact %q: route %q has duplicate query param name %q", live.Document.Name, path, p.Name)
			}
			paramNames[p.Name] = struct{}{}
		}
	}

	return nil
}
