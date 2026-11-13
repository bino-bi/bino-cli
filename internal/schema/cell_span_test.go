package schema

import "testing"

func TestValidate_GridCellSpan(t *testing.T) {
	grid := func(child string) string {
		return `
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata:
  name: g
spec:
  rowHeaders: ["A", "B"]
  columnHeaders: ["X", "Y"]
  children:
    - row: 0
      column: 0
      kind: Text
      spec: { value: a }
` + child
	}
	runValidateCases(t, []validateCase{
		{"span and background", grid("      rowSpan: 2\n      columnSpan: 2\n      background: \"#eeeeee\"\n"), false},
		{"span of one", grid("      rowSpan: 1\n      columnSpan: 1\n"), false},
		{"rowSpan zero", grid("      rowSpan: 0\n"), true},
		{"columnSpan negative", grid("      columnSpan: -1\n"), true},
		{"rowSpan not a whole number", grid("      rowSpan: 1.5\n"), true},
		{"columnSpan as text", grid("      columnSpan: two\n"), true},
		{"background must be a string", grid("      background: 5\n"), true},
	})
}

func TestValidate_SmallMultipleSpan(t *testing.T) {
	doc := func(rest string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: SmallMultiple\nmetadata:\n  name: sm\nspec:\n  dataset: profit\n  unitScaling: 10\n  scale: \"0.5\"\n" + rest
	}
	base := "  base: { kind: ChartTime, ref: t }\n"
	runValidateCases(t, []validateCase{
		{"overrides and item span", doc(base + "  axisTitles: rowEnd\n  panels:\n    by: rowGroup\n    overrides:\n      - { value: Berlin, rowSpan: 2, columnSpan: 2, background: \"#fff\" }\n      - { value: 2024, background: red }\n      - { value: true, rowSpan: 1 }\n    items:\n      - { label: avg, rowSpan: 2, columnSpan: 1, background: \"#eee\" }\n"), false},
		{"axisTitles each", doc(base + "  axisTitles: each\n  panels: { by: rowGroup }\n"), false},
		{"unknown axisTitles", doc(base + "  axisTitles: rowStart\n  panels: { by: rowGroup }\n"), true},
		{"override needs a value", doc(base + "  panels: { by: rowGroup, overrides: [{ rowSpan: 2 }] }\n"), true},
		{"override with an unknown field", doc(base + "  panels: { by: rowGroup, overrides: [{ value: a, card: {} }] }\n"), true},
		{"override rowSpan zero", doc(base + "  panels: { by: rowGroup, overrides: [{ value: a, rowSpan: 0 }] }\n"), true},
		{"override value must be a scalar", doc(base + "  panels: { by: rowGroup, overrides: [{ value: [a] }] }\n"), true},
		{"overrides need by", doc(base + "  panels: { items: [{ label: a }], overrides: [{ value: a, rowSpan: 2 }] }\n"), true},
		{"empty overrides", doc(base + "  panels: { by: rowGroup, overrides: [] }\n"), true},
		{"item columnSpan zero", doc(base + "  panels: { items: [{ label: a, columnSpan: 0 }] }\n"), true},
		{"item background must be a string", doc(base + "  panels: { items: [{ label: a, background: [1] }] }\n"), true},
		{"ref'd child overrides only the overrides", "apiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: p\nspec:\n  children:\n    - kind: SmallMultiple\n      ref: sm\n      spec:\n        axisTitles: rowEnd\n        panels: { overrides: [{ value: Berlin, rowSpan: 2 }] }\n", false},
	})
}
