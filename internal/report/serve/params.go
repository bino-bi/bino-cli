// Package serve generates the HTML shell, navigation script, and query
// parameter handling for serving LiveReportArtefacts over HTTP. It contains
// the render-side logic of `bino serve`; the HTTP handlers and caching live
// in internal/cli.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/pkg/duckdb"
)

// QueryParamValidationResult holds the result of query parameter validation.
type QueryParamValidationResult struct {
	Params       map[string]string // Merged parameters (request values + defaults)
	MissingNames []string          // Names of missing required parameters and of parameters with a rejected value
}

// IsValid returns true if no required parameter is missing and no value was rejected.
func (r QueryParamValidationResult) IsValid() bool {
	return len(r.MissingNames) == 0
}

// ValidateAndMergeQueryParams validates query parameters against route spec.
// Returns merged params (request values + defaults) and list of missing required params.
// Missing params are reported in the result, not as an error.
// A request value that breaks the declared type or options is not merged; its param is listed as missing too.
// Defaults are not checked.
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
	for _, p := range routeSpec.QueryParams {
		spec := valueCheckSpec(p)
		value, sent, valid := requestValue(requestQuery, p.Name, spec)
		if valid && p.Type == "number_range" {
			// The range slider sends its upper end as NAME_max.
			_, _, valid = requestValue(requestQuery, p.Name+"_max", spec)
		}
		if !valid {
			result.MissingNames = append(result.MissingNames, p.Name)
			continue
		}
		if sent {
			result.Params[p.Name] = value
			// Add _LABEL for select params with static items
			if p.Type == "select" && p.Options != nil && len(p.Options.Items) > 0 {
				result.Params[p.Name+"_LABEL"] = lookupLiveSelectLabel(p.Options.Items, value)
			}
		}
	}

	// Check for missing required params (params with no default)
	for _, requiredName := range routeSpec.GetRequiredQueryParams() {
		if _, ok := result.Params[requiredName]; !ok && !slices.Contains(result.MissingNames, requiredName) {
			result.MissingNames = append(result.MissingNames, requiredName)
		}
	}

	return result
}

// requestValue returns the first request value sent under name and whether it
// satisfies spec. An empty value that the declared type cannot hold counts as
// not sent, so the default applies.
func requestValue(requestQuery map[string][]string, name string, spec config.LayoutPageParamSpec) (value string, sent, valid bool) {
	values := requestQuery[name]
	if len(values) == 0 {
		return "", false, true
	}
	err := config.CheckParamValue("query", name, values[0], spec)
	switch {
	case err == nil:
		return values[0], true, true
	case values[0] == "":
		return "", false, true
	default:
		return "", false, false
	}
}

// valueCheckSpec adapts a query param to the spec config.CheckParamValue takes.
// Both ends of a number_range are plain numbers.
func valueCheckSpec(p config.LiveQueryParamSpec) config.LayoutPageParamSpec {
	spec := config.LayoutPageParamSpec{Type: p.Type}
	if p.Type == "number_range" {
		spec.Type = "number"
	}
	if p.Options != nil {
		spec.Options = &config.LayoutPageParamOptions{Min: p.Options.Min, Max: p.Options.Max}
		// The sidebar offers the dataset rows when a dataset is set, so the static items are not the valid set.
		if p.Options.Dataset == "" {
			for _, item := range p.Options.Items {
				spec.Options.Items = append(spec.Options.Items, config.LayoutPageParamOptionItem(item))
			}
		}
	}
	return spec
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
