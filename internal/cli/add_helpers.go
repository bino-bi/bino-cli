package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/schema"
)

// LayoutPageParamData holds data for a parameter definition in a LayoutPage.
type LayoutPageParamData struct {
	Name        string
	Type        string // string, number, boolean, select, date
	Description string
	Default     string
	Required    bool
	Options     *schema.LayoutPageParamOptions
}

// LayoutPageRefData holds data for a LayoutPage reference with optional params.
type LayoutPageRefData struct {
	Page   string
	Params map[string]string
}

// PageParamInfo contains information about a LayoutPage's parameters.
type PageParamInfo struct {
	Name   string
	File   string
	Params []config.LayoutPageParamSpec
}

// promptParamDefinition prompts the user to define a single parameter.
func promptParamDefinition() (*LayoutPageParamData, error) {
	param := &LayoutPageParamData{}

	// Parameter name
	name, err := huhInput("Parameter name (e.g., REGION, YEAR)", "", "", func(s string) error {
		if s == "" {
			return fmt.Errorf("name is required")
		}
		// Validate: uppercase letters, digits, underscores
		for _, r := range s {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
				return fmt.Errorf("parameter names should contain only letters, digits, and underscores")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	param.Name = strings.ToUpper(name) // Convert to uppercase by convention

	// Parameter type
	typeOptions := []huh.Option[string]{
		huh.NewOption("string - Free-form text", schema.ParamTypeString),
		huh.NewOption("number - Numeric value with optional min/max", schema.ParamTypeNumber),
		huh.NewOption("boolean - True/false value", schema.ParamTypeBoolean),
		huh.NewOption("select - Choice from predefined options", schema.ParamTypeSelect),
		huh.NewOption("date - Date value (YYYY-MM-DD)", schema.ParamTypeDate),
	}
	var paramType string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Parameter type").
				Options(typeOptions...).
				Value(&paramType),
		),
	).WithTheme(getHuhTheme())
	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil, errAddCanceled
		}
		return nil, err
	}
	param.Type = paramType

	// Description (optional)
	desc, err := huhInput("Description (optional)", "", "", nil)
	if err != nil {
		return nil, err
	}
	param.Description = desc

	// Required?
	required, err := huhConfirm("Is this parameter required?", false)
	if err != nil {
		return nil, err
	}
	param.Required = required

	// Default value (if not required)
	if !required {
		def, err := huhInput("Default value (optional)", "", "", nil)
		if err != nil {
			return nil, err
		}
		param.Default = def
	}

	// Type-specific options
	switch paramType {
	case schema.ParamTypeSelect:
		options, err := promptSelectOptions()
		if err != nil {
			return nil, err
		}
		if options != nil {
			param.Options = &schema.LayoutPageParamOptions{Items: options}
		}
	case schema.ParamTypeNumber:
		options, err := promptNumberOptions()
		if err != nil {
			return nil, err
		}
		param.Options = options
	}

	return param, nil
}

// promptSelectOptions prompts for select option items.
func promptSelectOptions() ([]schema.LayoutPageParamOptionItem, error) {
	var items []schema.LayoutPageParamOptionItem

	fmt.Println("\nDefine select options (at least 2 required):")

	for i := 1; ; i++ {
		value, err := huhInput(fmt.Sprintf("Option %d value", i), "", "", nil)
		if err != nil {
			return nil, err
		}
		if value == "" {
			if len(items) < 2 {
				fmt.Println("At least 2 options are required.")
				continue
			}
			break
		}

		label, err := huhInput(fmt.Sprintf("Option %d label (optional, defaults to value)", i), "", "", nil)
		if err != nil {
			return nil, err
		}

		items = append(items, schema.LayoutPageParamOptionItem{
			Value: value,
			Label: label,
		})

		if len(items) >= 2 {
			addMore, err := huhConfirm("Add another option?", false)
			if err != nil {
				return nil, err
			}
			if !addMore {
				break
			}
		}
	}

	return items, nil
}

