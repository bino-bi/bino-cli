package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/version"
)

// commandEnv holds common state produced by initCommandEnv. The build, preview,
// and serve commands all share the same startup sequence; this struct captures
// its result so each command's RunE can focus on command-specific logic.
type commandEnv struct {
	// ProjectRoot is the resolved absolute path (directory containing bino.toml).
	ProjectRoot string
	// ProjectCfg is the loaded bino.toml configuration.
	ProjectCfg *pathutil.ProjectConfig
	// CacheDir is the resolved cache directory for CDN assets.
	CacheDir string
	// EngineVersion is the resolved template engine version string.
	EngineVersion string
	// EngineVersionPinned is true when bino.toml declares an explicit engine-version.
	EngineVersionPinned bool
	// HookRunner runs lifecycle hooks for the active command.
	HookRunner *hooks.Runner
	// Resolver resolves CLI flag values with bino.toml defaults.
	Resolver *pathutil.ArgResolver
	// PluginManager manages plugin lifecycle. May be nil if no plugins are declared.
	PluginManager *plugin.Manager
	// PluginRegistry holds loaded plugins. May be nil if no plugins are declared.
	PluginRegistry *plugin.PluginRegistry
}

// initCommandEnv performs the initialization sequence shared by build, preview,
// and serve: resolve project root, load bino.toml, apply env vars, create a
// hook runner and arg resolver, and ensure the template engine is available.
//
// mode selects the [build], [preview], or [serve] section from bino.toml.
func initCommandEnv(ctx context.Context, cmd *cobra.Command, workdir, mode string, logger logx.Logger) (*commandEnv, error) {
	cacheDir, err := pathutil.CacheDir("cdn")
	if err != nil {
		return nil, RuntimeError(err)
	}
	logger.Debugf("Using cache directory %s", cacheDir)

	projectRoot, err := pipeline.ResolveProjectRoot(workdir)
	if err != nil {
		return nil, ConfigError(err)
	}

	projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
	if err != nil {
		logger.Debugf("Could not load bino.toml defaults: %v", err)
		projectCfg = &pathutil.ProjectConfig{}
	}

	cmdCfg := commandConfigForMode(projectCfg, mode)

	cmdCfg.Env.Apply(func(key, tomlVal, envVal string) {
		logger.Infof("Environment variable %s overrides bino.toml (%q -> %q)", key, tomlVal, envVal)
	})

	hookRunner := hooks.NewRunner(
		hooks.Resolve(projectCfg.Hooks, cmdCfg.Hooks, logger.Channel("hooks")),
		logger.Channel("hooks"), projectRoot,
	)

	resolver := pathutil.NewArgResolver(cmd, cmdCfg.Args, func(format string, args ...any) {
		logger.Infof(format, args...)
	})

	engineVersion := projectCfg.EngineVersion
	engineVersionPinned := engineVersion != ""
	engineMgr, err := engine.NewManager()
	if err != nil {
		return nil, RuntimeError(fmt.Errorf("initialize engine manager: %w", err))
	}
	engineInfo, err := engineMgr.EnsureVersion(ctx, engineVersion)
	if err != nil {
		return nil, ConfigError(fmt.Errorf("template engine: %w", err))
	}
	engineVersion = engineInfo.Version
	logger.Infof("Using template engine %s", engineVersion)

	// Load plugins if declared in bino.toml.
	var pluginMgr *plugin.Manager
	var pluginReg *plugin.PluginRegistry
	if len(projectCfg.Plugins) > 0 {
		pluginMgr = plugin.NewManager(logger.Channel("plugin"))
		pluginMgr.SetVerbose(logx.DebugEnabled(ctx))
		if err := pluginMgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err != nil {
			logger.Warnf("Failed to load plugins: %v", err)
			// Continue without plugins — don't block the command.
			pluginMgr = nil
		} else {
			pluginReg = pluginMgr.Registry()
		}
	}

	return &commandEnv{
		ProjectRoot:         projectRoot,
		ProjectCfg:          projectCfg,
		CacheDir:            cacheDir,
		EngineVersion:       engineVersion,
		EngineVersionPinned: engineVersionPinned,
		HookRunner:          hookRunner,
		Resolver:            resolver,
		PluginManager:       pluginMgr,
		PluginRegistry:      pluginReg,
	}, nil
}

// commandConfigForMode returns the CommandConfig section from bino.toml
// for the given command mode.
func commandConfigForMode(cfg *pathutil.ProjectConfig, mode string) pathutil.CommandConfig {
	switch mode {
	case "build":
		return cfg.Build
	case "preview":
		return cfg.Preview
	case "serve":
		return cfg.Serve
	default:
		return pathutil.CommandConfig{}
	}
}

// newQueryLogger creates a SQL query logger function that respects verbose mode.
// Returns nil when logSQL is false.
func newQueryLogger(ctx context.Context, logger logx.Logger, logSQL bool) func(string) {
	if !logSQL {
		return nil
	}
	return func(query string) {
		if logx.DebugEnabled(ctx) {
			logger.Infof("SQL query:\n%s", query)
		} else {
			logger.Infof("SQL: %s", strings.ReplaceAll(strings.TrimSpace(query), "\n", " "))
		}
	}
}
