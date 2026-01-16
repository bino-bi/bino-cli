package dataset

import (
	"fmt"
	"regexp"
	"strconv"
)

// inlineRefPattern matches @inline(N) references in SQL/PRQL queries.
// Examples: @inline(0), @inline(1), @inline(42)
var inlineRefPattern = regexp.MustCompile(`@inline\((\d+)\)`)

// RewriteInlineRefs replaces @inline(N) placeholders with generated datasource names.
// The inlineNames slice contains the generated DataSource names for inline dependencies,
// indexed by position (0-based).
//
// Example:
//
//	query: "SELECT * FROM @inline(0) WHERE region = 'US'"
//	inlineNames: ["_inline_datasource_a1b2c3d4"]
//	result: "SELECT * FROM \"_inline_datasource_a1b2c3d4\" WHERE region = 'US'"
func RewriteInlineRefs(query string, inlineNames []string) (string, error) {
	if len(inlineNames) == 0 {
		// No inline names provided - check if query has @inline refs
		if inlineRefPattern.MatchString(query) {
			return "", fmt.Errorf("query contains @inline references but no inline dependencies provided")
		}
		return query, nil
	}

	var rewriteErr error

	result := inlineRefPattern.ReplaceAllStringFunc(query, func(match string) string {
		if rewriteErr != nil {
			return match // Already have an error, don't process further
		}

		// Extract index from match
		submatches := inlineRefPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			rewriteErr = fmt.Errorf("invalid @inline reference: %s", match)
			return match
		}

		idx, err := strconv.Atoi(submatches[1])
		if err != nil {
			rewriteErr = fmt.Errorf("invalid @inline index: %s", submatches[1])
			return match
		}

		if idx < 0 || idx >= len(inlineNames) {
			rewriteErr = fmt.Errorf("@inline(%d) index out of bounds (have %d inline dependencies)", idx, len(inlineNames))
			return match
		}

		// Return the generated name as a quoted identifier for DuckDB
		return fmt.Sprintf(`"%s"`, inlineNames[idx])
	})

	return result, rewriteErr
}

// ValidateInlineRefs checks that all @inline(N) references have valid indices
// without performing replacement. Returns all errors found.
func ValidateInlineRefs(query string, inlineCount int) []error {
	matches := inlineRefPattern.FindAllStringSubmatch(query, -1)
	if len(matches) == 0 {
		return nil
	}

	var errs []error
	seen := make(map[int]bool)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		idx, err := strconv.Atoi(match[1])
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid @inline index %q", match[1]))
			continue
		}
		if seen[idx] {
			continue // Already checked this index
		}
		seen[idx] = true

		if idx < 0 || idx >= inlineCount {
			errs = append(errs, fmt.Errorf("@inline(%d) out of bounds (have %d inline dependencies)", idx, inlineCount))
		}
	}

	return errs
}

// HasInlineRefs returns true if the query contains any @inline(N) references.
func HasInlineRefs(query string) bool {
	return inlineRefPattern.MatchString(query)
}

// ExtractInlineIndices returns all unique indices referenced by @inline(N) in the query.
// The returned slice is sorted in ascending order.
func ExtractInlineIndices(query string) []int {
	matches := inlineRefPattern.FindAllStringSubmatch(query, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[int]bool)
	var indices []int

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		idx, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if !seen[idx] {
			seen[idx] = true
			indices = append(indices, idx)
		}
	}

	// Sort indices
	for i := 0; i < len(indices)-1; i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[i] > indices[j] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	return indices
}
