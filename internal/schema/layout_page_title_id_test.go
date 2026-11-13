package schema

import "testing"

func TestValidate_LayoutPageTitleID(t *testing.T) {
	page := func(fields string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: p\nspec:\n" + fields + "  children: []\n"
	}
	runValidateCases(t, []validateCase{
		{"titleId", page("  titleId: 13A\n"), false},
		{"titleId must be a string", page("  titleId: [13]\n"), true},
		{"a card has no titleId", "apiVersion: bino.bi/v1alpha1\nkind: LayoutCard\nmetadata:\n  name: c\nspec:\n  titleId: 13A\n  children:\n    - kind: Text\n      spec: { value: a }\n", true},
	})
}
