package plugin

import (
	"context"

	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/render"
)

// BuildRenderOptions creates the standard render.PluginOptions from a registry.
// This is the shared setup pattern used by build, preview, and serve commands.
func BuildRenderOptions(ctx context.Context, registry *PluginRegistry, projectRoot string, renderMode string) *render.PluginOptions {
	opts := &render.PluginOptions{
		CollectOptions: &datasource.CollectOptions{
			PluginRouter: NewPluginRouter(registry, projectRoot),
			ProjectRoot:  projectRoot,
		},
		ComponentRenderer: NewComponentRenderer(registry),
	}
	assetCache := BuildAssetCache(ctx, registry, renderMode)
	opts.ExtraHeadMarkup = RenderAssetTags(assetCache)
	return opts
}