// promptNumberOptions prompts for number type min/max options.
func promptNumberOptions() (*schema.LayoutPageParamOptions, error) {
	addConstraints, err := huhConfirm("Add min/max constraints?", false)
	if err != nil {
		return nil, err
	}
	if !addConstraints {
		return nil, nil
	}

	options := &schema.LayoutPageParamOptions{}

	minStr, err := huhInput("Minimum value (optional)", "", "", nil)
	if err != nil {
		return nil, err
	}
	if minStr != "" {
		if min, err := strconv.ParseFloat(minStr, 64); err == nil {
			options.Min = &min
		}
	}

	maxStr, err := huhInput("Maximum value (optional)", "", "", nil)
	if err != nil {
		return nil, err
	}
	if maxStr != "" {
		if max, err := strconv.ParseFloat(maxStr, 64); err == nil {
			options.Max = &max
		}
	}

	return options, nil
}

// promptParamDefinitions prompts the user to define multiple parameters.
func promptParamDefinitions() ([]LayoutPageParamData, error) {
	var params []LayoutPageParamData

	for {
		param, err := promptParamDefinition()
		if err != nil {
			return params, err
		}
		if param != nil {
			params = append(params, *param)
			fmt.Printf("  Added parameter: %s (%s)\n", param.Name, param.Type)
		}

		addMore, err := huhConfirm("Add another parameter?", false)
		if err != nil {
			return params, err
		}
		if !addMore {
			break
		}
	}

	return params, nil
}

// promptParamValue prompts for a single parameter value based on its spec.
func promptParamValue(param config.LayoutPageParamSpec) (string, error) {
	title := param.Name
	if param.Description != "" {
		title = fmt.Sprintf("%s (%s)", param.Name, param.Description)
	}

	// Handle select type with predefined options
	if param.Type == "select" && param.Options != nil && len(param.Options.Items) > 0 {
		var options []huh.Option[string]
		for _, item := range param.Options.Items {
			label := item.Value
			if item.Label != "" {
				label = fmt.Sprintf("%s (%s)", item.Label, item.Value)
			}
			options = append(options, huh.NewOption(label, item.Value))
		}

		var selected string
		if param.Default != nil {
			selected = *param.Default
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Options(options...).
					Value(&selected),
			),
		).WithTheme(getHuhTheme())

		if err := form.Run(); err != nil {
			if err == huh.ErrUserAborted {
				return "", errAddCanceled
			}
			return "", err
		}
		return selected, nil
	}

	// For other types, use text input with validation
	defaultVal := ""
	if param.Default != nil {
		defaultVal = *param.Default
	}

	placeholder := ""
	switch param.Type {
	case "number":
		placeholder = "Enter a number"
		if param.Options != nil {
			if param.Options.Min != nil && param.Options.Max != nil {
				placeholder = fmt.Sprintf("%.0f - %.0f", *param.Options.Min, *param.Options.Max)
			} else if param.Options.Min != nil {
				placeholder = fmt.Sprintf(">= %.0f", *param.Options.Min)
			} else if param.Options.Max != nil {
				placeholder = fmt.Sprintf("<= %.0f", *param.Options.Max)
			}
		}
	case "boolean":
		placeholder = "true or false"
	case "date":
		placeholder = "YYYY-MM-DD"
	}

	value, err := huhInput(title, placeholder, defaultVal, func(s string) error {
		if s == "" && param.Required && param.Default == nil {
			return fmt.Errorf("%s is required", param.Name)
		}
		// Type-specific validation
		switch param.Type {
		case "number":
			if s != "" {
				if _, err := strconv.ParseFloat(s, 64); err != nil {
					return fmt.Errorf("must be a valid number")
				}
			}
		case "boolean":
			if s != "" && s != "true" && s != "false" {
				return fmt.Errorf("must be 'true' or 'false'")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	// Use default if empty
	if value == "" && defaultVal != "" {
		return defaultVal, nil
	}

	return value, nil
}

// promptParamValues prompts for all parameter values for a page.
func promptParamValues(pageName string, params []config.LayoutPageParamSpec) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}

	fmt.Printf("\nConfigure parameters for %s:\n", pageName)

	values := make(map[string]string)
	for _, param := range params {
		value, err := promptParamValue(param)
		if err != nil {
			return nil, err
		}
		if value != "" {
			values[param.Name] = value
		}
	}

	return values, nil
}

// detectPageParams loads and returns the parameters defined on a LayoutPage.
func detectPageParams(workdir string, pageName string, manifests []ManifestInfo) ([]config.LayoutPageParamSpec, error) {
	// Find the manifest file for this page
	var pageManifest *ManifestInfo
	for _, m := range manifests {
		if m.Kind == "LayoutPage" && m.Name == pageName {
			pageManifest = &m
			break
		}
	}
	if pageManifest == nil {
		return nil, nil
	}

	// Read the file and parse to find params
	content, err := os.ReadFile(pageManifest.File)
	if err != nil {
		return nil, nil
	}

	// Parse multi-document YAML to find the right document
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	position := 0
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		position++

		if position != pageManifest.Position {
			continue
		}

		// Check if this document has params
		metadata, ok := doc["metadata"].(map[string]any)
		if !ok {
			return nil, nil
		}

		paramsRaw, ok := metadata["params"]
		if !ok {
			return nil, nil
		}

		// Convert to JSON and back to parse into struct
		paramsJSON, err := json.Marshal(paramsRaw)
		if err != nil {
			return nil, nil
		}

		var params []config.LayoutPageParamSpec
		if err := json.Unmarshal(paramsJSON, &params); err != nil {
			return nil, nil
		}

		return params, nil
	}

	return nil, nil
}

