package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/dataset"
)

// connCounter wraps an http.Handler and tracks the number of in-flight requests,
// so an attached MCP session (which holds an open Streamable-HTTP stream) counts
// as daemon activity for the idle-shutdown check.
type connCounter struct {
	inner http.Handler
	n     atomic.Int64
}

func (c *connCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.n.Add(1)
	defer c.n.Add(-1)
	c.inner.ServeHTTP(w, r)
}

func (c *connCounter) active() int64 { return c.n.Load() }

const defaultInactivityTimeout = 5 * time.Minute

// daemonTokenEnv names the env var that sets the daemon's auth token, so a
// program that starts the daemon knows the token without reading the port file.
const daemonTokenEnv = "BINO_DAEMON_TOKEN" //nolint:gosec // G101: env var name, not a credential

// watchIdle calls onIdle once active has reported zero on every check for
// longer than timeout. It returns after onIdle or when ctx is done.
func watchIdle(ctx context.Context, interval, timeout time.Duration, active func() int, onIdle func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastActive := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if active() > 0 {
				lastActive = time.Now()
				continue
			}
			if time.Since(lastActive) > timeout {
				onIdle()
				return
			}
		}
	}
}

func newDaemonCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity - refactor before extending
	var (
		port            int
		workdir         string
		listenAddr      string
		mcpEnabled      bool
		mcpAllowPublish bool
		readOnly        bool
	)

	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run persistent background daemon for IDE integration",
		Long:   "Runs an HTTP daemon that serves workspace index, validation, and data introspection to IDE extensions.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("daemon")
			// The editor that spawned the daemon may exit while other clients
			// still use it; a log write to its closed pipe must not kill us.
			signal.Ignore(syscall.SIGPIPE)

			// Without a port file the env token is the only way to learn it.
			if readOnly && os.Getenv(daemonTokenEnv) == "" {
				return ConfigErrorf("--read-only needs the auth token in %s", daemonTokenEnv)
			}

			if readOnly {
				dataset.DisableProjectCache()
			}

			env, err := initEditorCommandEnv(ctx, cmd, workdir, "preview", logger)
			if err != nil {
				return err
			}

			logger.Infof("Starting daemon for %s", env.ProjectRoot)

			// Check for existing daemon. A read-only daemon writes no port file,
			// so it neither registers itself nor collides with an editor's daemon.
			existing, _ := daemon.ReadPortFile(env.ProjectRoot) //nolint:errcheck // a missing or unreadable port file means no daemon is running
			if existing != nil && !readOnly {
				return ConfigErrorf("daemon already running on port %d (pid %d)", existing.Port, existing.PID)
			}

			// Create the shared session + state (also used by `bino mcp` standalone).
			managedCfg := daemon.ManagedStateConfig{
				ProjectRoot:  env.ProjectRoot,
				Logger:       logger,
				EngineCompat: engineCompatDiagnostic,
			}
			if env.PluginRegistry != nil {
				managedCfg.KindProvider = env.PluginRegistry
				managedCfg.PluginLinters = plugin.NewLinterRegistry(env.PluginRegistry)
			}
			if env.PluginLoadErr != nil {
				managedCfg.ConfigDiagnostics = []LSPDiagnostic{pluginLoadDiagnostic(env.ProjectRoot, env.PluginLoadErr)}
			}
			managed, err := daemon.NewManagedState(ctx, managedCfg)
			if err != nil {
				return RuntimeError(err)
			}
			defer managed.Close()
			state := managed.State

			// Initial refresh
			if err := state.Refresh(ctx); err != nil {
				logger.Errorf("Initial refresh failed: %v", err)
			}

			// Build the MCP handler (Streamable HTTP) sharing this State. Mounted
			// at /mcp on the daemon's listener by default, loopback-only. An
			// attached MCP session (open Streamable-HTTP request) counts as
			// activity for the idle-shutdown check below.
			var mcpHandler http.Handler
			var mcpActive func() int64
			var server *daemon.Server
			// One preview subprocess for the /preview routes and the MCP preview tools.
			preview := daemon.NewPreviewProcess(env.ProjectRoot, logger.Channel("server"))
			if mcpEnabled {
				deps := mcp.Deps{
					State:        state,
					Registry:     env.PluginRegistry,
					Authoring:    newCLIAuthoring(env.ProjectRoot, env.PluginRegistry),
					Packages:     newCLIPackages(state),
					AllowPublish: mcpAllowPublish,
					Preview:      preview,
					ReadOnly:     readOnly,
					// server is assigned below; the callback only runs once a
					// tool call has gone through the mounted handler.
					RegistryChanged: func() {
						server.BroadcastEvent("registry-changed", map[string]any{"reasons": []string{"mcp"}})
					},
				}
				inner := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
					return mcp.NewServer(deps)
				}, nil)
				cc := &connCounter{inner: inner}
				mcpHandler = cc
				mcpActive = cc.active
			}

			// Create HTTP server. The listen address defaults to 127.0.0.1 (localhost
			// only) to match the original local-IDE assumption; --listen-addr=0.0.0.0
			// is the explicit opt-in needed when the daemon runs inside a container and
			// must be reachable from another container on a private docker network.
			fullListenAddr := fmt.Sprintf("%s:%d", listenAddr, port)

			if env.PluginManager != nil {
				defer env.PluginManager.ShutdownAll(ctx)
			}

			server, err = daemon.NewServer(daemon.ServerConfig{
				ListenAddr:     fullListenAddr,
				State:          state,
				Logger:         logger.Channel("server"),
				PluginRegistry: env.PluginRegistry,
				MCPHandler:     mcpHandler,
				Preview:        preview,
				Token:          os.Getenv(daemonTokenEnv),
				ReadOnly:       readOnly,
			})
			if err != nil {
				return RuntimeError(err)
			}

			if !readOnly {
				if err := daemon.WritePortFile(env.ProjectRoot, server.Port(), server.Token()); err != nil {
					return RuntimeError(err)
				}
				defer daemon.RemovePortFile(env.ProjectRoot)
			}
			defer preview.Shutdown(5 * time.Second)

			logger.Infof("Daemon listening on %s:%d", listenAddr, server.Port())
			if mcpEnabled {
				logger.Infof("MCP endpoint at http://%s:%d/mcp", listenAddr, server.Port())
			}

			// Create a cancellable context for the server
			serverCtx, serverCancel := context.WithCancel(ctx)
			defer serverCancel()

			// Start file watcher; broadcast index/diagnostics updates to SSE clients
			// after each refresh.
			if err := managed.Watch(ctx, func(st *daemon.State, reasons []string) {
				server.BroadcastEvent("index-updated", map[string]any{
					"reason":    daemon.CoalesceReasons(reasons),
					"documents": len(st.Documents()),
				})
				diags := st.Diagnostics()
				server.BroadcastEvent("diagnostics", map[string]any{
					"valid":       len(diags) == 0,
					"diagnostics": diags,
				})
				if daemon.RegistryReason(reasons) {
					server.BroadcastEvent("registry-changed", map[string]any{"reasons": reasons})
				}
			}); err != nil {
				return RuntimeError(err)
			}

			// A read-only daemon is started and stopped by its host, which
			// may call it with short stateless requests that the idle check
			// never sees; only an editor's daemon exits on its own.
			if !readOnly {
				// Exit once no SSE client and no MCP session has been attached for
				// defaultInactivityTimeout, so a daemon nobody uses does not linger.
				go watchIdle(serverCtx, 30*time.Second, defaultInactivityTimeout, func() int {
					active := server.ClientCount()
					if mcpActive != nil {
						active += int(mcpActive())
					}
					return active
				}, func() {
					logger.Infof("No active clients for %v, shutting down", defaultInactivityTimeout)
					server.BroadcastEvent("shutdown", map[string]string{
						"reason": "inactivity timeout",
					})
					server.RequestShutdown()
				})
			}

			// Start HTTP server in background
			serverErrCh := make(chan error, 1)
			go func() {
				serverErrCh <- server.Start(serverCtx)
			}()

			logger.Infof("Daemon ready * press Ctrl+C to stop")

			// Block until context canceled, shutdown requested, or server error
			select {
			case <-ctx.Done():
				// External cancellation (SIGTERM/SIGINT)
			case <-server.ShutdownCh():
				logger.Infof("Shutdown requested")
				serverCancel()
			case err := <-serverErrCh:
				return err
			}

			return <-serverErrCh
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Port to listen on (default: ephemeral)")
	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory (project root)")
	cmd.Flags().StringVar(&listenAddr, "listen-addr", "127.0.0.1", "Address to listen on (use 0.0.0.0 to accept connections from a container network)")
	cmd.Flags().BoolVar(&mcpEnabled, "mcp", true, "Mount the MCP server at /mcp (Streamable HTTP); use --mcp=false to disable")
	cmd.Flags().BoolVar(&mcpAllowPublish, "mcp-allow-publish", false, "Expose the registry_publish MCP tool (irreversible: mints an immutable, possibly public, registry version)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Serve only tools and routes that change nothing: no authoring, registry, build, layout, publish, preview or source probe; no port file")
	cmd.MarkFlagsMutuallyExclusive("read-only", "mcp-allow-publish")

	return cmd
}
