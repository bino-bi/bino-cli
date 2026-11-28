package datacheck

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
)

// optionalColumns are columns the graph lists although the engine renders
// without them.
var optionalColumns = map[string]bool{"setnameIndex": true}

// read is one column a component reads, with every dataset it may come from.
type read struct {
	column, role, field string
	bindings            []string
}

// checkComponentColumns reports the columns a component reads that its
// DataSet does not have. Scenario and variance slots of tables and charts
// are scenario-column findings, every other column a dataset-column finding,
// so a missing column is reported once.
func (c *checker) checkComponentColumns(ctx context.Context, g *graph.Graph) {
	ids := make([]string, 0, len(g.Nodes))
	for id, n := range g.Nodes {
		if n.Kind == graph.NodeComponent && len(n.Columns) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := g.Nodes[id]
		for _, r := range componentReads(node.Columns) {
			if ctx.Err() != nil {
				return
			}
			c.checkRead(ctx, node, r)
		}
	}
}

// componentReads groups a node's refs by column, so a column bound to two
// datasets is one read that either may satisfy.
func componentReads(refs []graph.ColumnRef) []read {
	var out []read
	index := map[[3]string]int{}
	for _, r := range refs {
		col := strings.TrimSpace(r.Column)
		if r.Role == graph.RoleImplicit || r.Field == "" || col == "" || strings.EqualFold(col, graph.ColumnAuto) ||
			strings.Contains(col, "${") || strings.HasPrefix(col, "inherited-") || optionalColumns[col] {
			continue
		}
		k := [3]string{col, r.Role, r.Field}
		i, ok := index[k]
		if !ok {
			i = len(out)
			index[k] = i
			out = append(out, read{column: col, role: r.Role, field: r.Field})
		}
		out[i].bindings = append(out[i].bindings, strings.TrimSpace(r.Dataset))
	}
	return out
}

func (c *checker) checkRead(ctx context.Context, node *graph.Node, r read) {
	var sets []columnSet
	for _, b := range r.bindings {
		set, ok := c.bindingColumns(ctx, b)
		if !ok {
			return // unknown columns: the binding failed or was not checked
		}
		if set[r.column] {
			return
		}
		sets = append(sets, set)
	}

	doc, path, token := c.locate(node, r)
	if doc == nil {
		return
	}
	rule := lint.RuleDatasetColumn
	if r.role == graph.RoleScenario || r.role == graph.RoleVariance {
		rule = lint.RuleScenarioColumn
	}
	what := fmt.Sprintf("%s reads column %q", r.field, r.column)
	if rule == lint.RuleScenarioColumn && token != "" && token != r.column {
		what = fmt.Sprintf("variance %s needs column %q", token, r.column)
	}
	msg := fmt.Sprintf("%s, which %s does not have", what, bindingNames(r.bindings))
	if hint := caseHint(r.column, sets); hint != "" {
		msg += "; " + hint
	}
	c.findings = append(c.findings, c.src.finding(doc, rule, "error", path, msg))
}

