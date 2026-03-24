package plugin

import (
	"context"
	"fmt"
)

// ComponentRenderer renders HTML for plugin-provided component kinds.
// This interface is used by the render package to delegate custom kind rendering.
type ComponentRenderer interface {
	RenderComponent(ctx context.Context, kind, name string, spec []byte, renderMode string) (string, error)
}

// NewComponentRenderer creates a ComponentRenderer backed by the given PluginRegistry.
func NewComponentRenderer(registry *PluginRegistry) ComponentRenderer {
	return &registryComponentRenderer{registry: registry}
}

type registryComponentRenderer struct {
	registry *PluginRegistry
}

func (r *registryComponentRenderer) RenderComponent(ctx context.Context, kind, name string, spec []byte, renderMode string) (string, error) {
	// Look up which plugin owns this kind.
	reg, ok := r.registry.GetKindRegistration(kind)
	if !ok {
		return "", fmt.Errorf("unknown plugin kind %q", kind)
	}

	p, ok := r.registry.Get(reg.PluginName)
	if !ok {
		return "", fmt.Errorf("plugin %q not loaded", reg.PluginName)
	}

	return p.RenderComponent(ctx, kind, name, spec, renderMode)
}
