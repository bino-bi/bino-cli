package plugin

import (
	"context"
	"os"

	"bino.bi/bino/internal/report/datasource"
)

// NewPluginRouter creates a datasource.PluginRouter backed by the given PluginRegistry.
// The projectRoot is passed to plugins during collection.
func NewPluginRouter(registry *PluginRegistry, projectRoot string) datasource.PluginRouter {
	return &registryPluginRouter{registry: registry, projectRoot: projectRoot}
}

type registryPluginRouter struct {
	registry    *PluginRegistry
	projectRoot string
}

func (r *registryPluginRouter) DataSourcePlugin(kindName string) (datasource.PluginCollector, bool) {
	p, ok := r.registry.DataSourcePlugin(kindName)
	if !ok {
		return nil, false
	}
	return &pluginCollectorAdapter{plugin: p, projectRoot: r.projectRoot}, true
}

type pluginCollectorAdapter struct {
	plugin      Plugin
	projectRoot string
}

func (a *pluginCollectorAdapter) CollectDataSource(ctx context.Context, name string, rawSpec []byte, env map[string]string, projectRoot string) (*datasource.PluginCollectResult, error) {
	if projectRoot == "" {
		projectRoot = a.projectRoot
	}
	if env == nil {
		env = envMap()
	}
	result, err := a.plugin.CollectDataSource(ctx, name, rawSpec, env, projectRoot)
	if err != nil {
		return nil, err
	}
	return &datasource.PluginCollectResult{
		JSONRows:         result.JSONRows,
		ColumnTypes:      result.ColumnTypes,
		Ephemeral:        result.Ephemeral,
		DuckDBExpression: result.DuckDBExpression,
	}, nil
}

// envMap returns the current environment as a string map.
func envMap() map[string]string {
	m := make(map[string]string)
	for _, e := range os.Environ() {
		for i := range e {
			if e[i] == '=' {
				m[e[:i]] = e[i+1:]
				break
			}
		}
	}
	return m
}
