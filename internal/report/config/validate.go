package config

import (
	"fmt"
	"strings"
)

// KindProvider provides additional kind names from plugins.
// This interface avoids an import cycle between config/ and plugin/.
type KindProvider interface {
	PluginKindNames() []string
	GetKind(kindName string) (KindInfo, bool)
}

// KindInfo describes a plugin-registered kind. Mirrors plugin.KindRegistration
// without importing the plugin package.
type KindInfo struct {
	KindName       string
	Category       int // 0=DataSource, 1=Component, 2=Config, 3=Artifact
	DataSourceType string
	PluginName     string
}

// KindCategoryDataSource is the category value for DataSource kinds.
const KindCategoryDataSource = 0

var builtinUniqueNameKinds = map[string]struct{}{
	"DataSource":           {},
	"DataSet":              {},
	"ConnectionSecret":     {},
	"Asset":                {},
	"LayoutPage":           {},
	"LayoutCard":           {},
	"ChartStructure":       {},
	"ChartTime":            {},
	"ChartScatter":         {},
	"ChartBubble":          {},
	"ChartBullet":          {},
	"Table":                {},
	"Text":                 {},
	"SigningProfile":       {},
	"ComponentStyle":       {},
	"RuleSet":              {},
	"Internationalization": {},
	"ScalingGroup":         {},
}

// UniqueNameKinds returns the set of kinds that require unique names.
// It merges built-in kinds with any kinds registered by plugins.
func UniqueNameKinds(provider KindProvider) map[string]struct{} {
	kinds := make(map[string]struct{}, len(builtinUniqueNameKinds)+10)
	for k, v := range builtinUniqueNameKinds {
		kinds[k] = v
	}
	if provider != nil {
		for _, name := range provider.PluginKindNames() {
			kinds[name] = struct{}{}
		}
	}
	return kinds
}

// IsPluginKind returns true if the kind is registered by a plugin.
func IsPluginKind(kind string, provider KindProvider) bool {
	if provider == nil {
		return false
	}
	_, ok := provider.GetKind(kind)
	return ok
}

