package cli

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/version"
)

func newMCPCommand() *cobra.Command {
	var (
		workdir string
		noProxy bool
	)

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server over stdio for AI agents",
		Long: `Runs a Model Context Protocol (MCP) server over stdio, exposing bino's
introspection, validation, authoring, and build surface to AI agent clients
(Claude Code/Desktop, Cursor, ...).

When a bino daemon is already running for the project (e.g. VS Code is open),
this command proxies to that daemon's /mcp endpoint so the agent reuses the
already-loaded DuckDB session and file watcher instead of starting a second one.
With no daemon running, it serves the MCP directly from its own project state.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Route ALL logging to stderr — stdout is the JSON-RPC channel and
			// must carry nothing but MCP protocol traffic.
			verbose := logx.DebugEnabled(cmd.Context())
			logger := logx.NewTerminalWithColor(cmd.ErrOrStderr(), cmd.ErrOrStderr(), verbose, true).Channel("mcp")
			ctx := logx.WithLogger(cmd.Context(), logger)

			projectRoot, initialized, err := resolveMCPRoot(workdir)
			if err != nil {
				return ConfigError(err)
			}
			if !initialized {
				logger.Infof("No bino.toml found; starting MCP rooted at %s — not yet a bino project (use init_bundle to scaffold a bundle here)", projectRoot)
			}

			// Proxy to a running daemon when one exists, so the agent reuses the
			// daemon's loaded State rather than spinning up a second DuckDB session.
			if !noProxy {
				if pf, _ := daemon.ReadPortFile(projectRoot); pf != nil {
					endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", pf.Port)
					logger.Infof("Proxying to daemon MCP at %s", endpoint)
					if err := runMCPProxy(ctx, endpoint); err != nil {
						logger.Warnf("Proxy to daemon failed (%v); falling back to standalone", err)
					} else {
						return nil
					}
				}
			}

			return runMCPStandalone(ctx, logger, projectRoot)
		},
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory (project root)")
	cmd.Flags().BoolVar(&noProxy, "no-proxy", false, "Always run standalone, even if a daemon is running")
	return cmd
}

// resolveMCPRoot resolves the project root for the MCP server. Unlike the other
// commands, the MCP server must start even in a folder that is not yet a bino
// project, so an agent can scaffold a bundle in place (init_bundle /
// create_manifest) instead of being forced to bootstrap with a separate CLI
// call first. When no bino.toml is found up the tree, it roots the server at the
// working directory itself; initialized reports whether an existing project was
// found.
func resolveMCPRoot(workdir string) (root string, initialized bool, err error) {
	root, err = pipeline.ResolveProjectRoot(workdir)
	if err == nil {
		return root, true, nil
	}
	if !errors.Is(err, pathutil.ErrProjectRootNotFound) {
		return "", false, err
	}
	root, err = pathutil.ResolveWorkdir(workdir)
	if err != nil {
		return "", false, err
	}
	return root, false, nil
}

// runMCPStandalone serves the MCP directly over stdio from a freshly loaded
// project State (no daemon involved).
func runMCPStandalone(ctx context.Context, logger logx.Logger, projectRoot string) error {
	projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
	if err != nil {
		logger.Debugf("Could not load bino.toml: %v", err)
		projectCfg = &pathutil.ProjectConfig{}
	}

	// Load plugins so plugin kinds/linters appear in schema/validate (parity with
	// the daemon and `bino schema`). Engine compatibility is intentionally not
	// checked here — introspection should work regardless; the build tool shells
	// out to `bino build`, which enforces engine-compat itself.
	var reg *plugin.PluginRegistry
	if len(projectCfg.Plugins) > 0 {
		mgr := plugin.NewManager(logger.Channel("plugin"))
		mgr.SetVerbose(logx.DebugEnabled(ctx))
		if err := mgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err != nil {
			logger.Warnf("Failed to load plugins: %v", err)
		} else {
			reg = mgr.Registry()
			defer mgr.ShutdownAll(ctx)
		}
	}

	managedCfg := daemon.ManagedStateConfig{ProjectRoot: projectRoot, Logger: logger}
	if reg != nil {
		managedCfg.KindProvider = reg
		managedCfg.PluginLinters = plugin.NewLinterRegistry(reg)
	}
	managed, err := daemon.NewManagedState(ctx, managedCfg)
	if err != nil {
		return RuntimeError(err)
	}
	defer managed.Close()

	if err := managed.State.Refresh(ctx); err != nil {
		logger.Errorf("Initial refresh failed: %v", err)
	}
	if err := managed.Watch(ctx, nil); err != nil {
		logger.Warnf("File watcher unavailable: %v", err)
	}

	logger.Infof("bino MCP server ready (standalone) for %s", projectRoot)
	srv := mcp.NewServer(mcp.Deps{State: managed.State, Registry: reg, Authoring: newCLIAuthoring(projectRoot)})
	return srv.Run(ctx, &mcpsdk.StdioTransport{})
}

// runMCPProxy bridges this stdio entrypoint to a running daemon's /mcp endpoint:
// it connects to the daemon, mirrors its tools/resources onto a local stdio
// server that forwards each call, and relays progress notifications back. The
// daemon is connected first so a connect failure can fall back to standalone
// before any byte is written to stdout.
func runMCPProxy(ctx context.Context, endpoint string) error {
	var localSession atomic.Pointer[mcpsdk.ServerSession]

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "bino-mcp-proxy", Version: version.Version}, &mcpsdk.ClientOptions{
		// Relay progress notifications from the daemon to our stdio client. The
		// progress token is forwarded verbatim on each CallTool, so the client
		// correlates it with the original request.
		ProgressNotificationHandler: func(ctx context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
			if ls := localSession.Load(); ls != nil {
				_ = ls.NotifyProgress(ctx, req.Params)
			}
		},
	})

	upstream, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		return fmt.Errorf("connect daemon: %w", err)
	}
	defer func() { _ = upstream.Close() }()

	local := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "bino", Title: "bino — Report-as-Code", Version: version.Version}, nil)
	if err := mirrorUpstream(ctx, local, upstream); err != nil {
		return err
	}

	session, err := local.Connect(ctx, &mcpsdk.StdioTransport{}, nil)
	if err != nil {
		return fmt.Errorf("serve stdio: %w", err)
	}
	localSession.Store(session)
	return session.Wait()
}

// mirrorUpstream registers forwarding tools and resources on local that proxy
// each call to the connected upstream daemon. bino exposes well under one page
// of tools/resources, so pagination is not needed.
func mirrorUpstream(ctx context.Context, local *mcpsdk.Server, upstream *mcpsdk.ClientSession) error {
	tools, err := upstream.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	for _, t := range tools.Tools {
		local.AddTool(t, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			params := &mcpsdk.CallToolParams{Name: req.Params.Name, Arguments: req.Params.Arguments}
			if tok := req.Params.GetProgressToken(); tok != nil {
				params.SetProgressToken(tok)
			}
			return upstream.CallTool(ctx, params)
		})
	}

	if resources, err := upstream.ListResources(ctx, nil); err == nil {
		for _, r := range resources.Resources {
			local.AddResource(r, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
				return upstream.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: req.Params.URI})
			})
		}
	}
	if templates, err := upstream.ListResourceTemplates(ctx, nil); err == nil {
		for _, tmpl := range templates.ResourceTemplates {
			local.AddResourceTemplate(tmpl, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
				return upstream.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: req.Params.URI})
			})
		}
	}
	return nil
}
