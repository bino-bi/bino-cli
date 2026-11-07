package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
)

// checkGridCellLayoutEngine fails a render that uses grid cell size, alignment
// or captions on an engine that would ignore them.
func checkGridCellLayoutEngine(docs []config.Document, engineVersion string) error {
	if !config.UsesGridCellLayout(docs) {
		return nil
	}
	if err := engine.CheckGridCellLayoutSupport(engineVersion); err != nil {
		return fmt.Errorf("render: grid-cell-layout-engine-version: %w", err)
	}
	return nil
}
