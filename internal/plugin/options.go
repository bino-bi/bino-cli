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
	}
	RefreshAssets(ctx, opts, registry, renderMode)
	return opts
}

// RefreshAssets sets every field of opts that depends on which plugins the
// registry holds. Preview calls it again after a plugin reload; opts is
// shared by pointer, so the fields are updated in place. Preview renders that
// run without the refresh mutex copy opts first.
func RefreshAssets(ctx context.Context, opts *render.PluginOptions, registry *PluginRegistry, renderMode string) {
	assetCache := BuildAssetCache(ctx, registry, renderMode)
	opts.ExtraHeadMarkup = RenderAssetTags(assetCache)
	opts.AssetHandler = NewAssetHandler(assetCache)
	opts.HasComponentKinds = hasComponentKinds(registry)
	// A new renderer also forgets the fragment warnings already given, so a
	// reloaded plugin is checked again.
	opts.ComponentRenderer = NewComponentRenderer(registry)
}

// hasComponentKinds reports whether a loaded plugin provides a component kind.
func hasComponentKinds(registry *PluginRegistry) bool {
	for _, k := range registry.AllKinds() {
		if k.Category == KindCategoryComponent {
			return true
		}
	}
	return false
}
