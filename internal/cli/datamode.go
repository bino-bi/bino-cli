package cli

import (
	"fmt"
	"strings"

	"bino.bi/bino/internal/report/render"
)

// normalizeDataMode validates and canonicalises the --data-mode flag value.
// "" defaults to render.DataModeURL. Returns the canonical form or an error
// when the user passed an unrecognized value.
func normalizeDataMode(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", render.DataModeURL:
		return render.DataModeURL, nil
	case render.DataModeInline:
		return render.DataModeInline, nil
	default:
		return "", fmt.Errorf("invalid --data-mode %q: expected %q or %q", s, render.DataModeURL, render.DataModeInline)
	}
}
