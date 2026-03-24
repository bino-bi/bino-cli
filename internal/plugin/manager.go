package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// Manager handles plugin discovery, launching, and lifecycle.
type Manager struct {
	registry    *PluginRegistry
	clients     []*goplugin.Client // track for shutdown
	logger      logx.Logger
	verbose     bool
	hostService *BinoHostServer // shared host service for bidirectional calls
}

// NewManager creates a new plugin Manager.
func NewManager(logger logx.Logger) *Manager {
	hostService := NewBinoHostServer()
	return &Manager{
		registry:    NewRegistry(),
		logger:      logger,
		hostService: hostService,
	}
}

// HostService returns the shared BinoHost service.
// Callers use this to update documents, datasets, and DuckDB state
// as the pipeline progresses.
func (m *Manager) HostService() *BinoHostServer {
	return m.hostService
}

// SetVerbose enables verbose go-plugin logging (stderr passthrough).
func (m *Manager) SetVerbose(v bool) {
	m.verbose = v
}

// Registry returns the plugin registry (available after LoadAll).
func (m *Manager) Registry() *PluginRegistry {
	return m.registry
}

// LoadAll discovers, launches, and initializes all declared plugins.
// Plugins are loaded in alphabetical order by name for deterministic hook chaining.
// Returns an error if any declared plugin fails to load.
func (m *Manager) LoadAll(ctx context.Context, cfg *pathutil.ProjectConfig, projectRoot string, binoVersion string) error {
	if len(cfg.Plugins) == 0 {
		return nil
	}

	// Sort plugin names for deterministic order.
	names := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		decl := cfg.Plugins[name]
		if err := m.loadPlugin(ctx, name, decl, projectRoot, binoVersion); err != nil {
			m.ShutdownAll(ctx)
			return fmt.Errorf("loading plugin %q: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) loadPlugin(ctx context.Context, name string, decl pathutil.PluginDeclaration, projectRoot string, binoVersion string) error {
	// 1. Discover binary.
	binaryPath, err := DiscoverBinary(name, decl.Path, projectRoot)
	if err != nil {
		return err
	}
	m.logger.Debugf("discovered plugin %q at %s", name, binaryPath)

	// 2. Launch via go-plugin.
	hcLogger := hclog.NewNullLogger()
	var stderr *os.File
	if m.verbose {
		hcLogger = hclog.New(&hclog.LoggerOptions{
			Name:   "plugin." + name,
			Level:  hclog.Debug,
			Output: os.Stderr,
		})
		stderr = os.Stderr
	}

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          pluginMapWithHost(m.hostService),
		Cmd:              exec.CommandContext(ctx, binaryPath),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hcLogger,
		StartTimeout:     15 * time.Second,
		Stderr:           stderr,
	})
	m.clients = append(m.clients, client)

	// 3. Connect (launches the subprocess).
	rpcClient, err := client.Client()
	if err != nil {
		return fmt.Errorf("connecting to plugin: %w", err)
	}

	// 4. Get gRPC stub and host service ID from the dispensed client.
	raw, err := rpcClient.Dispense("bino")
	if err != nil {
		return fmt.Errorf("dispensing gRPC client: %w", err)
	}
	cwh, ok := raw.(*clientWithHost)
	if !ok {
		return fmt.Errorf("unexpected client type: %T", raw)
	}

	// 5. Call Init with host service broker ID.
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	manifest, err := cwh.client.Init(initCtx, &pluginv1.InitRequest{
		Config:        decl.Config,
		ProjectRoot:   projectRoot,
		BinoVersion:   binoVersion,
		HostServiceId: cwh.hostServiceID,
	})
	if err != nil {
		return fmt.Errorf("plugin Init: %w", err)
	}

	// 6. Wrap in grpcClient adapter.
	p := &grpcClient{
		client:   cwh.client,
		manifest: manifestFromProto(manifest),
		process:  client,
	}

	// 7. Register in registry.
	m.registry.Register(p)

	m.logger.Infof("loaded plugin %q v%s (%d kinds)", p.manifest.Name, p.manifest.Version, len(p.manifest.Kinds))
	return nil
}

// ShutdownAll gracefully shuts down all loaded plugins via Shutdown RPC, then kills processes.
func (m *Manager) ShutdownAll(ctx context.Context) {
	for _, p := range m.registry.AllPlugins() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := p.Shutdown(shutdownCtx); err != nil {
			m.logger.Warnf("plugin %q shutdown error: %v", p.Manifest().Name, err)
		}
		cancel()
	}
	m.KillAll()
}

// KillAll kills all plugin processes in the background and returns immediately.
// go-plugin's Kill() can block waiting for I/O drain, so we fire-and-forget.
// When the host process exits, orphaned plugin processes detect it and self-terminate.
func (m *Manager) KillAll() {
	for _, c := range m.clients {
		go c.Kill()
	}
}
