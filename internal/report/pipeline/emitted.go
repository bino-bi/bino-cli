package pipeline

import (
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/render"
)

// RegisterEmittedData registers a renderer's emitted dataset/datasource
// payloads on httpserver.Server so that <bn-datasource> / <bn-dataset>
// elements (in url mode) can fetch them via /__bino/data/{kind}/{name}?hash=…
// Safe to call with nil server, nil entries, or unknown kinds.
func RegisterEmittedData(server *httpserver.Server, entries []render.EmittedData) {
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
