package render

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// GridSpanProblem is a child of a Grid whose position or span cannot be drawn
// as written.
type GridSpanProblem struct {
	// Child is the index in spec.children.
	Child int
	// Field is rowSpan or columnSpan, or empty for the position of the child.
	Field   string
	Message string
}

// GridSpanProblems checks the rowSpan and columnSpan of the children of a Grid
// spec: a span must end inside the grid, and no child may sit in a position
// that the span of another child covers. A covering child that is optional or
// has constraints is no problem: it may not render, and then it covers
// nothing. Lint and render share it.
func GridSpanProblems(gridSpecRaw json.RawMessage) []GridSpanProblem {
	var s gridSpec
	if json.Unmarshal(gridSpecRaw, &s) != nil {
		return nil
	}
	_, problems := gridSpanLayout(s)
	return problems
}

// gridHeaderIDs are the slot ids of row or column headers: the id of an
// object header, the index of a plain label.
func gridHeaderIDs(raw json.RawMessage) []string {
	var headers []json.RawMessage
	if json.Unmarshal(raw, &headers) != nil {
		return nil
	}
	ids := make([]string, len(headers))
	for i, header := range headers {
		var withID struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(header, &withID) == nil && withID.ID != "" {
			ids[i] = withID.ID
			continue
		}
		ids[i] = strconv.Itoa(i)
	}
	return ids
}

// gridSpanLayout places the children as bn-grid does, row by row: a cell with
// a span also takes the positions right of and below it. It returns the
// children that sit in a taken position, by index, with the index of the child
// that covers them.
func gridSpanLayout(s gridSpec) (covered map[int]int, problems []GridSpanProblem) {
	spans := false
	for _, child := range s.Children {
		spans = spans || child.RowSpan > 1 || child.ColumnSpan > 1
	}
	if !spans {
		return nil, nil
	}
	rows, columns := gridHeaderIDs(s.RowHeaders), gridHeaderIDs(s.ColumnHeaders)
	index := func(ids []string, id string) int {
		for i, v := range ids {
			if v == id {
				return i
			}
		}
		return -1
	}
	at := map[[2]int]int{}
	for i, child := range s.Children {
		pos := [2]int{index(rows, child.Row.String()), index(columns, child.Column.String())}
		if _, taken := at[pos]; !taken && pos[0] >= 0 && pos[1] >= 0 {
			at[pos] = i
		}
	}
	covered = map[int]int{}
	owner := map[[2]int]int{}
	for r := range rows {
		for c := range columns {
			i, ok := at[[2]int{r, c}]
			if !ok {
				continue
			}
			child := s.Children[i]
			if by, taken := owner[[2]int{r, c}]; taken {
				covered[i] = by
				// Whether a child with a condition renders is only known at
				// render time, and when it does not, its span covers nothing.
				if !mayBeSkipped(s.Children[by]) {
					problems = append(problems, GridSpanProblem{Child: i, Message: fmt.Sprintf(
						"the span of the child in row %s, column %s covers this position: the child is not shown",
						s.Children[by].Row, s.Children[by].Column)})
				}
				continue
			}
			rowSpan, columnSpan := max(child.RowSpan, 1), max(child.ColumnSpan, 1)
			if rowSpan > len(rows)-r {
				problems = append(problems, GridSpanProblem{Child: i, Field: "rowSpan", Message: fmt.Sprintf(
					"rowSpan %d goes beyond the last row: only %d row(s) from row %s on", rowSpan, len(rows)-r, child.Row)})
				rowSpan = len(rows) - r
			}
			if columnSpan > len(columns)-c {
				problems = append(problems, GridSpanProblem{Child: i, Field: "columnSpan", Message: fmt.Sprintf(
					"columnSpan %d goes beyond the last column: only %d column(s) from column %s on", columnSpan, len(columns)-c, child.Column)})
				columnSpan = len(columns) - c
			}
			// as in bn-grid, a span ends before a position that is taken
			for width := 1; width < columnSpan; width++ {
				if _, taken := owner[[2]int{r, c + width}]; taken {
					columnSpan = width
					break
				}
			}
			for dr := range rowSpan {
				for dc := range columnSpan {
					owner[[2]int{r + dr, c + dc}] = i
				}
			}
		}
	}
	return covered, problems
}

// mayBeSkipped reports whether a child can render nothing: an optional ref,
// or constraints.
func mayBeSkipped(child gridChild) bool {
	return child.Optional || len(child.Metadata.Constraints) > 0
}
