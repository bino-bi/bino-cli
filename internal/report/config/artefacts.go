package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Artefact captures a validated ReportArtefact manifest.
type Artefact struct {
	Document Document
	Spec     ReportArtefactSpec
	Labels   map[string]string // metadata.labels for constraint context
	Warnings []string
}

const (
	DefaultArtefactFormat      = "xga"
	DefaultArtefactOrientation = "landscape"
	DefaultArtefactLanguage    = "de"
)

// ReportArtefactSpec mirrors the ReportArtefact manifest spec section.
type ReportArtefactSpec struct {
	Format         string   `json:"format"`
	Orientation    string   `json:"orientation"`
	Language       string   `json:"language"`
	Filename       string   `json:"filename"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Subject        string   `json:"subject"`
	Author         string   `json:"author"`
	Keywords       []string `json:"keywords"`
	SigningProfile string   `json:"signingProfile,omitempty"`
}

// ArtefactByName filters and orders ReportArtefact manifests.
type ArtefactByName []Artefact

func (a ArtefactByName) Len() int           { return len(a) }
func (a ArtefactByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ArtefactByName) Less(i, j int) bool { return a[i].Document.Name < a[j].Document.Name }

// CollectArtefacts inspects the provided documents for ReportArtefacts and ensures
// metadata.name uniqueness.
func CollectArtefacts(docs []Document) ([]Artefact, error) {
	artefacts := make([]Artefact, 0, len(docs))
	seen := make(map[string]struct{})
	for _, doc := range docs {
		if doc.Kind != "ReportArtefact" {
			continue
		}
		var payload struct {
			Spec ReportArtefactSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse ReportArtefact %s: %w", doc.Name, err)
		}
		if doc.Name == "" {
			return nil, fmt.Errorf("report artefact missing metadata.name")
		}
		if _, ok := seen[doc.Name]; ok {
			return nil, fmt.Errorf("multiple ReportArtefact documents share metadata.name %q", doc.Name)
		}
		seen[doc.Name] = struct{}{}
		warnings := applyReportArtefactDefaults(doc.Name, &payload.Spec)
		artefacts = append(artefacts, Artefact{Document: doc, Spec: payload.Spec, Labels: doc.Labels, Warnings: warnings})
	}
	sort.Sort(ArtefactByName(artefacts))
	return artefacts, nil
}

func applyReportArtefactDefaults(name string, spec *ReportArtefactSpec) []string {
	var warnings []string
	if spec == nil {
		return nil
	}
	if strings.TrimSpace(spec.Format) == "" {
		spec.Format = DefaultArtefactFormat
		warnings = append(warnings, fmt.Sprintf("ReportArtefact %s: spec.format not set; defaulting to %s", name, DefaultArtefactFormat))
	}
	if strings.TrimSpace(spec.Orientation) == "" {
		spec.Orientation = DefaultArtefactOrientation
		warnings = append(warnings, fmt.Sprintf("ReportArtefact %s: spec.orientation not set; defaulting to %s", name, DefaultArtefactOrientation))
	}
	if strings.TrimSpace(spec.Language) == "" {
		spec.Language = DefaultArtefactLanguage
		warnings = append(warnings, fmt.Sprintf("ReportArtefact %s: spec.language not set; defaulting to %s", name, DefaultArtefactLanguage))
	}
	return warnings
}

// LiveArtefact captures a validated LiveReportArtefact manifest for production serving.
type LiveArtefact struct {
	Document Document
	Spec     LiveReportArtefactSpec
	Warnings []string
}

// LiveReportArtefactSpec mirrors the LiveReportArtefact manifest spec section.
type LiveReportArtefactSpec struct {
	Title       string                   `json:"title"`
	Description string                   `json:"description,omitempty"`
	Routes      map[string]LiveRouteSpec `json:"routes"`
}

// LiveRouteSpec defines a route mapping to a ReportArtefact or LayoutPages.
// Either Artefact or LayoutPages must be set, but not both.
type LiveRouteSpec struct {
	Artefact    string               `json:"artefact,omitempty"`
	LayoutPages StringOrSlice        `json:"layoutPages,omitempty"` // one or more LayoutPage names
	Title       string               `json:"title,omitempty"`
	QueryParams []LiveQueryParamSpec `json:"queryParams,omitempty"`
}

// StringOrSlice is a type that can unmarshal from either a string or an array of strings.
type StringOrSlice []string

// UnmarshalJSON implements json.Unmarshaler for StringOrSlice.
func (s *StringOrSlice) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a single string first
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}

	// Try to unmarshal as an array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*s = arr
	return nil
}

// MarshalJSON implements json.Marshaler for StringOrSlice.
// Returns a single string if only one element, otherwise an array.
func (s StringOrSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}

// LiveQueryParamSpec defines an allowed query parameter for live serving.
type LiveQueryParamSpec struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type,omitempty"`     // string, number, number_range, select, date, date_time (default: string)
	Default     *string                `json:"default,omitempty"`  // nil means required (HTTP 400 if missing), unless optional is true
	Optional    bool                   `json:"optional,omitempty"` // if true, parameter is optional even without default
	Description string                 `json:"description,omitempty"`
	Options     *LiveQueryParamOptions `json:"options,omitempty"` // options for select, number, number_range types
}

