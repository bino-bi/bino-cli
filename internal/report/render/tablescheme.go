package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	reportspec "bino.bi/bino/internal/report/spec"
)

// checkTableCalculationSchemeEngine fails a render whose Tables turn on
// runningSum, thereofPosition or varianceWaterfall, in a manifest or as a
// dataset default, on an engine that would ignore them.
func checkTableCalculationSchemeEngine(docs []config.Document, results []dataset.Result, engineVersion string) error {
	props := reportspec.MergeTableCalculationSchemeProps(
		config.TableCalculationSchemeProps(docs), dataset.DefaultsTableCalculationSchemeProps(results))
	if len(props) == 0 {
		return nil
	}
	if err := engine.TableCalculationSchemeCheck(props)(engineVersion); err != nil {
		return fmt.Errorf("render: table-calculation-scheme-engine-version: %w", err)
	}
	return nil
}
