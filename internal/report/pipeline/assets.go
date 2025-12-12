package pipeline

import (
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/render"
)

// ConvertLocalAssets transforms render.LocalAsset slice to previewhttp.LocalAsset slice.
// This is used when setting up HTTP servers for build and preview.
func ConvertLocalAssets(assets []render.LocalAsset) []previewhttp.LocalAsset {
	if len(assets) == 0 {
		return nil
	}
	converted := make([]previewhttp.LocalAsset, 0, len(assets))
	for _, asset := range assets {
		converted = append(converted, previewhttp.LocalAsset{
			URLPath:   asset.URLPath,
			FilePath:  asset.FilePath,
			MediaType: asset.MediaType,
		})
	}
	return converted
}
