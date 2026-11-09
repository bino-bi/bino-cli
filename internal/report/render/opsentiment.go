package render

import (
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/config"
)

// checkOperationSentimentEngine fails a render that uses the variance
// sentiments _op or _opinv on an engine that would draw them wrong.
func checkOperationSentimentEngine(docs []config.Document, engineVersion string) error {
	if !config.UsesOperationSentiment(docs) {
		return nil
	}
	if err := engine.CheckOperationSentimentSupport(engineVersion); err != nil {
		return fmt.Errorf("render: operation-sentiment-engine-version: %w", err)
	}
	return nil
}