func bindingNames(bindings []string) string {
	names := make([]string, len(bindings))
	for i, b := range bindings {
		if name, ok := strings.CutPrefix(b, "$"); ok {
			names[i] = fmt.Sprintf("DataSource %q", name)
		} else {
			names[i] = fmt.Sprintf("DataSet %q", b)
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	return "none of " + strings.Join(names, ", ")
}

// caseHint names a column that differs only in case: the engine reads the
// row keys as written.
func caseHint(column string, sets []columnSet) string {
	for _, set := range sets {
		for _, have := range sortedKeys(set) {
			if strings.EqualFold(have, column) {
				return fmt.Sprintf("it has %q (column names are case sensitive)", have)
			}
		}
	}
	return ""
}

// place is a spec mapping in a document, where a component's fields can be.
type place struct {
	doc  *config.Document
	path string // dotted path of the spec mapping
	spec map[string]any
}

// locate finds the YAML path of the field a read comes from. It walks the
// layout the way the graph builder does: node names are "<root>#<i>.<j>"
// for layout children, and a ref child's fields are either its own spec
// overrides or the referenced document's spec. token is the scenario or
// variance token that names the column, when one does.
func (c *checker) locate(node *graph.Node, r read) (doc *config.Document, path, token string) {
	kind := node.Attributes["componentKind"]
	var places []place
	var fallback place

	if parent := node.Attributes["parent"]; parent == "" {
		d := c.src.doc(kind, node.Name)
		if d == nil {
			return nil, "", ""
		}
		fallback = place{doc: d, path: "spec"}
		if sp, ok := c.src.json(d)["spec"].(map[string]any); ok {
			places = append(places, place{d, "spec", sp})
		}
	} else {
		_, key, _ := strings.Cut(node.Name, "#")
		var ok bool
		for _, rootKind := range []string{"LayoutPage", "LayoutCard"} {
			for _, d := range c.src.byKey[rootKind+":"+parent] {
				if places, fallback, ok = c.walkLayout(d, key); ok {
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			return nil, "", ""
		}
	}

	field := r.field
	for _, p := range places {
		sp, base := p.spec, p.path
		if id, rest, ok := treeField(field); ok {
			sp, base = treeNodeSpec(sp, base, id)
			field = rest
		}
		if sp == nil {
			continue
		}
		if v, has := sp[field]; has {
			sub, tok := matchValue(v, r)
			path = base + "." + field
			if sub != "" {
				path += "." + sub
			}
			return p.doc, path, tok
		}
	}
	return fallback.doc, fallback.path, ""
}

// walkLayout follows an index path ("0.1") from a layout document to a
// child and returns the places its fields can be in, and the child itself.
func (c *checker) walkLayout(root *config.Document, key string) (places []place, at place, ok bool) {
	sp, ok := c.src.json(root)["spec"].(map[string]any)
	if !ok {
		return nil, place{}, false
	}
	cur := place{root, "spec", sp}
	curKind := root.Kind
	parts := strings.Split(key, ".")
	for i, part := range parts {
		var child map[string]any
		var childPath string
		if curKind == "SmallMultiple" {
			child, _ = cur.spec["base"].(map[string]any)
			childPath = cur.path + ".base"
		} else {
			idx, err := strconv.Atoi(part)
			list, _ := cur.spec["children"].([]any)
			if err != nil || idx < 0 || idx >= len(list) {
				return nil, place{}, false
			}
			child, _ = list[idx].(map[string]any)
			childPath = cur.path + ".children." + part
		}
		if child == nil {
			return nil, place{}, false
		}
		childKind, _ := child["kind"].(string)
		own, _ := child["spec"].(map[string]any)
		var refPlace *place
		if ref, _ := child["ref"].(string); ref != "" {
			if d := c.src.doc(childKind, ref); d != nil {
				if rsp, ok := c.src.json(d)["spec"].(map[string]any); ok {
					refPlace = &place{d, "spec", rsp}
				}
			}
		}

		if i == len(parts)-1 {
			if own != nil {
				places = append(places, place{cur.doc, childPath + ".spec", own})
			}
			if refPlace != nil {
				places = append(places, *refPlace)
			}
			return places, place{doc: cur.doc, path: childPath}, true
		}

		nextKey := "children"
		if childKind == "SmallMultiple" {
			nextKey = "base"
		}
		switch {
		case own != nil && own[nextKey] != nil:
			cur = place{cur.doc, childPath + ".spec", own}
		case refPlace != nil:
			cur = *refPlace
		default:
			return nil, place{}, false
		}
		curKind = childKind
	}
	return nil, place{}, false
}

// treeField splits a Tree node field "nodes[<id>].<field>".
func treeField(field string) (id, rest string, ok bool) {
	inner, ok := strings.CutPrefix(field, "nodes[")
	if !ok {
		return "", "", false
	}
	id, rest, ok = strings.Cut(inner, "].")
	return id, rest, ok
}

func treeNodeSpec(sp map[string]any, base, id string) (nodeSpec map[string]any, path string) {
	nodes, _ := sp["nodes"].([]any)
	for i, n := range nodes {
		m, _ := n.(map[string]any)
		if m == nil || m["id"] != id {
			continue
		}
		if s, ok := m["spec"].(map[string]any); ok {
			return s, fmt.Sprintf("%s.nodes.%d.spec", base, i)
		}
	}
	return nil, ""
}

// matchValue finds the list item of a field value that names the read
// column, and the token that does so.
func matchValue(v any, r read) (sub, token string) {
	switch val := v.(type) {
	case string:
		return "", matchToken(val, r)
	case []any:
		for i, item := range val {
			var text string
			switch it := item.(type) {
			case string:
				text = it
			case map[string]any:
				for _, k := range sortedKeys(it) {
					if s, ok := it[k].(string); ok && matchToken(s, r) != "" {
						text = s
						break
					}
				}
			}
			if tok := matchToken(text, r); tok != "" {
				return strconv.Itoa(i), tok
			}
		}
	}
	return "", ""
}

// matchToken returns the comma-separated token of s that reads the column:
// the column itself or a variance whose slots include it. Fields that are
// not token lists (filters, templates) match when they mention the column.
func matchToken(s string, r read) string {
	if r.role != graph.RoleScenario && r.role != graph.RoleVariance {
		if strings.Contains(s, r.column) {
			return r.column
		}
		return ""
	}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if at := strings.IndexByte(tok, '@'); at >= 0 {
			tok = strings.TrimSpace(tok[:at])
		}
		if tok == r.column {
			return tok
		}
		if a, b, ok := graph.VarianceSlots(tok); ok && (a == r.column || b == r.column) {
			return tok
		}
	}
	return ""
}
