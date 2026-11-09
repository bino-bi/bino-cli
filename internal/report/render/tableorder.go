package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
)

// checkTableOrderMeasureEngine fails a render that sorts a Table by a variance
// or an interval block on an engine that would sort by category instead.
func checkTableOrderMeasureEngine(docs []config.Document, engineVersion string) error {
	if !config.UsesTableOrderMeasure(docs) {
		return nil
	}
	if err := engine.CheckTableOrderMeasureSupport(engineVersion); err != nil {
		return fmt.Errorf("render: table-order-engine-version: %w", err)
	}
	return nil
}
