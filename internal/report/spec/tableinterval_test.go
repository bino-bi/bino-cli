package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTableIntervalActive(t *testing.T) {
	cases := []struct {
		interval      string
		columnthereof bool
		want          bool
	}{
		{"", false, false},
		{"none", false, false},
		{" None ", false, false},
		{"setname", false, true},
		{"month", false, true},
		{"auto", false, true},
		{"setname", true, false},
	}
	for _, c := range cases {
		if got := TableIntervalActive(c.interval, c.columnthereof); got != c.want {
			t.Errorf("TableIntervalActive(%q, %v) = %v, want %v", c.interval, c.columnthereof, got, c.want)
		}
	}
}

func TestSetnameGroupCount(t *testing.T) {
	cases := []struct {
		name string
		data []string
		want int
		ok   bool
	}{
		{"three names", []string{`[{"setname":"Jan"},{"setname":"Feb"},{"setname":"Jan"},{"setname":"YTD"}]`}, 3, true},
		{"blank rows are one group", []string{`[{"setname":"Jan"},{"setname":""},{"setname":null},{"ac1":1},{"setname":"null"}]`}, 2, true},
		{"blank is not the name -", []string{`[{"setname":"-"},{"ac1":1}]`}, 2, true},
		{"two datasets", []string{`[{"setname":"Jan"}]`, `[{"setname":"Feb"},{"setname":"Jan"}]`}, 2, true},
		{"no rows", []string{`[]`}, 0, true},
		{"no array", []string{`{"setname":"Jan"}`}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := make([]json.RawMessage, len(c.data))
			for i, d := range c.data {
				data[i] = json.RawMessage(d)
			}
			got, ok := SetnameGroupCount(data...)
			if got != c.want || ok != c.ok {
				t.Fatalf("SetnameGroupCount = %d, %v, want %d, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestCategoryIntervalPositionTooLarge(t *testing.T) {
	msg := CategoryIntervalPositionTooLarge(8, 3, []string{"sales"})
	for _, want := range []string{"is 8", "only 3 interval group(s)", "dataset sales", "from 0 to 3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}
