package spec

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TableIntervalSetname is the Table interval that groups by the setname column.
const TableIntervalSetname = "setname"

// TableIntervalActive reports whether a Table repeats its columns once per
// interval group. columnthereof wins over interval, as in the engine.
func TableIntervalActive(interval string, hasColumnthereof bool) bool {
	if hasColumnthereof {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "", "none":
		return false
	}
	return true
}

// SetnameGroupCount counts the distinct setname values in the rows of the
// given datasets: the interval groups of a Table with interval setname before
// its filter runs. Rows without a setname form one group. ok is false when a
// dataset does not decode.
func SetnameGroupCount(datasets ...json.RawMessage) (count int, ok bool) {
	groups := map[string]bool{}
	for _, data := range datasets {
		var rows []struct {
			Setname any `json:"setname"`
		}
		if err := json.Unmarshal(data, &rows); err != nil {
			return 0, false
		}
		for _, row := range rows {
			// The NUL prefix keeps the blank group apart from every real name.
			key := "\x00"
			if s, isString := row.Setname.(string); isString && s != "" && s != "null" {
				key = s
			} else if row.Setname != nil && !isString {
				key = fmt.Sprint(row.Setname)
			}
			groups[key] = true
		}
	}
	return len(groups), true
}

// CategoryIntervalPositionTooLarge is the message for a Table whose
// categoryIntervalPosition is larger than the number of its interval groups.
func CategoryIntervalPositionTooLarge(position, groups int, datasets []string) string {
	return fmt.Sprintf("categoryIntervalPosition is %d but there are only %d interval group(s): "+
		"dataset %s has %d distinct setname value(s), counted before the Table filter. "+
		"The category column is put last; use a value from 0 to %d",
		position, groups, strings.Join(datasets, ", "), groups, groups)
}