// getPageParamsInfo returns parameter info for all LayoutPages that have params.
func getPageParamsInfo(workdir string, manifests []ManifestInfo) (map[string][]config.LayoutPageParamSpec, error) {
	result := make(map[string][]config.LayoutPageParamSpec)

	for _, m := range manifests {
		if m.Kind != "LayoutPage" {
			continue
		}

		params, err := detectPageParams(workdir, m.Name, manifests)
		if err != nil {
			continue
		}
		if len(params) > 0 {
			result[m.Name] = params
		}
	}

	return result, nil
}

// updateArtefactLayoutPages adds a page to an existing artefact's layoutPages.
func updateArtefactLayoutPages(artefactPath string, pageRef LayoutPageRefData) error {
	content, err := os.ReadFile(artefactPath)
	if err != nil {
		return fmt.Errorf("read artefact file: %w", err)
	}

	// Parse multi-document YAML
	var documents []map[string]any
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		documents = append(documents, doc)
	}

	// Find and update ReportArtefact documents
	updated := false
	for i, doc := range documents {
		kind, _ := doc["kind"].(string)
		if kind != "ReportArtefact" {
			continue
		}

		spec, ok := doc["spec"].(map[string]any)
		if !ok {
			spec = make(map[string]any)
			doc["spec"] = spec
		}

		// Get existing layoutPages
		var layoutPages []any
		if existing, ok := spec["layoutPages"].([]any); ok {
			layoutPages = existing
		}

		// Add the new page reference
		if len(pageRef.Params) > 0 {
			// Object form with params
			layoutPages = append(layoutPages, map[string]any{
				"page":   pageRef.Page,
				"params": pageRef.Params,
			})
		} else {
			// Simple string form
			layoutPages = append(layoutPages, pageRef.Page)
		}

		spec["layoutPages"] = layoutPages
		documents[i] = doc
		updated = true
	}

	if !updated {
		return fmt.Errorf("no ReportArtefact found in %s", artefactPath)
	}

	// Write back
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	for i, doc := range documents {
		if i > 0 {
			buf.WriteString("---\n")
		}
		if err := encoder.Encode(doc); err != nil {
			return fmt.Errorf("encode document: %w", err)
		}
	}
	encoder.Close()

	if err := os.WriteFile(artefactPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("write artefact file: %w", err)
	}

	return nil
}

