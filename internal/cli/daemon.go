package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/watchers"
	"bino.bi/bino/pkg/duckdb"
)

const defaultInactivityTimeout = 5 * time.Minute

func newDaemonCommand() *cobra.Command {
	var (
		port    int
		workdir string
	)

	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run persistent background daemon for IDE integration",
		Long:   "Runs an HTTP daemon that serves workspace index, validation, and data introspection to IDE extensions.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("daemon")

			env, err := initCommandEnv(ctx, cmd, workdir, "preview", logger)
			if err != nil {
				return err
			}

			logger.Infof("Starting daemon for %s", env.ProjectRoot)

			// Check for existing daemon
			existing, _ := daemon.ReadPortFile(env.ProjectRoot)
			if existing != nil {
				return ConfigErrorf("daemon already running on port %d (pid %d)", existing.Port, existing.PID)
			}

			// Create shared DuckDB session
			duckdbOpts, err := duckdb.DefaultOptions()
			if err != nil {
				return RuntimeError(err)
			}
			sharedSession, err := duckdb.OpenSession(ctx, duckdbOpts)
			if err != nil {
				return RuntimeError(err)
			}
			defer sharedSession.Close()

			if err := sharedSession.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
				return RuntimeError(err)
			}

			// Create daemon state
			state, err := daemon.NewState(env.ProjectRoot, sharedSession, logger)
			if err != nil {
				return RuntimeError(err)
			}
			defer state.Close()

			// Initial refresh
			if err := state.Refresh(ctx); err != nil {
				logger.Errorf("Initial refresh failed: %v", err)
			}

			// Create HTTP server
			listenAddr := "127.0.0.1:0"
			if port > 0 {
				listenAddr = fmt.Sprintf("127.0.0.1:%d", port)
			}

			server, err := daemon.NewServer(daemon.ServerConfig{
				ListenAddr: listenAddr,
				State:      state,
				Logger:     logger.Channel("server"),
			})
			if err != nil {
				return RuntimeError(err)
			}

			// Write port file
			if err := daemon.WritePortFile(env.ProjectRoot, server.Port()); err != nil {
				return RuntimeError(err)
			}
			defer daemon.RemovePortFile(env.ProjectRoot)
			defer server.StopPreview()

			logger.Infof("Daemon listening on 127.0.0.1:%d", server.Port())

			// Create a cancellable context for the server
			serverCtx, serverCancel := context.WithCancel(ctx)
			defer serverCancel()

			// Start file watcher
			refreshCh := make(chan string, 16)
			enqueue := func(reason string) {
				select {
				case refreshCh <- reason:
				default:
				}
			}

			watchLog := logger.Channel("watcher")
			watcher, err := watchers.NewWatcher(watchers.Config{
				Root:   env.ProjectRoot,
				Logger: watchLog,
				Handler: func(evt watchers.Event) {
					watchLog.Infof("File updated %s (%s)", evt.RelativePath, evt.Op)
					enqueue(fmt.Sprintf("change %s", evt.RelativePath))
				},
			})
			if err != nil {
				return RuntimeError(err)
			}
			defer watcher.Close()
			go watcher.Run(ctx)

			// Debounced refresh goroutine
			go func() {
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
						coalesced := coalesceRefreshReasons(reasons)
						reasons = reasons[:0]
						if err := state.Refresh(ctx); err != nil {
							logger.Errorf("Refresh failed: %v", err)
							continue
						}
						logger.Infof("Refreshed (%s)", coalesced)

						// Broadcast updates to SSE clients
						server.BroadcastEvent("index-updated", map[string]any{
							"reason":    coalesced,
							"documents": len(state.Documents()),
						})
						diags := state.Diagnostics()
						server.BroadcastEvent("diagnostics", map[string]any{
							"valid":       len(diags) == 0,
							"diagnostics": diags,
						})
					}
				}
			}()

			// Inactivity timeout goroutine
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				lastActive := time.Now()

				for {
					select {
					case <-serverCtx.Done():
						return
					case <-ticker.C:
						if server.ClientCount() > 0 {
							lastActive = time.Now()
							continue
						}
						if time.Since(lastActive) > defaultInactivityTimeout {
							logger.Infof("No SSE clients for %v, shutting down", defaultInactivityTimeout)
							server.BroadcastEvent("shutdown", map[string]string{
								"reason": "inactivity timeout",
							})
							server.RequestShutdown()
							return
						}
					}
				}
			}()

			// Start HTTP server in background
			serverErrCh := make(chan error, 1)
			go func() {
				serverErrCh <- server.Start(serverCtx)
			}()

			logger.Infof("Daemon ready * press Ctrl+C to stop")

			// Block until context cancelled, shutdown requested, or server error
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

	return cmd
}

func coalesceRefreshReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "unknown"
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	return fmt.Sprintf("%s (+%d more)", reasons[0], len(reasons)-1)
}

