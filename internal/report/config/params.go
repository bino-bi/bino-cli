package config

import "encoding/json"

// ParamCapableKinds is the set of document kinds that may declare parameters
// via metadata.params. LayoutPage params are supplied by layoutPages refs in
// artefacts; the component kinds are supplied by `params` on ref children.
var ParamCapableKinds = map[string]struct{}{
	"LayoutPage":     {},
	"Text":           {},
	"Table":          {},
	"ChartStructure": {},
	"ChartTime":      {},
	"ChartScatter":   {},
	"ChartBubble":    {},
	"ChartBullet":    {},
	"Tree":           {},
	"Grid":           {},
	"LayoutCard":     {},
	"Image":          {},
}

// ExpandDocParams expands parameter references in a document's raw content
// using ${PARAM} substitution:
//   - refParams values may themselves contain ${VAR} references resolved from ENV
//   - effective values are built with precedence: explicit refParams > declared
//     defaults > environment variable of the same name
//   - for select params, ${PARAM_LABEL} is also available with the label from
//     the matching option item
//   - any other ${VAR} in the content falls back to the environment
//
// Returns the expanded content and the effective parameter values.
func ExpandDocParams(raw json.RawMessage, declared []LayoutPageParamSpec, refParams map[string]string) (expanded json.RawMessage, effective map[string]string) {
	envLookup := EnvLookup()

	// Step 1: Expand param values themselves (they may contain ${VAR} from ENV)
	expandedParams := make(map[string]string)
	for k, v := range refParams {
		val, _ := ExpandVars(v, envLookup)
		expandedParams[k] = val
	}

	// Step 2: Build effective params: explicit params > defaults > ENV fallback
	// Also add _LABEL variants for select type params
	effectiveParams := make(map[string]string)
	for _, def := range declared {
		// Priority: explicit params > defaults > env
		if v, ok := expandedParams[def.Name]; ok {
			effectiveParams[def.Name] = v
			// For select params, also set the _LABEL variant
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, v)
			}
		} else if def.Default != nil {
			effectiveParams[def.Name] = *def.Default
			// For select params with default, also set the label
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, *def.Default)
			}
		} else if envVal, found := envLookup(def.Name); found {
			// Param comes from environment variable - still look up its label
			effectiveParams[def.Name] = envVal
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, envVal)
			}
		}
	}

	// Step 3: Create lookup chain: params > ENV (fallback for non-param vars)
	lookup := ChainLookup(
		MapLookup(effectiveParams),
		envLookup,
	)

	// Step 4: Expand document content
	expandedContent, _ := ExpandVars(string(raw), lookup)

	return json.RawMessage(expandedContent), effectiveParams
}

// lookupSelectLabel finds the label for a given value in a list of select option items.
// If the value is not found or has no label, the value itself is returned.
func lookupSelectLabel(items []LayoutPageParamOptionItem, value string) string {
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