// LiveQueryParamOptions defines options for select, number, and number_range type parameters.
type LiveQueryParamOptions struct {
	Items       []LiveQueryParamOptionItem `json:"items,omitempty"`       // static options for select
	Dataset     string                     `json:"dataset,omitempty"`     // dataset name for dynamic options
	ValueColumn string                     `json:"valueColumn,omitempty"` // column to use as value
	LabelColumn string                     `json:"labelColumn,omitempty"` // column to use as label (defaults to valueColumn)
	Min         *float64                   `json:"min,omitempty"`         // min for number/number_range
	Max         *float64                   `json:"max,omitempty"`         // max for number/number_range
	Step        *float64                   `json:"step,omitempty"`        // step for number/number_range
}

// LiveQueryParamOptionItem defines a single option for select type parameters.
type LiveQueryParamOptionItem struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"` // defaults to value if empty
}

// LiveArtefactByName filters and orders LiveReportArtefact manifests.
type LiveArtefactByName []LiveArtefact

func (a LiveArtefactByName) Len() int           { return len(a) }
func (a LiveArtefactByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a LiveArtefactByName) Less(i, j int) bool { return a[i].Document.Name < a[j].Document.Name }

// CollectLiveArtefacts inspects the provided documents for LiveReportArtefacts and ensures
// metadata.name uniqueness.
func CollectLiveArtefacts(docs []Document) ([]LiveArtefact, error) {
	artefacts := make([]LiveArtefact, 0, len(docs))
	seen := make(map[string]struct{})
	for _, doc := range docs {
		if doc.Kind != "LiveReportArtefact" {
			continue
		}
		var payload struct {
			Spec LiveReportArtefactSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse LiveReportArtefact %s: %w", doc.Name, err)
		}
		if doc.Name == "" {
			return nil, fmt.Errorf("live report artefact missing metadata.name")
		}
		if _, ok := seen[doc.Name]; ok {
			return nil, fmt.Errorf("multiple LiveReportArtefact documents share metadata.name %q", doc.Name)
		}
		seen[doc.Name] = struct{}{}
		var warnings []string
		artefacts = append(artefacts, LiveArtefact{Document: doc, Spec: payload.Spec, Warnings: warnings})
	}
	sort.Sort(LiveArtefactByName(artefacts))
	return artefacts, nil
}

// FindLiveArtefact finds a LiveReportArtefact by name.
// Returns nil if not found.
func FindLiveArtefact(artefacts []LiveArtefact, name string) *LiveArtefact {
	for i := range artefacts {
		if artefacts[i].Document.Name == name {
			return &artefacts[i]
		}
	}
	return nil
}

// GetQueryParamDefaults returns a map of query param names to their default values.
// Parameters without defaults are not included in the map.
func (r *LiveRouteSpec) GetQueryParamDefaults() map[string]string {
	defaults := make(map[string]string)
	for _, p := range r.QueryParams {
		if p.Default != nil {
			defaults[p.Name] = *p.Default
		}
	}
	return defaults
}

// GetRequiredQueryParams returns a list of query param names that have no default and are not optional.
func (r *LiveRouteSpec) GetRequiredQueryParams() []string {
	var required []string
	for _, p := range r.QueryParams {
		if p.Default == nil && !p.Optional {
			required = append(required, p.Name)
		}
	}
	return required
}

