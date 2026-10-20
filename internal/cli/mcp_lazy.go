package cli

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/config"
)

// lazyProject opens the standalone MCP project state (plugins, DuckDB, first
// refresh, file watcher) on the first call that needs it, not at startup.
// `bino mcp` runs in every agent session, mostly in folders where no tool is
// ever called, so an eager load costs a DuckDB session and a tree watch for
// nothing.
type lazyProject struct {
	// ctx is the server lifetime. Opening and the first refresh use it, not the
	// request ctx: the SDK cancels that when the call returns, which would kill
	// plugin processes, and a cancelled first call must not cache an empty State.
	ctx    context.Context
	root   string
	logger logx.Logger
	open   func(ctx context.Context, logger logx.Logger, root string) (*daemon.ManagedState, *plugin.PluginRegistry, func(), error)

	mu      sync.Mutex
	managed *daemon.ManagedState
	reg     *plugin.PluginRegistry
	cleanup func()
}

func newLazyProject(ctx context.Context, logger logx.Logger, root string) *lazyProject {
	return &lazyProject{ctx: ctx, root: root, logger: logger, open: loadStandaloneState}
}

// load returns the project state, opening it on first use. While the root has
// no bino.toml it returns mcp.ErrNotInitialized and opens nothing, so a
// bino.toml written later (by init_bundle or by hand) is picked up on the next
// call. A failed open is not cached; the next call tries again.
func (p *lazyProject) load(context.Context) (*daemon.State, *plugin.PluginRegistry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.managed != nil {
		return p.managed.State, p.reg, nil
	}
	if _, err := os.Stat(filepath.Join(p.root, "bino.toml")); err != nil {
		return nil, nil, mcp.ErrNotInitialized
	}
	managed, reg, cleanup, err := p.open(p.ctx, p.logger, p.root)
	if err != nil {
		return nil, nil, err
	}
	if err := managed.State.Refresh(p.ctx); err != nil {
		p.logger.Errorf("Initial refresh failed: %v", err)
	}
	if err := managed.Watch(p.ctx, nil); err != nil {
		p.logger.Warnf("File watcher unavailable: %v", err)
	}
	p.logger.Infof("Loaded project state for %s", p.root)
	p.managed, p.reg, p.cleanup = managed, reg, cleanup
	return managed.State, reg, nil
}

func (p *lazyProject) state(ctx context.Context) (*daemon.State, error) {
	st, _, err := p.load(ctx)
	return st, err
}

func (p *lazyProject) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cleanup != nil {
		p.cleanup()
	}
}

// lazyKinds adapts the lazily loaded plugin registry to config.KindProvider for
// manifest validation. Without a loaded project there are no plugin kinds.
type lazyKinds struct{ p *lazyProject }

func (k lazyKinds) provider() config.KindProvider {
	_, reg, err := k.p.load(k.p.ctx)
	if err != nil || reg == nil {
		return nil
	}
	return reg
}

func (k lazyKinds) PluginKindNames() []string {
	if prov := k.provider(); prov != nil {
		return prov.PluginKindNames()
	}
	return nil
}

func (k lazyKinds) GetKind(kindName string) (config.KindInfo, bool) {
	if prov := k.provider(); prov != nil {
		return prov.GetKind(kindName)
	}
	return config.KindInfo{}, false
}
