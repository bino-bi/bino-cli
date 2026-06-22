package lsp

import "strings"

// scenarioDoc maps an IBCS scenario prefix to its meaning. This is the only IBCS
// knowledge the LSP carries; it powers hover and completion documentation for
// scenario/variance tokens.
var scenarioDoc = map[string]string{
	"ac": "Actual — measured/realized values.",
	"pp": "Previous Period — the comparable prior period (or Plan, per convention).",
	"fc": "Forecast — expected values for the remaining horizon.",
	"pl": "Plan — budgeted/target values.",
}

// varianceSentiment documents the sentiment suffix of the variance grammar.
var varianceSentiment = map[string]string{
	"pos": "positive sentiment — more is better (e.g. revenue).",
	"neg": "negative sentiment — more is worse (e.g. cost).",
	"neu": "neutral sentiment — no inherent good/bad direction.",
}

// scenarioMeaning returns hover documentation for a scenario slot like "ac1" or
// "pp2", or "" when the token is not a recognised scenario.
func scenarioMeaning(slot string) string {
	if len(slot) < 2 {
		return ""
	}
	if doc, ok := scenarioDoc[slot[:2]]; ok {
		return doc
	}
	return ""
}

// varianceMeaning explains a variance token of the grammar d{B}_{A}_{sentiment}
// (or relative dr_...), or "" when it does not parse.
func varianceMeaning(token string) string {
	t := token
	rel := false
	switch {
	case strings.HasPrefix(t, "dr"):
		rel = true
		t = strings.TrimPrefix(t, "dr")
	case strings.HasPrefix(t, "d"):
		t = strings.TrimPrefix(t, "d")
	default:
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(t, "_"), "_")
	if len(parts) != 3 {
		return ""
	}
	base, cmp, sent := parts[0], parts[1], parts[2]
	kind := "absolute"
	if rel {
		kind = "relative (%)"
	}
	b := strings.Builder{}
	b.WriteString(kind)
	b.WriteString(" variance of ")
	b.WriteString(cmp)
	b.WriteString(" vs ")
	b.WriteString(base)
	if s, ok := varianceSentiment[sent]; ok {
		b.WriteString("; ")
		b.WriteString(s)
	}
	return b.String()
}
