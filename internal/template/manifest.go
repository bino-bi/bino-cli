package template

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const (
	// APIVersion is the only template-manifest schema version this binary parses.
	APIVersion = "bino.bi/v1alpha1"
	// KindProjectTemplate is the manifest kind for a bino project template.
	KindProjectTemplate = "ProjectTemplate"
	// manifestFile is the template manifest filename at a template's root.
	manifestFile = "bino.template.yaml"
)

// ProjectTemplate is the parsed bino.template.yaml manifest.
type ProjectTemplate struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

// Metadata carries the template name.
type Metadata struct {
	Name string `yaml:"name"`
}

// Spec is the template's declarative configuration. There is no hook or command
// surface by design — the parser rejects unknown keys (fail-closed) so a
// speculative hooks:/run: key in an untrusted manifest can never be honored.
type Spec struct {
	MinBinoVersion string   `yaml:"min-bino-version,omitempty"`
	EngineVersion  string   `yaml:"engine-version,omitempty"`
	Fields         []Field  `yaml:"fields,omitempty"`
	Verbatim       []string `yaml:"verbatim,omitempty"`
	Binary         []string `yaml:"binary,omitempty"`
}

// Field declares a substitution variable collected from the user (or --set).
type Field struct {
	Name     string `yaml:"name"`
	Prompt   string `yaml:"prompt,omitempty"`
	Default  string `yaml:"default,omitempty"`
	Required bool   `yaml:"required,omitempty"`
}

// ParseManifest parses and validates a bino.template.yaml. Unknown keys are a
// hard error (strict, fail-closed); the apiVersion and kind must match exactly.
func ParseManifest(data []byte) (*ProjectTemplate, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var t ProjectTemplate
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("parse template manifest: %w", err)
	}
	if t.APIVersion != APIVersion {
		return nil, fmt.Errorf("unsupported template apiVersion %q (this bino understands %q)", t.APIVersion, APIVersion)
	}
	if t.Kind != KindProjectTemplate {
		return nil, fmt.Errorf("unexpected template kind %q, want %q", t.Kind, KindProjectTemplate)
	}
	return &t, nil
}

// Validate checks the template against the running CLI version. A min-bino-version
// gate is skipped (not failed) when the CLI version is a dev build, so local
// development never gets blocked by a forward-looking pin.
func (t *ProjectTemplate) Validate(cliVersion string) error {
	if t.Spec.MinBinoVersion == "" {
		return nil
	}
	cv, err := semver.NewVersion(cliVersion)
	if err != nil {
		// A non-semver CLI version (e.g. a dev build) should never be blocked
		// by a forward-looking pin, so the gate is skipped, not failed.
		return nil //nolint:nilerr // intentionally lenient for non-semver CLI versions
	}
	if isDevVersion(cv) {
		return nil
	}
	constraint, err := semver.NewConstraint(t.Spec.MinBinoVersion)
	if err != nil {
		return fmt.Errorf("invalid min-bino-version %q: %w", t.Spec.MinBinoVersion, err)
	}
	if !constraint.Check(cv) {
		return fmt.Errorf("this template requires bino %s, but you are running %s", t.Spec.MinBinoVersion, cliVersion)
	}
	return nil
}

// isDevVersion reports whether a parsed version looks like a development build
// (e.g. 0.1.0-dev), whose pre-release should satisfy any min-bino-version.
func isDevVersion(v *semver.Version) bool {
	if v == nil {
		return false
	}
	pre := strings.ToLower(v.Prerelease())
	return strings.Contains(pre, "dev") || strings.Contains(pre, "snapshot")
}

// IsVerbatim reports whether path p is copied as-is (no substitution).
func (t *ProjectTemplate) IsVerbatim(p string) bool {
	return matchAny(t.Spec.Verbatim, p)
}

// IsBinary reports whether path p is treated as binary (never read as text).
func (t *ProjectTemplate) IsBinary(p string) bool {
	return matchAny(t.Spec.Binary, p)
}

func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if matchGlob(g, p) {
			return true
		}
	}
	return false
}
