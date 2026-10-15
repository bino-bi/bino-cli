package plugin

import (
	"context"
	"fmt"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/render"
)

// ComponentRenderer renders HTML for plugin-provided component kinds.
// This interface is used by the render package to delegate custom kind rendering.
type ComponentRenderer interface {
	RenderComponent(ctx context.Context, req render.PluginRenderRequest) (html string, warnings []string, err error)
}

// NewComponentRenderer creates a ComponentRenderer backed by the given PluginRegistry.
func NewComponentRenderer(registry *PluginRegistry) ComponentRenderer {
	return &registryComponentRenderer{registry: registry}
}

type registryComponentRenderer struct {
	registry *PluginRegistry
}

// RenderComponent renders through the plugin that owns req.Kind. Warning and
// error diagnostics become warnings, info diagnostics are only logged at
// debug level. Diagnostics never fail the render; only a returned error does.
func (r *registryComponentRenderer) RenderComponent(ctx context.Context, req render.PluginRenderRequest) (html string, warnings []string, err error) {
	// Look up which plugin owns this kind.
	reg, ok := r.registry.GetKindRegistration(req.Kind)
	if !ok {
		return "", nil, fmt.Errorf("unknown plugin kind %q", req.Kind)
	}

	p, ok := r.registry.Get(reg.PluginName)
	if !ok {
		return "", nil, fmt.Errorf("plugin %q not loaded", reg.PluginName)
	}

	res, err := p.RenderComponent(ctx, req)
	if err != nil {
		return "", nil, err
	}

	for _, d := range res.Diagnostics {
		source := d.Source
		if source == "" {
			source = reg.PluginName
		}
		prefix := fmt.Sprintf("plugin %s: %s %q: ", source, req.Kind, req.Name)
		switch d.Severity {
		case SeverityError:
			warnings = append(warnings, prefix+"error: "+d.Message)
		case SeverityInfo:
			logx.FromContext(ctx).Debugf("%s%s", prefix, d.Message)
		default:
			warnings = append(warnings, prefix+d.Message)
		}
	}
	return res.HTML, warnings, nil
}
