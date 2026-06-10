package pipeline

import (
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/render"
)

// ConvertLocalAssets transforms render.LocalAsset slice to httpserver.LocalAsset slice.
// This is used when setting up HTTP servers for build and preview.
func ConvertLocalAssets(assets []render.LocalAsset) []httpserver.LocalAsset {
	if len(assets) == 0 {
		return nil
	}
	converted := make([]httpserver.LocalAsset, 0, len(assets))
	for _, asset := range assets {
		converted = append(converted, httpserver.LocalAsset{
			URLPath:   asset.URLPath,
			FilePath:  asset.FilePath,
			MediaType: asset.MediaType,
		})
	}
	return converted
}
