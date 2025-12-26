package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Document captures the minimal metadata needed by the CLI to orchestrate
// downstream stages without committing to a full internal representation yet.
type Document struct {
	File           string            // Absolute path to the YAML file that produced this document.
	Position       int               // 1-based index within the source file for multi-doc manifests.
	Kind           string            // Kind extracted from the manifest header.
	Name           string            // metadata.name value.
	Labels         map[string]string // metadata.labels for constraint evaluation.
	Constraints    []string          // metadata.constraints for conditional inclusion.
	Raw            json.RawMessage   // Validated JSON payload for downstream consumers.
	MissingEnvVars []string          // Environment variables referenced but not set (no default).
}

type documentHeader struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string            `json:"name"`
		Labels      map[string]string `json:"labels"`
		Constraints []string          `json:"constraints"`
	} `json:"metadata"`
}

// MissingEnvVar represents an unresolved environment variable reference.
type MissingEnvVar struct {
	VarName string // Name of the missing environment variable.
	File    string // File where the variable was referenced.
}

// CheckMissingEnvVars aggregates all missing environment variables across documents.
// Returns an error if any documents have unresolved environment variables.
// The error message includes the variable names and their source files.
func CheckMissingEnvVars(docs []Document) error {
	var missing []MissingEnvVar
	seen := make(map[string]struct{})

	for _, doc := range docs {
		for _, varName := range doc.MissingEnvVars {
			key := varName + ":" + doc.File
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			missing = append(missing, MissingEnvVar{VarName: varName, File: doc.File})
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var parts []string
	for _, m := range missing {
		parts = append(parts, fmt.Sprintf("%s in %s", m.VarName, m.File))
	}
	return fmt.Errorf("unresolved environment variables: %s", strings.Join(parts, ", "))
}

// CheckMissingEnvVarsExcluding aggregates all missing environment variables across documents,
// excluding any variable names in the provided set. Returns an error if any unresolved variables remain.
func CheckMissingEnvVarsExcluding(docs []Document, exclude map[string]struct{}) error {
	var missing []MissingEnvVar
	seen := make(map[string]struct{})

	for _, doc := range docs {
		for _, varName := range doc.MissingEnvVars {
			// Skip if this var is in the exclude list
			if _, ok := exclude[varName]; ok {
				continue
			}
			key := varName + ":" + doc.File
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			missing = append(missing, MissingEnvVar{VarName: varName, File: doc.File})
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var parts []string
	for _, m := range missing {
		parts = append(parts, fmt.Sprintf("%s in %s", m.VarName, m.File))
	}
	return fmt.Errorf("unresolved environment variables: %s", strings.Join(parts, ", "))
}

// CollectMissingEnvVars returns all missing environment variables across documents.
// Unlike CheckMissingEnvVars, it returns the list instead of an error.
func CollectMissingEnvVars(docs []Document) []MissingEnvVar {
	var missing []MissingEnvVar
	seen := make(map[string]struct{})

	for _, doc := range docs {
		for _, varName := range doc.MissingEnvVars {
			key := varName + ":" + doc.File
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			missing = append(missing, MissingEnvVar{VarName: varName, File: doc.File})
		}
	}
	return missing
}
