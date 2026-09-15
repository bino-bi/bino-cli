package walk

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// KindDocMarkdown renders a kind's description plus its required spec fields.
func KindDocMarkdown(m *Model, kind string) string {
	desc, req := m.KindDoc(kind)
	if desc == "" && len(req) == 0 {
		return ""
	}
	out := desc
	if len(req) > 0 {
		if out != "" {
			out += "\n\n"
		}
		out += "Required: `" + strings.Join(req, "`, `") + "`"
	}
	return out
}

// ScaffoldAPIVersion reads the schema's apiVersion const/enum, with the
// current version as fallback.
func ScaffoldAPIVersion(m *Model) string {
	node := m.ResolveAt([]string{"apiVersion"}, nil)
	if vals := node.EnumValues(); len(vals) > 0 {
		return vals[0]
	}
	return "bino.bi/v1alpha1"
}

// ScaffoldBody renders a kind's full-document template. Tabstops cover
// metadata.name and every required spec field; defaults are pre-filled and
// enums become snippet choices. With snippets false a plain-text body is
// rendered instead.
func ScaffoldBody(m *Model, kind, apiVersion string, snippets bool) string {
	var b strings.Builder
	b.WriteString("apiVersion: " + apiVersion + "\n")
	b.WriteString("kind: " + kind + "\n")
	b.WriteString("metadata:\n")
	if snippets {
		b.WriteString("  name: ${1:name}\n")
	} else {
		b.WriteString("  name: name\n")
	}
	b.WriteString("spec:")
	var required []PropInfo
	for _, p := range m.ResolveAt([]string{"spec"}, map[string]string{"": kind}).Props() {
		if p.Required {
			required = append(required, p)
		}
	}
	sort.Slice(required, func(i, j int) bool { return required[i].Name < required[j].Name })
	if len(required) == 0 {
		if snippets {
			b.WriteString("\n  $2")
		} else {
			b.WriteString(" {}")
		}
		return b.String()
	}
	tab := 2
	for _, p := range required {
		b.WriteString("\n  " + p.Name + ": " + scaffoldValue(p, tab, snippets))
		tab++
	}
	return b.String()
}

// scaffoldValue renders one required field's placeholder.
func scaffoldValue(p PropInfo, tab int, snippets bool) string {
	if !snippets {
		switch {
		case p.Default != nil:
			return RenderDefault(p.Default)
		case len(p.Enum) > 0:
			return p.Enum[0]
		default:
			return ""
		}
	}
	n := strconv.Itoa(tab)
	switch {
	case p.Default != nil:
		return "${" + n + ":" + RenderDefault(p.Default) + "}"
	case len(p.Enum) > 0:
		return "${" + n + "|" + strings.Join(p.Enum, ",") + "|}"
	default:
		return "${" + n + "}"
	}
}

// RenderDefault renders a schema default for display (strings unquoted).
func RenderDefault(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
