package schema

import "testing"

type validateCase struct {
	name    string
	yaml    string
	wantErr bool
}

func runValidateCases(t *testing.T, tests []validateCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidate_StyleKeysOfCellSpanEngine(t *testing.T) {
	style := func(content string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: ComponentStyle\nmetadata:\n  name: s\nspec:\n  content:\n" + content
	}
	runValidateCases(t, []validateCase{
		{"cellBackground and titleFontSize", style("    bn-grid: { cellBackground: \"#f5f5f5\" }\n    bn-layout-card: { titleFontSize: 16 }\n"), false},
		{"cellBackground must be a string", style("    bn-grid: { cellBackground: 5 }\n"), true},
		{"titleFontSize must be a number", style("    bn-layout-card: { titleFontSize: large }\n"), true},
	})
}

func TestValidate_ShortYearFormatKey(t *testing.T) {
	doc := "apiVersion: bino.bi/v1alpha1\nkind: Internationalization\nmetadata:\n  name: i\nspec:\n  code: en\n  content:\n    bn-chart-time.date-format-short-year: "
	runValidateCases(t, []validateCase{
		{"pattern", doc + "\"’yy\"\n", false},
		{"must be a string", doc + "[yy]\n", true},
	})
}
