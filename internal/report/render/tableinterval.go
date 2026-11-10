package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
)

// checkCategoryIntervalPositionEngine fails a render that uses
// categoryIntervalPosition on an engine that would ignore it.
func checkCategoryIntervalPositionEngine(docs []config.Document, engineVersion string) error {
	if !config.UsesCategoryIntervalPosition(docs) {
		return nil
	}
	if err := engine.CheckCategoryIntervalPositionSupport(engineVersion); err != nil {
		return fmt.Errorf("render: table-category-interval-position-engine-version: %w", err)
	}
	return nil
}

// checkCategoryIntervalPosition warns when a Table with interval setname asks
// for more interval groups left of the category column than its datasets
// have. Date intervals are left to the engine: only it knows their buckets.
func (rc *renderCtx) checkCategoryIntervalPosition(name string, s tableSpec) {
	if s.CategoryIntervalPosition == nil || *s.CategoryIntervalPosition <= 0 {
		return
	}
	if len(s.Columnthereof) > 0 || !strings.EqualFold(strings.TrimSpace(s.Interval), reportspec.TableIntervalSetname) {
		return
	}
	names := s.Dataset.Strings()
	if len(names) != len(s.Dataset.Entries()) {
		return
	}
	rows := make([]json.RawMessage, 0, len(names))
	for _, ds := range names {
		data, ok := rc.datasetRows[ds]
		if !ok {
			return // rows not known here, for example a DataSource binding
		}
		rows = append(rows, data)
	}
	groups, ok := reportspec.SetnameGroupCount(rows...)
	if !ok || groups == 0 || *s.CategoryIntervalPosition <= groups {
		return
	}
	label := "Table"
	if name != "" {
		label = fmt.Sprintf("Table %q", name)
	}
	rc.warn(fmt.Sprintf("table-category-interval-position-too-large: %s: %s", label,
		reportspec.CategoryIntervalPositionTooLarge(*s.CategoryIntervalPosition, groups, names)))
}
