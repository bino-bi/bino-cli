package plugin

import "context"

// Plugin is the interface to a loaded, running plugin process.
// The gRPC client adapter (Task 04) implements this interface.
type Plugin interface {
	// Manifest returns the plugin's manifest (set during Init).
	Manifest() PluginManifest

	// GetSchemas returns JSON Schema definitions for plugin kinds.
	// Map key is kind name, value is JSON Schema bytes.
	GetSchemas(ctx context.Context) (map[string][]byte, error)

	// CollectDataSource executes a plugin-provided datasource.
	CollectDataSource(ctx context.Context, name string, rawSpec []byte, env map[string]string, projectRoot string) (*CollectResult, error)

	// Lint runs plugin lint rules against the given documents.
	// The opts parameter provides optional enriched context (datasets, rendered HTML).
	Lint(ctx context.Context, docs []DocumentPayload, opts *LintOptions) ([]LintFinding, error)

	// GetAssets returns JS/CSS assets for the given render mode.
	GetAssets(ctx context.Context, renderMode string) (scripts []AssetFile, styles []AssetFile, err error)

	// ListCommands returns CLI subcommand descriptors.
	ListCommands(ctx context.Context) ([]CommandDescriptor, error)

	// ExecCommand executes a plugin CLI command, streaming output.
	// The callback receives stdout/stderr chunks. Returns the exit code.
	ExecCommand(ctx context.Context, command string, args []string, flags map[string]string, workdir string, output func(stdout, stderr []byte)) (int, error)

	// OnHook dispatches a pipeline hook to the plugin.
	OnHook(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error)

	// RenderComponent generates HTML for a plugin-provided component kind.
	// Returns the HTML fragment to insert into the layout.
	RenderComponent(ctx context.Context, kind, name string, spec []byte, renderMode string) (string, error)

	// Shutdown gracefully terminates the plugin.
	Shutdown(ctx context.Context) error
}
