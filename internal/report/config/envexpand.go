package config

import (
	"os"
	"regexp"
	"strings"
)

// envVarPattern matches ${VAR} and ${VAR:default} syntax.
// It does NOT match escaped sequences like \${VAR}.
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)

// escapePlaceholder is used to temporarily replace \${ during expansion.
const escapePlaceholder = "\x00BINO_ESC_DOLLAR_BRACE\x00"

// LookupFunc is a function that looks up a variable value by name.
// It returns the value and true if found, or empty string and false if not found.
type LookupFunc func(name string) (string, bool)

// EnvLookup returns a LookupFunc that uses os.LookupEnv.
func EnvLookup() LookupFunc {
	return os.LookupEnv
}

// MapLookup returns a LookupFunc that uses a map for lookups.
func MapLookup(m map[string]string) LookupFunc {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// ChainLookup returns a LookupFunc that tries multiple lookups in order.
// Returns the first successful lookup.
func ChainLookup(lookups ...LookupFunc) LookupFunc {
	return func(name string) (string, bool) {
		for _, lookup := range lookups {
			if v, ok := lookup(name); ok {
				return v, true
			}
		}
		return "", false
	}
}

// ExpandEnvVars replaces environment variable references in the input string.
//
// Supported syntax:
//   - ${VAR}         - replaced with the value of VAR, or empty string if not set
//   - ${VAR:default} - replaced with the value of VAR, or "default" if not set
//   - \${VAR}        - escape sequence, replaced with literal ${VAR}
//
// Returns the expanded string and a slice of variable names that were referenced
// but not set (and had no default value). The caller can use this list to warn
// or error depending on the context.
func ExpandEnvVars(s string) (expanded string, missingVars []string) {
	return ExpandVars(s, EnvLookup())
}

// ExpandVars replaces variable references in the input string using the provided lookup function.
//
// Supported syntax:
//   - ${VAR}         - replaced with the value of VAR, or empty string if not set
//   - ${VAR:default} - replaced with the value of VAR, or "default" if not set
//   - \${VAR}        - escape sequence, replaced with literal ${VAR}
//
// Returns the expanded string and a slice of variable names that were referenced
// but not set (and had no default value). The caller can use this list to warn
// or error depending on the context.
func ExpandVars(s string, lookup LookupFunc) (expanded string, missingVars []string) {
	// Step 1: Replace escaped \${ with a placeholder
	s = strings.ReplaceAll(s, `\${`, escapePlaceholder)

	// Step 2: Expand variables using the provided lookup
	seen := make(map[string]struct{})
	expanded = envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		// hasDefault is true if the colon syntax was used (even with empty default)
		// The regex captures the colon group only if ":" is present.
		// We detect this by checking if the match contains ":" after the var name.
		hasDefault := strings.Contains(match, varName+":")
		defaultValue := ""
		if hasDefault && len(parts) >= 3 {
			defaultValue = parts[2]
		}

		if val, ok := lookup(varName); ok {
			return val
		}

		// Variable not set
		if hasDefault {
			return defaultValue
		}

		// Track missing variable (only once per name)
		if _, alreadySeen := seen[varName]; !alreadySeen {
			seen[varName] = struct{}{}
			missingVars = append(missingVars, varName)
		}
		return ""
	})

	// Step 3: Restore escaped sequences to literal ${
	expanded = strings.ReplaceAll(expanded, escapePlaceholder, "${")

	return expanded, missingVars
}
