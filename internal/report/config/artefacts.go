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
