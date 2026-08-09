package daemon

import (
	"context"
	"fmt"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/watchers"
	"bino.bi/bino/pkg/duckdb"
)

// ManagedState bundles a DuckDB session, a daemon State, and an optional file
// watcher so the exact same project-model setup backs both the HTTP daemon
// (internal/cli/daemon.go) and the MCP server (internal/cli/mcp.go standalone
// mode). It owns the session lifetime; call Close when done.
type ManagedState struct {
	State   *State
	session *duckdb.Session
	root    string
	logger  logx.Logger
}

// ManagedStateConfig configures NewManagedState.
type ManagedStateConfig struct {
	ProjectRoot   string
	Logger        logx.Logger
	KindProvider  config.KindProvider
	PluginLinters lint.PluginLinterRegistry
	// EngineCompat is the engine-version compatibility check to run during
	// validation (see State.SetEngineCompat). May be nil.
	EngineCompat func(dir string) (Diagnostic, bool)
}

// NewManagedState opens a shared DuckDB session, loads the standard extensions,
// constructs a State, and wires plugin kind/linter providers. It does NOT call
// Refresh — callers decide when to load (so they can log the initial-load error
// without aborting startup).
func NewManagedState(ctx context.Context, cfg ManagedStateConfig) (*ManagedState, error) {
	if cfg.Logger == nil {
		cfg.Logger = logx.Nop()
	}

	duckdbOpts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, fmt.Errorf("duckdb options: %w", err)
	}
	session, err := duckdb.OpenSession(ctx, duckdbOpts)
	if err != nil {
		return nil, fmt.Errorf("open duckdb session: %w", err)
	}
	if err := session.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
		session.Close() //nolint:errcheck // best-effort teardown on the init error path
		return nil, fmt.Errorf("load duckdb extensions: %w", err)
	}

	state, err := NewState(cfg.ProjectRoot, session, cfg.Logger)
	if err != nil {
		session.Close() //nolint:errcheck // best-effort teardown on the init error path
		return nil, err
	}
	if cfg.KindProvider != nil {
		state.SetKindProvider(cfg.KindProvider)
	}
	if cfg.PluginLinters != nil {
		state.SetPluginLinters(cfg.PluginLinters)
	}
	if cfg.EngineCompat != nil {
		state.SetEngineCompat(cfg.EngineCompat)
	}

	return &ManagedState{State: state, session: session, root: cfg.ProjectRoot, logger: cfg.Logger}, nil
}

// Watch starts a debounced file watcher that calls State.Refresh on project
// changes. onRefresh, if non-nil, is invoked after each successful refresh with
// the batch of raw reasons (the daemon uses these to broadcast SSE events;
// CoalesceReasons summarizes them for display). The watcher and its goroutines
// stop when ctx is done.
func (m *ManagedState) Watch(ctx context.Context, onRefresh func(state *State, reasons []string)) error {
	refreshCh := make(chan string, 16)
	enqueue := func(reason string) {
		select {
		case refreshCh <- reason:
		default:
		}
	}

	watchLog := m.logger.Channel("watcher")
	watcher, err := watchers.NewWatcher(watchers.Config{
		Root:   m.root,
		Logger: watchLog,
		Handler: func(evt watchers.Event) {
			watchLog.Infof("File updated %s (%s)", evt.RelativePath, evt.Op)
			enqueue(fmt.Sprintf("change %s", evt.RelativePath))
		},
	})
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	go watcher.Run(ctx)

	go func() {
		defer watcher.Close() //nolint:errcheck // fsnotify teardown when the daemon watch loop exits
		debounce := time.NewTimer(0)
		if !debounce.Stop() {
			<-debounce.C
		}
		var reasons []string
		for {
			select {
			case <-ctx.Done():
				debounce.Stop()
				return
			case reason := <-refreshCh:
				reasons = append(reasons, reason)
				debounce.Reset(300 * time.Millisecond)
			case <-debounce.C:
				if len(reasons) == 0 {
					continue
				}
				batch := append([]string(nil), reasons...)
				reasons = reasons[:0]
				if err := m.State.Refresh(ctx); err != nil {
					m.logger.Errorf("Refresh failed: %v", err)
					continue
				}
				m.logger.Infof("Refreshed (%s)", CoalesceReasons(batch))
				if onRefresh != nil {
					onRefresh(m.State, batch)
				}
			}
		}
	}()

	return nil
}

// Close tears down the State temp dir and the shared DuckDB session.
func (m *ManagedState) Close() {
	if m.State != nil {
		m.State.Close()
	}
	if m.session != nil {
		m.session.Close() //nolint:errcheck // in-memory session teardown at daemon shutdown
	}
}

// CoalesceReasons summarizes a batch of watcher reasons for display.
func CoalesceReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "unknown"
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	return fmt.Sprintf("%s (+%d more)", reasons[0], len(reasons)-1)
}
