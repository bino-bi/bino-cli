package plugin

import (
	"strings"
	"sync"
)

// PluginRegistry holds all loaded plugins and provides lookup by capability.
// It is safe for concurrent reads after initialization.
//
//nolint:revive // name used widely across codebase
type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin           // name → Plugin
	kinds   map[string]KindRegistration // kind_name → registration
	order   []string                    // plugin names in declaration order (for hook chaining)
}

// NewRegistry creates an empty PluginRegistry.
func NewRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]Plugin),
		kinds:   make(map[string]KindRegistration),
	}
}

// Register adds a plugin and indexes its capabilities.
// If a kind name is already registered by another plugin, the later registration wins.
func (r *PluginRegistry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m := p.Manifest()
	r.plugins[m.Name] = p
	r.order = append(r.order, m.Name)

	for _, k := range m.Kinds {
		reg := k
		reg.PluginName = m.Name
		r.kinds[k.KindName] = reg
	}
}

// Get returns a plugin by name.
func (r *PluginRegistry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.plugins[name]
	return p, ok
}

// GetKindRegistration returns the KindRegistration for a given kind name.
func (r *PluginRegistry) GetKindRegistration(kindName string) (KindRegistration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	k, ok := r.kinds[kindName]
	return k, ok
}

// AllKinds returns all registered plugin kinds.
func (r *PluginRegistry) AllKinds() []KindRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	kinds := make([]KindRegistration, 0, len(r.kinds))
	for _, k := range r.kinds {
		kinds = append(kinds, k)
	}
	return kinds
}

// AllPlugins returns all plugins in declaration order.
func (r *PluginRegistry) AllPlugins() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugins := make([]Plugin, 0, len(r.order))
	for _, name := range r.order {
		plugins = append(plugins, r.plugins[name])
	}
	return plugins
}

// DataSourcePlugin returns the plugin responsible for a given DataSource kind.
// Returns nil, false if this kind is not a plugin DataSource.
func (r *PluginRegistry) DataSourcePlugin(kindName string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	k, ok := r.kinds[kindName]
	if !ok || k.Category != KindCategoryDataSource {
		return nil, false
	}
	p, ok := r.plugins[k.PluginName]
	return p, ok
}

// PluginsWithLinter returns all plugins that provide lint rules, in declaration order.
func (r *PluginRegistry) PluginsWithLinter() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, name := range r.order {
		p := r.plugins[name]
		if p.Manifest().ProvidesLinter {
			result = append(result, p)
		}
	}
	return result
}

// PluginsWithAssets returns all plugins that provide JS/CSS assets, in declaration order.
func (r *PluginRegistry) PluginsWithAssets() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, name := range r.order {
		p := r.plugins[name]
		if p.Manifest().ProvidesAssets {
			result = append(result, p)
		}
	}
	return result
}

// PluginsForHook returns plugins subscribed to a given hook checkpoint, in declaration order.
func (r *PluginRegistry) PluginsForHook(checkpoint string) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, name := range r.order {
		p := r.plugins[name]
		for _, h := range p.Manifest().Hooks {
			if h == checkpoint {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// PluginKindNames returns all kind names contributed by plugins.
// Used by validate.go to extend the uniqueNameKinds map.
func (r *PluginRegistry) PluginKindNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.kinds))
	for name := range r.kinds {
		names = append(names, name)
	}
	return names
}

// DuckDBExtensions returns the deduplicated union of all plugin DuckDB extensions.
func (r *PluginRegistry) DuckDBExtensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{})
	var result []string
	for _, name := range r.order {
		p := r.plugins[name]
		for _, ext := range p.Manifest().DuckDBExtensions {
			if _, ok := seen[ext]; !ok {
				seen[ext] = struct{}{}
				result = append(result, ext)
			}
		}
	}
	return result
}

// CategorizeKind determines the KindCategory for a kind name.
// Checks the registry first, then falls back to suffix-based detection.
func (r *PluginRegistry) CategorizeKind(kindName string) KindCategory {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if k, ok := r.kinds[kindName]; ok {
		return k.Category
	}

	// Suffix-based fallback for built-in kinds.
	switch {
	case strings.HasSuffix(kindName, "DataSource"):
		return KindCategoryDataSource
	case strings.HasSuffix(kindName, "Artefact"), strings.HasSuffix(kindName, "Artifact"): //nolint:misspell // backward-compat string match
		return KindCategoryArtifact
	default:
		return KindCategoryComponent
	}
}
