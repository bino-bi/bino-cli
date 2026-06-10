// Package serve generates the HTML shell, navigation script, and query
// parameter handling for serving LiveReportArtefacts over HTTP. It contains
// the render-side logic of `bino serve`; the HTTP handlers and caching live
// in internal/cli.
package serve

import (
	"context"
	"encoding/json"
	"fmt"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/pkg/duckdb"
)

// QueryParamValidationResult holds the result of query parameter validation.
type QueryParamValidationResult struct {
	Params       map[string]string // Merged parameters (request values + defaults)
	MissingNames []string          // Names of missing required parameters
}

// IsValid returns true if there are no missing required parameters.
func (r QueryParamValidationResult) IsValid() bool {
	return len(r.MissingNames) == 0
}

// ValidateAndMergeQueryParams validates query parameters against route spec.
// Returns merged params (request values + defaults) and list of missing required params.
// Missing params are reported in the result, not as an error.
// For select type params with static items, also adds {name}_LABEL with the label from the option item.
func ValidateAndMergeQueryParams(routeSpec config.LiveRouteSpec, requestQuery map[string][]string) QueryParamValidationResult {
	result := QueryParamValidationResult{
		Params:       make(map[string]string),
		MissingNames: nil,
	}

	// Build param spec lookup for label resolution
	paramSpecs := make(map[string]config.LiveQueryParamSpec)
	for _, p := range routeSpec.QueryParams {
		paramSpecs[p.Name] = p
	}

	// Apply defaults first
	defaults := routeSpec.GetQueryParamDefaults()
	for name, defaultVal := range defaults {
		result.Params[name] = defaultVal
		// Add _LABEL for select params with static items
		if s, ok := paramSpecs[name]; ok && s.Type == "select" && s.Options != nil && len(s.Options.Items) > 0 {
			result.Params[name+"_LABEL"] = lookupLiveSelectLabel(s.Options.Items, defaultVal)
		}
	}

	// Override with request values (only for declared params)
	declaredParams := make(map[string]struct{})
	for _, p := range routeSpec.QueryParams {
		declaredParams[p.Name] = struct{}{}
	}

	for name := range declaredParams {
		if values, ok := requestQuery[name]; ok && len(values) > 0 {
			result.Params[name] = values[0]
			// Add _LABEL for select params with static items
			if s, ok := paramSpecs[name]; ok && s.Type == "select" && s.Options != nil && len(s.Options.Items) > 0 {
				result.Params[name+"_LABEL"] = lookupLiveSelectLabel(s.Options.Items, values[0])
			}
		}
	}

	// Check for missing required params (params with no default)
	for _, requiredName := range routeSpec.GetRequiredQueryParams() {
		if _, ok := result.Params[requiredName]; !ok {
			result.MissingNames = append(result.MissingNames, requiredName)
		}
	}

	return result
}

// lookupLiveSelectLabel finds the label for a given value in a list of live select option items.
// If the value is not found or has no label, the value itself is returned.
func lookupLiveSelectLabel(items []config.LiveQueryParamOptionItem, value string) string {
	for _, item := range items {
		if item.Value == value {
			if item.Label != "" {
				return item.Label
			}
			return value // No label defined, use value
		}
	}
	return value // Value not found in items, use value as-is
}

// queryParamInfo holds info about a query parameter for JSON serialization.
type queryParamInfo struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"` // string, number, number_range, select, date, date_time
	Default     *string            `json:"default,omitempty"`
	Description string             `json:"description,omitempty"`
	Required    bool               `json:"required"`
	Options     *queryParamOptions `json:"options,omitempty"`
}

// queryParamOptions holds options for select, number, and number_range type parameters.
type queryParamOptions struct {
	Items []QueryParamOptionItem `json:"items,omitempty"`
	Min   *float64               `json:"min,omitempty"`
	Max   *float64               `json:"max,omitempty"`
	Step  *float64               `json:"step,omitempty"`
}

// QueryParamOptionItem holds a single option for select type parameters.
type QueryParamOptionItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ResolveDatasetOptions resolves select options from datasets for a route's query parameters.
// Returns a map from parameter name to resolved options.
func ResolveDatasetOptions(ctx context.Context, workdir string, docs []config.Document, routeSpec config.LiveRouteSpec, session *duckdb.Session) map[string][]QueryParamOptionItem {
	result := make(map[string][]QueryParamOptionItem)

	// Find parameters that need dataset resolution
	datasetsNeeded := make(map[string]config.LiveQueryParamSpec)
	for _, p := range routeSpec.QueryParams {
		if p.Options != nil && p.Options.Dataset != "" {
			datasetsNeeded[p.Options.Dataset] = p
		}
	}

	if len(datasetsNeeded) == 0 {
		return result
	}

	// Execute datasets
	var execOpts *dataset.ExecuteOptions
	if session != nil {
		execOpts = &dataset.ExecuteOptions{Session: session}
	}
	datasetResults, _, err := dataset.Execute(ctx, workdir, docs, execOpts)
	if err != nil {
		// Log error but continue - options will be empty
		return result
	}

	// Build lookup of dataset results
	datasetResultMap := make(map[string]json.RawMessage)
	for _, r := range datasetResults {
		datasetResultMap[r.Name] = r.Data
	}

	// Resolve options for each parameter
	for datasetName, paramSpec := range datasetsNeeded {
		data, ok := datasetResultMap[datasetName]
		if !ok {
			continue
		}

		// Parse dataset result as array of objects
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			continue
		}

		valueCol := paramSpec.Options.ValueColumn
		labelCol := paramSpec.Options.LabelColumn
		if labelCol == "" {
			labelCol = valueCol
		}

		items := make([]QueryParamOptionItem, 0, len(rows))
		for _, row := range rows {
			valueRaw, ok := row[valueCol]
			if !ok {
				continue
			}
			value := fmt.Sprintf("%v", valueRaw)

			label := value
			if labelRaw, ok := row[labelCol]; ok {
				label = fmt.Sprintf("%v", labelRaw)
			}

			items = append(items, QueryParamOptionItem{
				Value: value,
				Label: label,
			})
		}

		result[paramSpec.Name] = items
	}

	return result
}
