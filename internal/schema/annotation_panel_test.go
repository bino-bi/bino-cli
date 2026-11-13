package schema

import "testing"

func TestValidate_AnnotationPanel(t *testing.T) {
	note := func(target string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: Annotation\nmetadata:\n  name: a\nspec:\n  target: " + target + "\n"
	}
	runValidateCases(t, []validateCase{
		{"panel of a SmallMultiple", note(`{ kind: SmallMultiple, name: sm, panel: Berlin, point: { date: "2024-01-01", scenario: ac1 } }`), false},
		{"SmallMultiple as a whole", note(`{ kind: SmallMultiple, name: sm }`), false},
		{"panel on another kind", note(`{ kind: ChartTime, name: trend, panel: Berlin }`), true},
		{"empty panel", note(`{ kind: SmallMultiple, name: sm, panel: "" }`), true},
		{"panel must be a string", note(`{ kind: SmallMultiple, name: sm, panel: [a] }`), true},
		{"arrow between two panels", "apiVersion: bino.bi/v1alpha1\nkind: Annotation\nmetadata:\n  name: a\nspec:\n  mark: arrow\n  target: { kind: SmallMultiple, name: sm, panel: Berlin }\n  to: { kind: SmallMultiple, name: sm, panel: Munich }\n", false},
	})
}