// promptAddToArtefacts prompts to add a newly created page to existing artefacts.
func promptAddToArtefacts(workdir string, pageName string, manifests []ManifestInfo, pageParams []LayoutPageParamData) error {
	// Find existing ReportArtefacts
	artefacts := FilterByKind(manifests, "ReportArtefact")
	if len(artefacts) == 0 {
		return nil
	}

	addToArtefact, err := huhConfirm("Add this page to an existing ReportArtefact?", false)
	if err != nil || !addToArtefact {
		return err
	}

	// Select artefact(s)
	items := ManifestsToFuzzyItems(artefacts)
	selected, err := huhMultiFuzzySelect("Select artefact(s)", items)
	if err != nil {
		return err
	}

	for _, item := range selected {
		pageRef := LayoutPageRefData{Page: pageName}

		// If page has params, prompt for values
		if len(pageParams) > 0 {
			withParams, err := huhConfirm(fmt.Sprintf("Add %s to %s with parameters?", pageName, item.Name), true)
			if err != nil {
				return err
			}
			if withParams {
				// Convert LayoutPageParamData to config.LayoutPageParamSpec for prompting
				var paramSpecs []config.LayoutPageParamSpec
				for _, p := range pageParams {
					var def *string
					if p.Default != "" {
						def = &p.Default
					}
					spec := config.LayoutPageParamSpec{
						Name:        p.Name,
						Type:        p.Type,
						Description: p.Description,
						Default:     def,
						Required:    p.Required,
					}
					if p.Options != nil {
						spec.Options = &config.LayoutPageParamOptions{}
						for _, item := range p.Options.Items {
							spec.Options.Items = append(spec.Options.Items, config.LayoutPageParamOptionItem{
								Value: item.Value,
								Label: item.Label,
							})
						}
						spec.Options.Min = p.Options.Min
						spec.Options.Max = p.Options.Max
					}
					paramSpecs = append(paramSpecs, spec)
				}

				values, err := promptParamValues(pageName, paramSpecs)
				if err != nil {
					return err
				}
				pageRef.Params = values
			}
		}

		// Update the artefact file
		artefactPath := item.File
		if !filepath.IsAbs(artefactPath) {
			artefactPath = filepath.Join(workdir, artefactPath)
		}
		if err := updateArtefactLayoutPages(artefactPath, pageRef); err != nil {
			fmt.Printf("Warning: could not update %s: %v\n", item.Name, err)
		} else {
			if len(pageRef.Params) > 0 {
				fmt.Printf("  Added %s to %s with params %v\n", pageName, item.Name, pageRef.Params)
			} else {
				fmt.Printf("  Added %s to %s\n", pageName, item.Name)
			}
		}
	}

	return nil
}

// convertParamDataToSchemaParams converts CLI param data to schema params for YAML generation.
func convertParamDataToSchemaParams(params []LayoutPageParamData) []schema.LayoutPageParamSpec {
	var result []schema.LayoutPageParamSpec
	for _, p := range params {
		spec := schema.LayoutPageParamSpec{
			Name:        p.Name,
			Type:        p.Type,
			Description: p.Description,
			Default:     p.Default,
			Required:    p.Required,
		}
		if p.Options != nil {
			spec.Options = p.Options
		}
		result = append(result, spec)
	}
	return result
}

// convertSchemaParamsToConfigParams converts schema params to config params for prompting.
func convertSchemaParamsToConfigParams(params []schema.LayoutPageParamSpec) []config.LayoutPageParamSpec {
	var result []config.LayoutPageParamSpec
	for _, p := range params {
		var def *string
		if p.Default != "" {
			def = &p.Default
		}
		spec := config.LayoutPageParamSpec{
			Name:        p.Name,
			Type:        p.Type,
			Description: p.Description,
			Default:     def,
			Required:    p.Required,
		}
		if p.Options != nil {
			spec.Options = &config.LayoutPageParamOptions{}
			for _, item := range p.Options.Items {
				spec.Options.Items = append(spec.Options.Items, config.LayoutPageParamOptionItem{
					Value: item.Value,
					Label: item.Label,
				})
			}
			spec.Options.Min = p.Options.Min
			spec.Options.Max = p.Options.Max
		}
		result = append(result, spec)
	}
	return result
}
