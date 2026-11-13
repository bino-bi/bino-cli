package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// checkGridCellLayoutEngine fails a render that uses grid cell size, alignment
// or captions on an engine that would ignore them.
func checkGridCellLayoutEngine(docs []config.Document, engineVersion string) error {
	if !config.UsesGridCellLayout(docs) {
		return nil
	}
	if err := engine.GridCellLayoutCheck(config.UsesSmallMultipleCard(docs))(engineVersion); err != nil {
		return fmt.Errorf("render: grid-cell-layout-engine-version: %w", err)
	}
	return nil
}

// checkSmallMultipleSpanEngine fails a render that uses cell spans, cell
// backgrounds or the other fields of that engine release on an engine that
// would ignore them. The dataset results show the chart fields that a dataset
// default sets.
func checkSmallMultipleSpanEngine(docs []config.Document, results []dataset.Result, engineVersion string) error {
	features := dataset.SmallMultipleSpanFeatures(docs, results)
	if len(features) == 0 {
		return nil
	}
	if err := engine.SmallMultipleSpanCheck(features)(engineVersion); err != nil {
		return fmt.Errorf("render: small-multiple-span-engine-version: %w", err)
	}
	return nil
}
