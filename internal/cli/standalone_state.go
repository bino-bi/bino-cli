package cli

import (
	"context"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
)

// loadStandaloneState builds a project ManagedState with plugins loaded, shared
// by the standalone `bino mcp` and `bino lsp` paths so the two cannot drift. The
// returned cleanup closes the state and shuts plugins down; the caller decides
// when to Refresh/Watch. A plugin that does not load becomes a bino.toml
// diagnostic, not an error, so the editor keeps working. Engine compatibility
// is intentionally not checked - introspection should work regardless.
func loadStandaloneState(ctx context.Context, logger logx.Logger, projectRoot string) (managed *daemon.ManagedState, reg *plugin.PluginRegistry, cleanup func(), err error) {
	projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
	if err != nil {
		logger.Debugf("Could not load bino.toml: %v", err)
		projectCfg = &pathutil.ProjectConfig{}
	}

	managedCfg := daemon.ManagedStateConfig{ProjectRoot: projectRoot, Logger: logger, EngineCompat: engineCompatDiagnostic}
	mgr, loadErr := loadProjectPlugins(ctx, logger, projectCfg, projectRoot)
	if loadErr != nil {
		logger.Errorf("%v", loadErr)
		managedCfg.ConfigDiagnostics = []LSPDiagnostic{pluginLoadDiagnostic(projectRoot, loadErr)}
	}
	if mgr != nil {
		reg = mgr.Registry()
		managedCfg.KindProvider = reg
		managedCfg.PluginLinters = plugin.NewLinterRegistry(reg)
	}
	managed, err = daemon.NewManagedState(ctx, managedCfg)
	if err != nil {
		if mgr != nil {
			mgr.ShutdownAll(ctx)
		}
		return nil, nil, nil, err
	}

	cleanup = func() {
		managed.Close()
		if mgr != nil {
			mgr.ShutdownAll(ctx)
		}
	}
	return managed, reg, cleanup, nil
}
