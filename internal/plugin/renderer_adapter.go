package plugin

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

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
	// warned holds kind + "\x00" + check for the fragment warnings already
	// given, so each shows once per kind.
	warned sync.Map
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
	prefix := fmt.Sprintf("plugin %s: %s: ", reg.PluginName, req.Kind)
	for _, w := range fragmentWarnings(res.HTML) {
		if _, seen := r.warned.LoadOrStore(req.Kind+"\x00"+w.check, true); !seen {
			warnings = append(warnings, prefix+w.message)
		}
	}
	return res.HTML, warnings, nil
}

var (
	fragmentRootTag = regexp.MustCompile(`^<([A-Za-z][^\s/>]*)`)
	scriptOpenTag   = regexp.MustCompile(`(?is)<script\b([^>]*)>`)
	scriptTypeAttr  = regexp.MustCompile(`(?is)\btype\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

type fragmentWarning struct{ check, message string }

// fragmentWarnings finds the front-end mistakes the host can see in a
// fragment: a reserved root tag and an inline script that preview never runs.
func fragmentWarnings(html string) []fragmentWarning {
	var out []fragmentWarning
	if m := fragmentRootTag.FindStringSubmatch(strings.TrimSpace(html)); m != nil {
		tag := strings.ToLower(m[1])
		for _, reserved := range []string{"bn-", "bino-"} {
			if strings.HasPrefix(tag, reserved) {
				out = append(out, fragmentWarning{"root", fmt.Sprintf("root element <%s> uses the reserved %s prefix; use a tag that starts with the plugin name", tag, reserved)})
			}
		}
	}
	for _, m := range scriptOpenTag.FindAllStringSubmatch(html, -1) {
		if !isJSONScript(m[1]) {
			out = append(out, fragmentWarning{"script", "the HTML contains an inline <script>; it runs in bino build but not in bino preview, ship JS through GetAssets"})
			break
		}
	}
	return out
}

func isJSONScript(attrs string) bool {
	m := scriptTypeAttr.FindStringSubmatch(attrs)
	if m == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m[1]+m[2]+m[3]), "application/json")
}
