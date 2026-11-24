package spec

import (
	"reflect"
	"testing"
)

func TestFindNameUses(t *testing.T) {
	for _, tt := range []struct {
		text string
		prql bool
		want []int
	}{
		{"SELECT * FROM sales", false, []int{14}},
		{`FROM "sales" JOIN sales_2024`, false, []int{6}},
		{"FROM presales, t.sales, my-sales, @acme/sales", false, nil},
		{"WHERE x = 'sales' -- sales\nFROM sales", false, []int{32}},
		{"from sales # sales", true, []int{5}},
		{"from `sales`", true, []int{6}},
	} {
		if got := findNameUses(tt.text, "sales", tt.prql); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("findNameUses(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}
