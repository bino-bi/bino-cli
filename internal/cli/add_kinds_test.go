package cli

import "testing"

// Every registered `bino add` subcommand must appear in kindCategories, or it
// is invisible to the interactive selector, shell completion, and the --help
// prose that is generated from the same list.
func TestAddSubcommandsCoveredByKindSelector(t *testing.T) {
	names := make(map[string]bool)
	for _, n := range allKindNames() {
		names[n] = true
	}
	for _, sub := range newAddCommand().Commands() {
		if !names[sub.Name()] {
			t.Errorf("bino add %s is registered but missing from kindCategories (selector, completion, help)", sub.Name())
		}
	}
}

// Every selector kind must have AddConfig storage, or its output location is
// never remembered across wizard runs.
func TestKindConfigRoundTripForSelectorKinds(t *testing.T) {
	kindForName := map[string]string{
		"dataset":              "DataSet",
		"datasource":           "DataSource",
		"connectionsecret":     "ConnectionSecret",
		"layoutpage":           "LayoutPage",
		"layoutcard":           "LayoutCard",
		"text":                 "Text",
		"table":                "Table",
		"chartstructure":       "ChartStructure",
		"charttime":            "ChartTime",
		"chartscatter":         "ChartScatter",
		"chartbubble":          "ChartBubble",
		"chartbullet":          "ChartBullet",
		"asset":                "Asset",
		"componentstyle":       "ComponentStyle",
		"ruleset":              "RuleSet",
		"internationalization": "Internationalization",
		"scalinggroup":         "ScalingGroup",
		"reportartefact":       "ReportArtefact",
		"livereportartefact":   "LiveReportArtefact",
		"signingprofile":       "SigningProfile",
	}
	for _, name := range allKindNames() {
		kind, ok := kindForName[name]
		if !ok {
			t.Errorf("no manifest-kind mapping for selector entry %q — extend this test", name)
			continue
		}
		cfg := &AddConfig{}
		kc := &KindConfig{Mode: "separate-files", Directory: "manifests"}
		cfg.SetKindConfig(kind, kc)
		if got := cfg.GetKindConfig(kind); got != kc {
			t.Errorf("AddConfig does not store kind %s (GetKindConfig after SetKindConfig returned %v)", kind, got)
		}
	}
}
