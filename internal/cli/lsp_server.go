package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/lsp"
)

// newLSPServerCommand runs the bino Language Server over stdio. It mirrors
// `bino mcp`: when a daemon is running for the project it proxies to it (so the
// daemon stays the single heavy instance); otherwise it serves standalone over
// its own ManagedState. The editor always speaks plain stdio LSP.
func newLSPServerCommand() *cobra.Command {
	var (
		workdir string
		noProxy bool
		socket  int
		phase2  bool
		stdio   bool
	)

	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Run a Language Server (LSP) over stdio for bino manifests",
		Long: `Runs a Language Server Protocol server over stdio, giving any LSP-capable
editor (VS Code, Neovim, Helix, ...) context-aware completion, hover, and live
diagnostics for bino manifests.

When a bino daemon is already running for the project (e.g. VS Code is open),
this command proxies to that daemon's HTTP routes so the editor reuses the
already-loaded DuckDB session and file watcher instead of starting a second one.
With no daemon running, it serves standalone from its own project state.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// stdout is the JSON-RPC channel — route ALL logging to stderr.
			verbose := logx.DebugEnabled(cmd.Context())
			logger := logx.NewTerminalWithColor(cmd.ErrOrStderr(), cmd.ErrOrStderr(), verbose, true).Channel("lsp")
			ctx := logx.WithLogger(cmd.Context(), logger)

			projectRoot, initialized, err := resolveMCPRoot(workdir)
			if err != nil {
				return ConfigError(err)
			}
			if !initialized {
				logger.Infof("No bino.toml found; serving LSP rooted at %s", projectRoot)
			}

			var backend lsp.Backend
			if !noProxy {
				if pf, _ := daemon.ReadPortFile(projectRoot); pf != nil {
					base := fmt.Sprintf("http://127.0.0.1:%d", pf.Port)
					if hb, herr := lsp.NewHTTPBackend(ctx, base, logger); herr == nil {
						logger.Infof("Proxying to daemon at %s", base)
						backend = hb
					} else {
						logger.Warnf("Proxy to daemon failed (%v); falling back to standalone", herr)
					}
				}
			}
			if backend == nil {
				managed, reg, cleanup, lerr := loadStandaloneState(ctx, logger, projectRoot)
				if lerr != nil {
					return RuntimeError(lerr)
				}
				backend = newLSPInProcessBackend(managed, reg, cleanup)
				logger.Infof("bino LSP server ready (standalone) for %s", projectRoot)
			}

			if err := backend.Start(ctx); err != nil {
				logger.Warnf("backend start: %v", err)
			}
			defer func() { _ = backend.Close() }()

			stream := lsp.StdioStream()
			if socket > 0 {
				s, serr := lsp.SocketStream(ctx, socket)
				if serr != nil {
					return RuntimeError(serr)
				}
				stream = s
			}

			srv := lsp.NewServer(backend, logger, phase2, projectRoot)
			return srv.Serve(ctx, stream)
		},
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory (project root)")
	cmd.Flags().BoolVar(&noProxy, "no-proxy", false, "Always run standalone, even if a daemon is running")
	cmd.Flags().IntVar(&socket, "socket", 0, "Serve over TCP on this port instead of stdio (debugging)")
	cmd.Flags().BoolVar(&phase2, "phase2", true, "Advertise navigation/refactor capabilities (definition, references, rename, ...)")
	// `--stdio` is a no-op accepted for compatibility: LSP clients (e.g.
	// vscode-languageclient with TransportKind.stdio) append it by convention to
	// tell the server which transport to use. stdio is already the default.
	cmd.Flags().BoolVar(&stdio, "stdio", false, "Use stdio transport (default; accepted for LSP-client compatibility)")
	_ = stdio
	return cmd
}
