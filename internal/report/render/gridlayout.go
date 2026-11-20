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
	if stack := dataset.StackSeriesFeatures(docs, results); len(stack) > 0 {
		if err := engine.StackSeriesCheck(stack)(engineVersion); err != nil {
			return fmt.Errorf("render: stack-series-engine-version: %w", err)
		}
	}
	if sums := dataset.StructureSumsFeatures(docs, results); len(sums) > 0 {
		if err := engine.StructureSumsCheck(sums)(engineVersion); err != nil {
			return fmt.Errorf("render: structure-sums-engine-version: %w", err)
		}
	}
	if totals := dataset.TimeTotalsFeatures(docs, results); len(totals) > 0 {
		if err := engine.TimeTotalsCheck(totals)(engineVersion); err != nil {
			return fmt.Errorf("render: time-totals-engine-version: %w", err)
		}
	}
	features := dataset.SmallMultipleSpanFeatures(docs, results)
	if len(features) == 0 {
		return nil
	}
	if err := engine.SmallMultipleSpanCheck(features)(engineVersion); err != nil {
		return fmt.Errorf("render: small-multiple-span-engine-version: %w", err)
	}
	return nil
}
