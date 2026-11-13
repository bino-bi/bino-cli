package schema

import "testing"

func TestValidate_ChartFields(t *testing.T) {
	chart := func(kind, fields string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: " + kind + "\nmetadata:\n  name: c\nspec:\n  dataset: sales\n" + fields
	}
	runValidateCases(t, []validateCase{
		{"time chart, all new fields", chart("ChartTime", "  scenarios: []\n  variances: [dac1_pp1_pos]\n  dataFormatDigitsDecimal: 0\n  dataFormatDigitsPercent: 1\n  axisLabelsStep: 2\n  scenarioSwitch: true\n  axisTitlePosition: none\n"), false},
		{"structure chart, digits and no scenarios", chart("ChartStructure", "  scenarios: []\n  dataFormatDigitsDecimal: 2\n  dataFormatDigitsPercent: 0\n"), false},
		{"table keeps its empty scenario list", chart("Table", "  scenarios: []\n"), false},
		{"axisTitlePosition right", chart("ChartTime", "  axisTitlePosition: right\n"), false},
		{"unknown axisTitlePosition", chart("ChartTime", "  axisTitlePosition: top\n"), true},
		{"axisLabelsStep zero", chart("ChartTime", "  axisLabelsStep: 0\n"), true},
		{"axisLabelsStep not a whole number", chart("ChartTime", "  axisLabelsStep: 1.5\n"), true},
		{"scenarioSwitch must be a boolean", chart("ChartTime", "  scenarioSwitch: \"yes\"\n"), true},
		{"negative digits", chart("ChartTime", "  dataFormatDigitsDecimal: -1\n"), true},
		{"digits as text", chart("ChartStructure", "  dataFormatDigitsPercent: two\n"), true},
		{"structure chart has no scenarioSwitch", chart("ChartStructure", "  scenarioSwitch: true\n"), true},
		{"structure chart has no axisLabelsStep", chart("ChartStructure", "  axisLabelsStep: 2\n"), true},
		{"structure chart has no axisTitlePosition", chart("ChartStructure", "  axisTitlePosition: right\n"), true},
	})
}
