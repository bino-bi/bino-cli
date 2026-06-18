package template

import (
	"strings"
	"testing"
)

func TestParseManifestStrictUnknownKey(t *testing.T) {
	// A speculative hooks: key must be rejected (fail-closed), never ignored.
	src := `apiVersion: bino.bi/v1alpha1
kind: ProjectTemplate
metadata:
  name: evil
spec:
  hooks:
    - run: rm -rf /
`
	if _, err := ParseManifest([]byte(src)); err == nil {
		t.Fatal("expected error for unknown key 'hooks', got nil")
	}
}

func TestParseManifestRejectsWrongAPIVersion(t *testing.T) {
	src := `apiVersion: bino.bi/v2
kind: ProjectTemplate
metadata:
  name: future
spec: {}
`
	_, err := ParseManifest([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("expected apiVersion error, got %v", err)
	}
}

func TestParseManifestRejectsWrongKind(t *testing.T) {
	src := `apiVersion: bino.bi/v1alpha1
kind: NotATemplate
metadata:
  name: x
spec: {}
`
	_, err := ParseManifest([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("expected kind error, got %v", err)
	}
}

func TestValidateMinBinoVersion(t *testing.T) {
	tmpl := &ProjectTemplate{Spec: Spec{MinBinoVersion: ">=1.2.0"}}
	tests := []struct {
		name       string
		cliVersion string
		wantErr    bool
	}{
		{"satisfied", "1.5.0", false},
		{"too old", "1.0.0", true},
		{"dev build skips gate", "0.1.0-dev", false},
		{"unparseable skips gate", "not-a-version", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tmpl.Validate(tt.cliVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.cliVersion, err, tt.wantErr)
			}
		})
	}
}

func TestValidateNoMinVersion(t *testing.T) {
	tmpl := &ProjectTemplate{}
	if err := tmpl.Validate("0.1.0-dev"); err != nil {
		t.Errorf("expected nil for empty min-bino-version, got %v", err)
	}
}

func TestBuiltinManifestsParse(t *testing.T) {
	for _, name := range BuiltinNames() {
		if _, err := BuiltinManifest(name); err != nil {
			t.Errorf("built-in %q manifest failed to parse: %v", name, err)
		}
	}
}