// IsDataSourceKind returns true if the kind represents a DataSource.
func IsDataSourceKind(kind string, provider KindProvider) bool {
	if kind == "DataSource" {
		return true
	}
	if provider != nil {
		if info, ok := provider.GetKind(kind); ok {
			return info.Category == KindCategoryDataSource
		}
	}
	return strings.HasSuffix(kind, "DataSource")
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

// ValidateArtefactNames checks that all included documents for a specific artifact
// have unique names per kind. This should be called after constraint filtering.
// The provider parameter is optional (nil uses built-in kinds only).
// Returns an error if duplicate names are found within the same kind.
func ValidateArtefactNames(artefactName string, docs []Document, provider KindProvider) error {
	// Group by kind, then check name uniqueness within each kind
	byKind := make(map[string]map[string]Document)
	tracked := UniqueNameKinds(provider)

	for _, doc := range docs {
		if _, ok := tracked[doc.Kind]; !ok {
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

// ReservedLiveRoutePaths are serving paths claimed by PWA support. Hosting
// infrastructure serves them uncredentialed, so a tenant data route at one of
// these paths would leak; they are rejected even when spec.pwa is unset.
var ReservedLiveRoutePaths = map[string]struct{}{
	"/manifest.webmanifest": {},
	"/sw.js":                {},
}

// AssetInfo describes an Asset document for LiveReportArtefact validation.
type AssetInfo struct {
	// Type is the Asset's spec.type.
	Type string
	// HasLocalPath reports whether the Asset's spec.source.localPath is set.
	HasLocalPath bool
}

// ValidateLiveArtefact validates a LiveReportArtefact spec.
// It checks:
//   - Root route "/" is present
//   - All route paths are unique, valid, and not reserved for PWA serving
//   - Each route has either artifact or layoutPages (not both, not neither)
//   - All referenced artifacts and layoutPages exist
//   - PWA icons reference existing Asset documents of spec.type "image"
//     backed by a source.localPath
//
// assets maps Asset document names to their validation-relevant fields.
func ValidateLiveArtefact(live LiveArtefact, artifacts []Artifact, layoutPageNames map[string]struct{}, assets map[string]AssetInfo) error {
	spec := live.Spec

	// Check for mandatory root route
	if _, ok := spec.Routes["/"]; !ok {
		return fmt.Errorf("LiveReportArtefact %q: missing mandatory root route \"/\"", live.Document.Name)
	}

	// Build set of valid artifact names
	artefactNames := make(map[string]struct{}, len(artifacts))
	for _, a := range artifacts {
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
		if _, reserved := ReservedLiveRoutePaths[path]; reserved {
			return fmt.Errorf("LiveReportArtefact %q: route path %q is reserved for PWA serving", live.Document.Name, path)
		}

		// Check that exactly one of artefact or layoutPages is set
		hasArtefact := route.Artifact != ""
		hasLayoutPages := len(route.LayoutPages) > 0

		if hasArtefact && hasLayoutPages {
			return fmt.Errorf("LiveReportArtefact %q: route %q has both artefact and layoutPages; only one is allowed", live.Document.Name, path)
		}
		if !hasArtefact && !hasLayoutPages {
			return fmt.Errorf("LiveReportArtefact %q: route %q must have either artefact or layoutPages", live.Document.Name, path)
		}

		// Validate referenced artifact exists
		if hasArtefact {
			if _, ok := artefactNames[route.Artifact]; !ok {
				return fmt.Errorf("LiveReportArtefact %q: route %q references unknown ReportArtefact %q", live.Document.Name, path, route.Artifact)
			}
		}

		// Validate referenced layoutPages exist
		if hasLayoutPages && layoutPageNames != nil {
			for _, lpRef := range route.LayoutPages {
				if lpRef.Page == "" {
					return fmt.Errorf("LiveReportArtefact %q: route %q has empty layoutPages entry", live.Document.Name, path)
				}
				// Skip glob patterns - they'll be validated at runtime
				if lpRef.IsGlob() {
					continue
				}
				if _, ok := layoutPageNames[lpRef.Page]; !ok {
					return fmt.Errorf("LiveReportArtefact %q: route %q references unknown LayoutPage %q", live.Document.Name, path, lpRef.Page)
				}
			}
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

	// Validate PWA icon assets. Icons are served at the relative path
	// assets/files/<name>, so they must be image assets backed by a local file.
	if spec.PWA != nil {
		for i, icon := range spec.PWA.Icons {
			info, ok := assets[icon.Asset]
			if !ok {
				return fmt.Errorf("LiveReportArtefact %q: pwa icon[%d] references unknown Asset %q", live.Document.Name, i, icon.Asset)
			}
			if info.Type != "image" {
				return fmt.Errorf("LiveReportArtefact %q: pwa icon[%d] Asset %q has spec.type %q; must be \"image\"", live.Document.Name, i, icon.Asset, info.Type)
			}
			if !info.HasLocalPath {
				return fmt.Errorf("LiveReportArtefact %q: pwa icon[%d] Asset %q must use a source.localPath (PWA icons are served as local files)", live.Document.Name, i, icon.Asset)
			}
		}
	}

	return nil
}

// ValidLayoutPageParamTypes lists valid types for LayoutPage params.
var ValidLayoutPageParamTypes = map[string]struct{}{
	"":          {}, // default to string
	"string":    {},
	"number":    {},
	"boolean":   {},
	"select":    {},
	"date":      {},
	"date_time": {},
}

// ValidateLayoutPageParams validates the parameter definitions in a param-capable
// document (LayoutPage or a referenceable component kind).
// Returns a list of warnings and an error if validation fails.
func ValidateLayoutPageParams(doc Document) (warnings []string, err error) {
	if _, ok := ParamCapableKinds[doc.Kind]; !ok {
		return nil, nil
	}

	seenNames := make(map[string]struct{})
	for i, param := range doc.Params {
		// Name is required
		if param.Name == "" {
			return warnings, fmt.Errorf("%s %q: param[%d] missing required 'name' field", doc.Kind, doc.Name, i)
		}

		// Check for duplicate names
		if _, seen := seenNames[param.Name]; seen {
			return warnings, fmt.Errorf("%s %q: duplicate param name %q", doc.Kind, doc.Name, param.Name)
		}
		seenNames[param.Name] = struct{}{}

		// Validate type
		if _, valid := ValidLayoutPageParamTypes[param.Type]; !valid {
			return warnings, fmt.Errorf("%s %q: param %q has invalid type %q (valid: string, number, boolean, select, date, date_time)", doc.Kind, doc.Name, param.Name, param.Type)
		}

		// Validate select type has options
		if param.Type == "select" && (param.Options == nil || len(param.Options.Items) == 0) {
			return warnings, fmt.Errorf("%s %q: param %q of type 'select' requires options.items", doc.Kind, doc.Name, param.Name)
		}

		// Validate number type constraints
		if param.Type == "number" && param.Options != nil {
			if param.Options.Min != nil && param.Options.Max != nil && *param.Options.Min > *param.Options.Max {
				return warnings, fmt.Errorf("%s %q: param %q has min > max", doc.Kind, doc.Name, param.Name)
			}
		}
	}

	return warnings, nil
}

// ValidateLayoutPageRefParams validates parameter values against parameter definitions.
// Returns warnings for unknown params (params passed but not defined) and errors for:
// - Missing required params
// - Invalid param values (type mismatch, out of range, etc.)
//
// The pageDocs map should contain all LayoutPage documents keyed by name.
func ValidateLayoutPageRefParams(ref LayoutPageRef, pageDocs map[string]Document) (warnings []string, err error) {
	pageDoc, found := pageDocs[ref.Page]
	if !found {
		// Page not found - this will be caught elsewhere
		return nil, nil
	}

	return ValidateRefParams("LayoutPage", ref.Page, ref.Params, pageDoc.Params)
}

// ValidateRefParams validates parameter values passed by a reference against the
// target document's parameter definitions.
// Returns warnings for unknown params (params passed but not defined) and errors for:
// - Missing required params
// - Invalid param values (type mismatch, out of range, etc.)
func ValidateRefParams(targetKind, targetName string, refParams map[string]string, declared []LayoutPageParamSpec) (warnings []string, err error) {
	subject := fmt.Sprintf("%s %q", targetKind, targetName)

	// Build param definitions map
	paramDefs := make(map[string]LayoutPageParamSpec)
	for _, p := range declared {
		paramDefs[p.Name] = p
	}

	// Check for unknown params (warn but don't error)
	for paramName := range refParams {
		if _, defined := paramDefs[paramName]; !defined {
			warnings = append(warnings, fmt.Sprintf("%s: unknown param %q passed (not defined in metadata.params)", subject, paramName))
		}
	}

	// Check required params are provided
	for _, paramDef := range declared {
		_, provided := refParams[paramDef.Name]
		hasDefault := paramDef.Default != nil

		if paramDef.Required && !provided && !hasDefault {
			return warnings, fmt.Errorf("%s: missing required param %q", subject, paramDef.Name)
		}
	}

	// Validate param values
	for paramName, paramValue := range refParams {
		paramDef, defined := paramDefs[paramName]
		if !defined {
			continue // Already warned about unknown params
		}

		if err := validateParamValue(subject, paramName, paramValue, paramDef); err != nil {
			return warnings, err
		}
	}

	return warnings, nil
}

// validateParamValue validates a single param value against its definition.
// The subject identifies the target document in messages, e.g. `LayoutPage "sales"`.
func validateParamValue(subject, paramName, value string, def LayoutPageParamSpec) error {
	switch def.Type {
	case "number":
		// Value might contain ${VAR} which can't be validated statically
		if containsVarReference(value) {
			return nil
		}
		// Try to parse as number
		var num float64
		if _, err := fmt.Sscanf(value, "%f", &num); err != nil {
			return fmt.Errorf("%s: param %q value %q is not a valid number", subject, paramName, value)
		}
		// Check range constraints
		if def.Options != nil {
			if def.Options.Min != nil && num < *def.Options.Min {
				return fmt.Errorf("%s: param %q value %v is below minimum %v", subject, paramName, num, *def.Options.Min)
			}
			if def.Options.Max != nil && num > *def.Options.Max {
				return fmt.Errorf("%s: param %q value %v is above maximum %v", subject, paramName, num, *def.Options.Max)
			}
		}

	case "boolean":
		if containsVarReference(value) {
			return nil
		}
		if value != "true" && value != "false" {
			return fmt.Errorf("%s: param %q value %q is not a valid boolean (must be 'true' or 'false')", subject, paramName, value)
		}

	case "select":
		if containsVarReference(value) {
			return nil
		}
		if def.Options == nil || len(def.Options.Items) == 0 {
			return nil // No items to validate against
		}
		valid := false
		for _, item := range def.Options.Items {
			if item.Value == value {
				valid = true
				break
			}
		}
		if !valid {
			var validValues []string
			for _, item := range def.Options.Items {
				validValues = append(validValues, item.Value)
			}
			return fmt.Errorf("%s: param %q value %q is not a valid option (valid: %v)", subject, paramName, value, validValues)
		}
	}

	return nil
}

// containsVarReference checks if a string contains ${VAR} syntax.
func containsVarReference(s string) bool {
	return len(s) >= 4 && (s[0] == '$' && s[1] == '{' || containsSubstr(s, "${"))
}

// containsSubstr is a simple substring check.
func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
