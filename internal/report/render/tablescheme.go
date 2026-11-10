package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// checkTableCalculationSchemeEngine fails a render whose Tables turn on
// runningSum, thereofPosition or varianceWaterfall, in a manifest or as a
// default of their primary dataset, on an engine that would ignore them.
func checkTableCalculationSchemeEngine(docs []config.Document, results []dataset.Result, engineVersion string) error {
	props := dataset.TableCalculationSchemeProps(docs, results)
	if len(props) == 0 {
		return nil
	}
	if err := engine.TableCalculationSchemeCheck(props)(engineVersion); err != nil {
		return fmt.Errorf("render: table-calculation-scheme-engine-version: %w", err)
	}
	return nil
}
