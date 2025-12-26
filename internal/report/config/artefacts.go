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

// LiveRouteSpec defines a route mapping to a ReportArtefact.
type LiveRouteSpec struct {
	Artefact    string               `json:"artefact"`
	Title       string               `json:"title,omitempty"`
	QueryParams []LiveQueryParamSpec `json:"queryParams,omitempty"`
}

// LiveQueryParamSpec defines an allowed query parameter for live serving.
type LiveQueryParamSpec struct {
	Name        string  `json:"name"`
	Default     *string `json:"default,omitempty"` // nil means required (HTTP 400 if missing)
	Description string  `json:"description,omitempty"`
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

// GetRequiredQueryParams returns a list of query param names that have no default.
func (r *LiveRouteSpec) GetRequiredQueryParams() []string {
	var required []string
	for _, p := range r.QueryParams {
		if p.Default == nil {
			required = append(required, p.Name)
		}
	}
	return required
}
