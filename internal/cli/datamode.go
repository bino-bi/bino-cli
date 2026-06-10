package cli

import (
	"fmt"
	"strings"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/render"
)

// registerEmittedData registers a renderer's emitted dataset/datasource
// payloads on httpserver.Server so that <bn-datasource> / <bn-dataset>
// elements (in url mode) can fetch them via /__bino/data/{kind}/{name}?hash=…
// Safe to call with nil server, nil entries, or unknown kinds.
func registerEmittedData(server *httpserver.Server, entries []render.EmittedData) {
	if server == nil || len(entries) == 0 {
		return
	}
	for _, e := range entries {
		switch e.Kind {
		case render.EmittedKindDatasource:
			server.PutDatasource(e.Name, e.Hash, e.Body)
		case render.EmittedKindDataset:
			server.PutDataset(e.Name, e.Hash, e.Body)
		}
	}
}

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
