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
	// Step 1: Replace escaped \${ with a placeholder
	s = strings.ReplaceAll(s, `\${`, escapePlaceholder)

	// Step 2: Expand environment variables
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

		if val, ok := os.LookupEnv(varName); ok {
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
