package spec

import (
	"slices"
	"strings"
)

// TableThereofPositionAbove is the Table thereofPosition that prints the
// thereof rows before their parent.
const TableThereofPositionAbove = "above"

// tableCalculationSchemeProps are the Table fields for calculation schemes.
var tableCalculationSchemeProps = []string{"runningSum", "thereofPosition", "varianceWaterfall"}

// TableCalculationSchemeProps returns the calculation scheme fields that a
// decoded Table spec turns on: runningSum, thereofPosition and
// varianceWaterfall, in this order. A field at its default is the output of
// every engine.
func TableCalculationSchemeProps(spec map[string]any) []string {
	var props []string
	for _, prop := range tableCalculationSchemeProps {
		on, _ := spec[prop].(bool)
		if prop == "thereofPosition" {
			pos, _ := spec[prop].(string)
			on = strings.EqualFold(strings.TrimSpace(pos), TableThereofPositionAbove)
		}
		if on {
			props = append(props, prop)
		}
	}
	return props
}

// MergeTableCalculationSchemeProps joins lists of calculation scheme fields
// into one list without repeats, in the order of TableCalculationSchemeProps.
func MergeTableCalculationSchemeProps(lists ...[]string) []string {
	var props []string
	for _, prop := range tableCalculationSchemeProps {
		if slices.ContainsFunc(lists, func(list []string) bool { return slices.Contains(list, prop) }) {
			props = append(props, prop)
		}
	}
	return props
}

// HasAbsoluteVarianceColumn reports whether a Table barColumns list has an
// absolute variance column, such as dac1_pp1_pos. Relative variances start
// with dr. A single string may hold a comma list or a JSON array.
func HasAbsoluteVarianceColumn(barColumns StringOrSlice) bool {
	for _, entry := range barColumns {
		cols := strings.FieldsFunc(strings.ToLower(entry), func(r rune) bool { return strings.ContainsRune(`,[]"' `, r) })
		for _, col := range cols {
			if strings.HasPrefix(col, "d") && !strings.HasPrefix(col, "dr") {
				return true
			}
		}
	}
	return false
}
