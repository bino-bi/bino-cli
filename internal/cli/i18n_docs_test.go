package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scenarioRangeToken matches the middle two members of each scenario family,
// which the docs cover with a `global.ac1` … `global.ac4` range row.
var scenarioRangeToken = regexp.MustCompile(`^global\.(ac|pp|fc|pl)[23]$`)

// i18nReferenceDoc is the reference page that documents the built-in token
// vocabulary. It is the fourth hand-maintained copy of the key list (after the
// engine bundles, defaultI18nTokens and the JSON schema), and the one that
// silently fell behind — it covered 30 of 78 tokens before this test existed.
const i18nReferenceDoc = "../../docs/src/content/docs/reference/internationalization.mdx"

// sharedXYTokenSuffixes are documented once, by suffix, in a table shared
// between the scatter and bubble charts rather than repeated per prefix.
var sharedXYTokenSuffixes = []string{
	"xy-prop-json",
	"xy-measure-invalid",
	"xy-family-mixed",
	"xy-level-order",
	"xy-duplicate-point",
	"xy-point-clipped",
	"xy-variance-nodata",
	"xy-label-unknown",
	"xy-series-overflow",
	"xy-point-budget",
}

// tokensDocumentedByPattern are covered by a `<chart>.<suffix>` row that stands
// in for every chart prefix, so the fully-qualified key never appears verbatim.
var tokensDocumentedByPattern = map[string]bool{
	"bn-chart-scatter.no-data":      true,
	"bn-chart-bubble.no-data":       true,
	"bn-chart-bullet.no-data":       true,
	"bn-chart-scatter.axis.in":      true,
	"bn-chart-bubble.axis.in":       true,
	"bn-chart-bullet.axis.in":       true,
	"bn-chart-scatter.legend.title": true,
	"bn-chart-bubble.legend.title":  true,
}

func TestI18nReferenceDocumentsEveryToken(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(i18nReferenceDoc))
	if err != nil {
		t.Fatalf("read %s: %v", i18nReferenceDoc, err)
	}
	doc := string(raw)

	shared := map[string]bool{}
	for _, suffix := range sharedXYTokenSuffixes {
		shared[suffix] = true
	}

	for key := range defaultI18nTokens["de"] {
		if tokensDocumentedByPattern[key] {
			continue
		}

		// The scenario labels are documented as ranges (`global.ac1` … `global.ac4`),
		// so the second and third of each family never appear verbatim.
		if scenarioRangeToken.MatchString(key) {
			continue
		}

		// Shared XY diagnostics are documented once by bare suffix.
		needle := "`" + key + "`"
		if _, suffix, ok := strings.Cut(key, "."); ok && shared[suffix] {
			needle = "`" + suffix + "`"
		}

		if !strings.Contains(doc, needle) {
			t.Errorf("token %q is in defaultI18nTokens but not documented in %s (looked for %s)",
				key, i18nReferenceDoc, needle)
		}
	}
}