// ScreenshotArtefact captures a validated ScreenshotArtefact manifest.
type ScreenshotArtefact struct {
	Document Document
	Spec     ScreenshotArtefactSpec
	Labels   map[string]string
	Warnings []string
}

const (
	DefaultScreenshotFilenamePattern = "ref"
	DefaultScreenshotImageFormat     = "png"
)

// ScreenshotArtefactSpec mirrors the ScreenshotArtefact manifest spec section.
type ScreenshotArtefactSpec struct {
	Refs            []ScreenshotRef `json:"refs"`
	LayoutPages     StringOrSlice   `json:"layoutPages"`               // one or more LayoutPage names to render
	Format          string          `json:"format"`
	Orientation     string          `json:"orientation"`
	Language        string          `json:"language"`
	FilenamePrefix  string          `json:"filenamePrefix"`
	FilenamePattern string          `json:"filenamePattern,omitempty"` // "index" or "ref"
	ImageFormat     string          `json:"imageFormat,omitempty"`     // "png" or "jpeg"
	Quality         *int            `json:"quality,omitempty"`         // JPEG quality 1-100
	OmitBackground  bool            `json:"omitBackground,omitempty"`
	Scale           string          `json:"scale,omitempty"` // "css" or "device"
}

// ScreenshotRef identifies a component to capture a screenshot of.
type ScreenshotRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// ScreenshotArtefactByName filters and orders ScreenshotArtefact manifests.
type ScreenshotArtefactByName []ScreenshotArtefact

func (a ScreenshotArtefactByName) Len() int           { return len(a) }
func (a ScreenshotArtefactByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ScreenshotArtefactByName) Less(i, j int) bool { return a[i].Document.Name < a[j].Document.Name }

// CollectScreenshotArtefacts inspects the provided documents for ScreenshotArtefacts and ensures
// metadata.name uniqueness.
func CollectScreenshotArtefacts(docs []Document) ([]ScreenshotArtefact, error) {
	artefacts := make([]ScreenshotArtefact, 0, len(docs))
	seen := make(map[string]struct{})
	for _, doc := range docs {
		if doc.Kind != "ScreenshotArtefact" {
			continue
		}
		var payload struct {
			Spec ScreenshotArtefactSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse ScreenshotArtefact %s: %w", doc.Name, err)
		}
		if doc.Name == "" {
			return nil, fmt.Errorf("screenshot artefact missing metadata.name")
		}
		if _, ok := seen[doc.Name]; ok {
			return nil, fmt.Errorf("multiple ScreenshotArtefact documents share metadata.name %q", doc.Name)
		}
		seen[doc.Name] = struct{}{}
		warnings := applyScreenshotArtefactDefaults(doc.Name, &payload.Spec)
		artefacts = append(artefacts, ScreenshotArtefact{Document: doc, Spec: payload.Spec, Labels: doc.Labels, Warnings: warnings})
	}
	sort.Sort(ScreenshotArtefactByName(artefacts))
	return artefacts, nil
}

func applyScreenshotArtefactDefaults(name string, spec *ScreenshotArtefactSpec) []string {
	var warnings []string
	if spec == nil {
		return nil
	}
	if strings.TrimSpace(spec.Format) == "" {
		spec.Format = DefaultArtefactFormat
		warnings = append(warnings, fmt.Sprintf("ScreenshotArtefact %s: spec.format not set; defaulting to %s", name, DefaultArtefactFormat))
	}
	if strings.TrimSpace(spec.Orientation) == "" {
		spec.Orientation = DefaultArtefactOrientation
		warnings = append(warnings, fmt.Sprintf("ScreenshotArtefact %s: spec.orientation not set; defaulting to %s", name, DefaultArtefactOrientation))
	}
	if strings.TrimSpace(spec.Language) == "" {
		spec.Language = DefaultArtefactLanguage
		warnings = append(warnings, fmt.Sprintf("ScreenshotArtefact %s: spec.language not set; defaulting to %s", name, DefaultArtefactLanguage))
	}
	if strings.TrimSpace(spec.FilenamePattern) == "" {
		spec.FilenamePattern = DefaultScreenshotFilenamePattern
	}
	if strings.TrimSpace(spec.ImageFormat) == "" {
		spec.ImageFormat = DefaultScreenshotImageFormat
	}
	return warnings
}
