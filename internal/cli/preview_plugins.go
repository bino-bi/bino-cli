package cli

import (
	"context"
	"time"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/render"
)

// previewPlugins restarts the plugins in bino preview when a plugin binary or
// bino.toml changes. A nil *previewPlugins (no plugins declared) does nothing.
type previewPlugins struct {
	mgr    *plugin.Manager
	opts   *render.PluginOptions
	server *httpserver.Server
	logger logx.Logger

	// err is the last failed reload. While set, refreshes show it instead of
	// running on an empty registry, which would only give schema errors.
	err error
	// stale is set by a reload and cleared by the next refresh that succeeds.
	// Until then pages may still come from the old plugins.
	stale bool
}

// reloadIfChanged reloads the plugins when they changed. It returns the
// stored reload error, or whether the next refresh must render every page.
// Callers hold the refresh mutex. ctx must be the command ctx: the plugin
// processes end with it.
func (p *previewPlugins) reloadIfChanged(ctx context.Context) (fullRefresh bool, err error) {
	if p == nil {
		return false, nil
	}
	if !p.mgr.Changed() {
		return p.stale, p.err
	}
	p.logger.Infof("Plugin binary or bino.toml changed, restarting plugins")
	err = p.mgr.Reload(ctx)
	// Also after a failure, so nothing serves the old assets.
	plugin.RefreshAssets(ctx, p.opts, p.mgr.Registry(), "preview")
	p.server.SetPluginAssets(p.opts.AssetHandler)
	if err != nil {
		p.err = pluginLoadError(err)
		p.logger.Errorf("%v", p.err)
		return false, p.err
	}
	p.err = nil
	p.stale = true
	return true, nil
}

// refreshed is called after a refresh succeeded. After a reload it makes the
// browser load the page again: a context swap never runs the new plugin
// scripts in <head>. Waiting for a good refresh keeps its error visible,
// because a reloaded page does not get the error again.
func (p *previewPlugins) refreshed() {
	if p == nil || !p.stale {
		return
	}
	p.stale = false
	p.server.BroadcastPluginsReloaded()
}

// poll checks once a second and calls changed when a plugin binary or
// bino.toml differs from the last load. Plugin binaries live outside the
// watched dirs (.bino/ is ignored, $PATH and ~/.bino are not watched), and a
// rebuild replaces the file, so they are polled instead.
func (p *previewPlugins) poll(ctx context.Context, changed func()) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.mgr.Changed() {
				changed()
			}
		}
	}
}
