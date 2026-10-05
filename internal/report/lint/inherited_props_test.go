package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inheritDoc builds a document named <name>.yaml from a JSON spec.
func inheritDoc(kind, name, spec string) Document {
	raw := fmt.Sprintf(`{"apiVersion":"bino.bi/v1","kind":%q,"metadata":{"name":%q},"spec":%s}`, kind, name, spec)
	return Document{File: "/p/" + name + ".yaml", Position: 1, Kind: kind, Name: name, Raw: json.RawMessage(raw)}
}

// inheritSpec joins spec fields (a JSON fragment, may be empty) with a child list.
func inheritSpec(fields, listKey string, children []string) string {
	parts := make([]string, 0, 2)
	if fields != "" {
		parts = append(parts, fields)
	}
	if children != nil {
		parts = append(parts, fmt.Sprintf(`%q:[%s]`, listKey, strings.Join(children, ",")))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func inheritPage(name, fields string, children ...string) Document {
	return inheritDoc("LayoutPage", name, inheritSpec(fields, "children", children))
}

// inheritChild is an inline child of the given kind.
func inheritChild(kind, fields string) string {
	return fmt.Sprintf(`{"kind":%q,"spec":{%s}}`, kind, fields)
}

func inheritCard(fields string, children ...string) string {
	return fmt.Sprintf(`{"kind":"LayoutCard","spec":%s}`, inheritSpec(fields, "children", children))
}

func inheritGrid(children ...string) string {
	return fmt.Sprintf(`{"kind":"Grid","spec":%s}`, inheritSpec("", "children", children))
}

func inheritTree(nodes ...string) string {
	return fmt.Sprintf(`{"kind":"Tree","spec":%s}`, inheritSpec("", "nodes", nodes))
}

func inheritRef(kind, name string) string {
	return fmt.Sprintf(`{"kind":%q,"ref":%q}`, kind, name)
}

// runInheritedRule runs the rule and returns each finding as "<file> <path>".
func runInheritedRule(t *testing.T, docs []Document) []string {
	t.Helper()
	findings := inheritedPropUnresolved.Check(context.Background(), docs)
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.RuleID != "inherited-prop-unresolved" {
			t.Errorf("RuleID = %q, want inherited-prop-unresolved", f.RuleID)
		}
		if f.Severity != "" {
			t.Errorf("Severity = %q, want the default (warning)", f.Severity)
		}
		got = append(got, filepath.Base(f.File)+" "+f.Path)
	}
	return got
}

func TestInheritedPropUnresolved(t *testing.T) {
	const (
		scenariosSet = `"titleScenarios":["ac1","pp1"]`
		pageKeyword  = `"scenarios":"inherited-page"`
		closest      = `"scenarios":"inherited-closest"`
	)
	tablePage := inheritChild("Table", pageKeyword)
	tableClosest := inheritChild("Table", closest)

	tests := []struct {
		name string
		docs []Document
		want []string
	}{
		{
			name: "page sets the field, inherited-page",
			docs: []Document{inheritPage("p", scenariosSet, tablePage)},
		},
		{
			name: "page sets nothing, inherited-page",
			docs: []Document{inheritPage("p", "", tablePage)},
			want: []string{"p.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "page sets nothing, inherited-closest",
			docs: []Document{inheritPage("p", "", tableClosest)},
			want: []string{"p.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "card lacks the field, page has it, inherited-closest",
			docs: []Document{inheritPage("p", scenariosSet, inheritCard("", tableClosest))},
		},
		{
			name: "card lacks the field, page has it, inherited-page",
			docs: []Document{inheritPage("p", scenariosSet, inheritCard("", tablePage))},
		},
		{
			name: "card sets the field, page lacks it, inherited-closest",
			docs: []Document{inheritPage("p", "", inheritCard(scenariosSet, tableClosest))},
		},
		{
			name: "card sets the field, page lacks it, inherited-page",
			docs: []Document{inheritPage("p", "", inheritCard(scenariosSet, tablePage))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.scenarios"},
		},
		{
			name: "nested cards, only the outer one sets the field",
			docs: []Document{inheritPage("p", "", inheritCard(scenariosSet, inheritCard("", tableClosest)))},
		},
		{
			name: "no card and not the page set it",
			docs: []Document{inheritPage("p", "", inheritCard("", inheritCard("", tableClosest)))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.children.0.spec.scenarios"},
		},
		{
			name: "a blank value does not set the field",
			docs: []Document{inheritPage("p", `"titleScenarios":"  "`, inheritCard(`"titleScenarios":[]`, tableClosest))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.scenarios"},
		},
		{
			name: "card inherits from a page that sets the field",
			docs: []Document{inheritPage("p", scenariosSet, inheritCard(`"titleScenarios":"inherited-page"`, tableClosest))},
		},
		{
			name: "card inherits from a page that lacks the field",
			docs: []Document{inheritPage("p", "", inheritCard(`"titleScenarios":"inherited-page"`, tableClosest))},
			want: []string{
				"p.yaml spec.children.0.spec.titleScenarios",
				"p.yaml spec.children.0.spec.children.0.spec.scenarios",
			},
		},
		{
			name: "unresolved inner card is skipped, the outer card sets the field",
			docs: []Document{inheritPage("p", "",
				inheritCard(scenariosSet, inheritCard(`"titleScenarios":"inherited-page"`, tableClosest)))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.titleScenarios"},
		},
		{
			name: "standalone Table used on a page that sets the field and on one that does not",
			docs: []Document{
				inheritPage("with", scenariosSet, inheritRef("Table", "t")),
				inheritPage("without", "", inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
			},
			want: []string{"t.yaml spec.scenarios"},
		},
		{
			name: "standalone Table used on two pages that lack the field",
			docs: []Document{
				inheritPage("a", "", inheritRef("Table", "t")),
				inheritPage("b", "", inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
			},
			want: []string{"t.yaml spec.scenarios", "t.yaml spec.scenarios"},
		},
		{
			name: "standalone Table used twice on one page is reported once",
			docs: []Document{
				inheritPage("p", "", inheritRef("Table", "t"), inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
			},
			want: []string{"t.yaml spec.scenarios"},
		},
		{
			name: "standalone Table used on one page inside a card that sets the field and outside it",
			docs: []Document{
				inheritPage("p", "", inheritCard(scenariosSet, inheritRef("Table", "t")), inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+closest+`}`),
			},
			want: []string{"t.yaml spec.scenarios"},
		},
		{
			name: "standalone Table used on one page inside and outside a card, nobody sets the field",
			docs: []Document{
				inheritPage("p", "", inheritCard("", inheritRef("Table", "t")), inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+closest+`}`),
			},
			want: []string{"t.yaml spec.scenarios"},
		},
		{
			name: "inline override carries the keyword",
			docs: []Document{
				inheritPage("p", "", `{"kind":"Table","ref":"t","spec":{`+pageKeyword+`}}`),
				inheritDoc("Table", "t", `{"scenarios":["ac1"]}`),
			},
			want: []string{"p.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "inline override replaces the keyword",
			docs: []Document{
				inheritPage("p", "", `{"kind":"Table","ref":"t","spec":{"scenarios":["ac1"]}}`),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
			},
		},
		{
			name: "standalone documents that nothing references",
			docs: []Document{
				inheritPage("p", ""),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
				inheritDoc("LayoutCard", "c", inheritSpec(`"ruleset":"inherited-page"`, "children", []string{tablePage})),
			},
		},
		{
			name: "ref to a name that two documents share is not followed",
			docs: []Document{
				inheritPage("p", "", inheritRef("Table", "t")),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
				inheritDoc("Table", "t", `{"scenarios":["ac1"]}`),
				inheritDoc("Table", "t", `{`+pageKeyword+`}`),
			},
		},
		{
			name: "missing ref is left to missing-required-reference",
			docs: []Document{inheritPage("p", "", inheritRef("Table", "nowhere"))},
		},
		{
			name: "standalone card, Table inside it lacks a page value",
			docs: []Document{
				inheritPage("p", "", inheritRef("LayoutCard", "c")),
				inheritDoc("LayoutCard", "c", inheritSpec("", "children", []string{tablePage})),
			},
			want: []string{"c.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "standalone card gets the field from an inline override",
			docs: []Document{
				inheritPage("p", "", `{"kind":"LayoutCard","ref":"c","spec":{`+scenariosSet+`}}`),
				inheritDoc("LayoutCard", "c", inheritSpec("", "children", []string{tableClosest})),
			},
		},
		{
			name: "card that references itself ends",
			docs: []Document{
				inheritPage("p", "", inheritRef("LayoutCard", "c")),
				inheritDoc("LayoutCard", "c", inheritSpec("", "children", []string{inheritRef("LayoutCard", "c"), tablePage})),
			},
			want: []string{"c.yaml spec.children.1.spec.scenarios"},
		},
		{
			name: "standalone card nested in itself through an inline child list is no cycle",
			docs: []Document{
				inheritPage("p", "", `{"kind":"LayoutCard","ref":"c","spec":{"children":[`+inheritRef("LayoutCard", "c")+`]}}`),
				inheritDoc("LayoutCard", "c", inheritSpec("", "children", []string{tablePage})),
			},
			want: []string{"c.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "two cards that reference each other end",
			docs: []Document{
				inheritPage("p", "", inheritRef("LayoutCard", "a")),
				inheritDoc("LayoutCard", "a", inheritSpec("", "children", []string{inheritRef("LayoutCard", "b")})),
				inheritDoc("LayoutCard", "b", inheritSpec("", "children", []string{inheritRef("LayoutCard", "a"), tablePage})),
			},
			want: []string{"b.yaml spec.children.1.spec.scenarios"},
		},
		{
			name: "Grid and Tree are walked through to the card",
			docs: []Document{inheritPage("p", "",
				inheritCard(scenariosSet, inheritGrid(tableClosest), inheritTree(tableClosest)))},
		},
		{
			name: "Grid and Tree are no inheritance source",
			docs: []Document{inheritPage("p", "", inheritGrid(tableClosest), inheritTree(tablePage))},
			want: []string{
				"p.yaml spec.children.0.spec.children.0.spec.scenarios",
				"p.yaml spec.children.1.spec.nodes.0.spec.scenarios",
			},
		},
		{
			name: "ChartStructure resolved",
			docs: []Document{inheritPage("p", scenariosSet, inheritChild("ChartStructure", pageKeyword))},
		},
		{
			name: "ChartStructure unresolved",
			docs: []Document{inheritPage("p", "", inheritChild("ChartStructure", pageKeyword))},
			want: []string{"p.yaml spec.children.0.spec.scenarios"},
		},
		{
			name: "ChartTime resolved",
			docs: []Document{inheritPage("p", scenariosSet, inheritCard("", inheritChild("ChartTime", closest)))},
		},
		{
			name: "ChartTime unresolved",
			docs: []Document{inheritPage("p", "", inheritCard("", inheritChild("ChartTime", closest)))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.scenarios"},
		},
		{
			name: "one finding per unresolved prop",
			docs: []Document{inheritPage("p", "",
				inheritChild("Table", pageKeyword+`,"variances":"inherited-page","order":"inherited-page","orderDirection":"inherited-page"`))},
			want: []string{
				"p.yaml spec.children.0.spec.scenarios",
				"p.yaml spec.children.0.spec.variances",
				"p.yaml spec.children.0.spec.order",
				"p.yaml spec.children.0.spec.orderDirection",
			},
		},
		{
			name: "the keyword inside a list is an ordinary entry",
			docs: []Document{inheritPage("p", "", inheritChild("Table", `"scenarios":["inherited-page"]`))},
		},
		{
			name: "ruleset named by the page, Table in a plain card",
			docs: []Document{inheritPage("p", `"ruleset":"corporate"`,
				inheritCard("", inheritChild("Table", `"ruleset":"inherited-closest"`)))},
		},
		{
			name: "ruleset named by nobody",
			docs: []Document{inheritPage("p", "", inheritCard("", inheritChild("Table", `"ruleset":"inherited-closest"`)))},
			want: []string{"p.yaml spec.children.0.spec.children.0.spec.ruleset"},
		},
		{
			name: "ChartScatter ruleset resolved",
			docs: []Document{inheritPage("p", `"ruleset":"corporate"`, inheritChild("ChartScatter", `"ruleset":"inherited-page"`))},
		},
		{
			name: "ChartScatter ruleset unresolved",
			docs: []Document{inheritPage("p", "", inheritChild("ChartScatter", `"ruleset":"inherited-page"`))},
			want: []string{"p.yaml spec.children.0.spec.ruleset"},
		},
		{
			name: "ChartBullet inherits ruleset but not order",
			docs: []Document{inheritPage("p", "", inheritChild("ChartBullet", `"order":"inherited-page","ruleset":"inherited-page"`))},
			want: []string{"p.yaml spec.children.0.spec.ruleset"},
		},
		{
			name: "ChartBubble ruleset unresolved",
			docs: []Document{inheritPage("p", "", inheritChild("ChartBubble", `"ruleset":"inherited-closest"`))},
			want: []string{"p.yaml spec.children.0.spec.ruleset"},
		},
		{
			name: "card ruleset inherits from a page without one",
			docs: []Document{inheritPage("p", "", inheritCard(`"ruleset":"inherited-page"`, inheritChild("Text", `"value":"x"`)))},
			want: []string{"p.yaml spec.children.0.spec.ruleset"},
		},
		{
			name: "a keyword on the page names no rule set",
			docs: []Document{inheritPage("p", `"ruleset":"inherited-page"`, inheritChild("Table", `"ruleset":"inherited-page"`))},
			want: []string{"p.yaml spec.children.0.spec.ruleset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runInheritedRule(t, tt.docs); !slices.Equal(got, tt.want) {
				t.Errorf("findings = %v, want %v", got, tt.want)
			}
		})
	}
}

// Each prop reads exactly one field: that field alone resolves it, and all the
// other fields together do not.
func TestInheritedPropUnresolved_FieldMapping(t *testing.T) {
	fields := map[string]string{
		"titleScenarios":      `"titleScenarios":["ac1","pp1"]`,
		"titleVariances":      `"titleVariances":"dac1_pp1_pos"`,
		"titleOrder":          `"titleOrder":"ac1"`,
		"titleOrderDirection": `"titleOrderDirection":"desc"`,
		"ruleset":             `"ruleset":"corporate"`,
	}
	mapping := map[string]string{
		"scenarios":      "titleScenarios",
		"variances":      "titleVariances",
		"order":          "titleOrder",
		"orderDirection": "titleOrderDirection",
		"ruleset":        "ruleset",
	}

	for prop, field := range mapping {
		for _, keyword := range []string{"inherited-page", "inherited-closest"} {
			t.Run(prop+" "+keyword, func(t *testing.T) {
				table := inheritChild("Table", fmt.Sprintf("%q:%q", prop, keyword))

				if got := runInheritedRule(t, []Document{inheritPage("p", fields[field], table)}); len(got) != 0 {
					t.Errorf("page sets only %s: findings = %v, want none", field, got)
				}

				var others []string
				for name, value := range fields {
					if name != field {
						others = append(others, value)
					}
				}
				allButField := strings.Join(others, ",")
				want := []string{"p.yaml spec.children.0.spec." + prop}
				got := runInheritedRule(t, []Document{inheritPage("p", allButField, table)})
				if !slices.Equal(got, want) {
					t.Errorf("page sets all but %s: findings = %v, want %v", field, got, want)
				}
			})
		}

		// The same field names on a LayoutCard, as a source and as a keyword.
		t.Run(field+" on a card", func(t *testing.T) {
			table := inheritChild("Table", fmt.Sprintf("%q:%q", prop, "inherited-closest"))

			if got := runInheritedRule(t, []Document{inheritPage("p", "", inheritCard(fields[field], table))}); len(got) != 0 {
				t.Errorf("card sets only %s: findings = %v, want none", field, got)
			}

			keyword := fmt.Sprintf("%q:%q", field, "inherited-page")
			text := inheritChild("Text", `"value":"x"`)
			if got := runInheritedRule(t, []Document{inheritPage("p", fields[field], inheritCard(keyword, text))}); len(got) != 0 {
				t.Errorf("card inherits %s from a page that sets it: findings = %v, want none", field, got)
			}
			want := []string{"p.yaml spec.children.0.spec." + field}
			if got := runInheritedRule(t, []Document{inheritPage("p", "", inheritCard(keyword, text))}); !slices.Equal(got, want) {
				t.Errorf("card inherits %s from a page without it: findings = %v, want %v", field, got, want)
			}
		})
	}
}

func TestInheritedPropUnresolved_Message(t *testing.T) {
	tests := []struct {
		name string
		page Document
		want string
	}{
		{
			name: "inherited-page",
			page: inheritPage("sales", "", inheritCard("", inheritChild("Table", `"scenarios":"inherited-page"`))),
			want: `'scenarios: inherited-page' has nothing to inherit: LayoutPage "sales" does not set 'titleScenarios'; ` +
				`set it there or give 'scenarios' an explicit value`,
		},
		{
			name: "inherited-closest without a card",
			page: inheritPage("sales", "", inheritChild("Table", `"order":"inherited-closest"`)),
			want: `'order: inherited-closest' has nothing to inherit: neither an enclosing LayoutCard nor ` +
				`LayoutPage "sales" sets 'titleOrder'; set it on one of them or give 'order' an explicit value`,
		},
		{
			name: "inherited-closest inside a card",
			page: inheritPage("sales", "", inheritCard("", inheritChild("Table", `"ruleset":"inherited-closest"`))),
			want: `'ruleset: inherited-closest' has nothing to inherit: neither an enclosing LayoutCard nor ` +
				`LayoutPage "sales" sets 'ruleset'; set it on one of them or give 'ruleset' an explicit value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := inheritedPropUnresolved.Check(context.Background(), []Document{tt.page})
			if len(findings) != 1 {
				t.Fatalf("got %d findings, want 1", len(findings))
			}
			if findings[0].Message != tt.want {
				t.Errorf("Message = %q, want %q", findings[0].Message, tt.want)
			}
		})
	}
}

// The finding points at the document that holds the keyword, not at the page.
func TestInheritedPropUnresolved_Location(t *testing.T) {
	table := inheritDoc("Table", "t", `{"scenarios":"inherited-page"}`)
	table.File, table.Position = "/p/components.yaml", 3

	findings := inheritedPropUnresolved.Check(context.Background(), []Document{
		inheritPage("p", "", inheritRef("Table", "t")),
		table,
	})
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if f := findings[0]; f.File != table.File || f.DocIdx != 3 || f.Path != "spec.scenarios" {
		t.Errorf("finding at %s #%d %s, want %s #3 spec.scenarios", f.File, f.DocIdx, f.Path, table.File)
	}
}

func TestInheritedPropUnresolved_Disable(t *testing.T) {
	docs := []Document{inheritPage("p", "", inheritChild("Table", `"scenarios":"inherited-page"`))}
	hasFinding := func(findings []Finding) bool {
		return slices.ContainsFunc(findings, func(f Finding) bool { return f.RuleID == "inherited-prop-unresolved" })
	}

	runner := NewProjectRunner(lintProject(t, "[lint]\ndisable = [\"inherited-prop-unresolved\"]\n"))
	if warnings := runner.ConfigWarnings(); len(warnings) != 0 {
		t.Fatalf("unexpected config warnings: %v", warnings)
	}

	raw := runner.Run(context.Background(), docs)
	if !hasFinding(raw) {
		t.Fatalf("Run returned %v, want an inherited-prop-unresolved finding", findingIDs(raw))
	}
	if kept := runner.Apply(raw); hasFinding(kept) {
		t.Errorf("Apply kept %v, want inherited-prop-unresolved dropped", findingIDs(kept))
	}
}
