package schema

import (
	"encoding/json"
	"slices"
	"sort"
	"testing"

	embedkinds "bino.bi/bino/internal/report/embed"
)

// kindConstants lists every Kind* constant declared in types.go. When a kind
// is added to document.schema.json's kind enum it must also be added to
// types.go, to embed's builtinCategory, and to this list — otherwise
// TestKindRegistryParity fails.
var kindConstants = []string{
	KindDataSet,
	KindDataSource,
	KindConnectionSecret,
	KindLayoutPage,
	KindLayoutCard,
	KindText,
	KindTable,
	KindChartStructure,
	KindChartTime,
	KindChartScatter,
	KindChartBubble,
	KindChartBullet,
	KindTree,
	KindGrid,
	KindAsset,
	KindComponentStyle,
	KindRuleSet,
	KindInternationalization,
	KindReportArtefact,
	KindLiveReportArtefact,
	KindScreenshotArtefact,
	KindDocumentArtefact,
	KindSigningProfile,
	KindScalingGroup,
}

func schemaKindEnum(t *testing.T) []string {
	t.Helper()
	var doc struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(DocumentSchemaBytes(), &doc); err != nil {
		t.Fatalf("parse embedded document schema: %v", err)
	}
	if len(doc.Properties.Kind.Enum) == 0 {
		t.Fatal("embedded document schema has no kind enum")
	}
	return doc.Properties.Kind.Enum
}

func sortedKindSet(t *testing.T, label string, kinds []string) []string {
	t.Helper()
	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("%s lists %q twice", label, k)
		}
		seen[k] = true
	}
	out := append([]string(nil), kinds...)
	sort.Strings(out)
	return out
}

// TestKindRegistryParity asserts the three kind registries agree: the schema
// kind enum (document.schema.json), the Kind* constants (types.go), and
// embed's builtinCategory. Divergence between them is how a kind silently
// vanishes from IsBinoHeader, bino://kinds, and the graph.
func TestKindRegistryParity(t *testing.T) {
	enum := sortedKindSet(t, "schema kind enum", schemaKindEnum(t))
	consts := sortedKindSet(t, "schema.Kind* constants", kindConstants)
	registry := sortedKindSet(t, "embed.AllBuiltinKinds", embedkinds.AllBuiltinKinds())

	if !slices.Equal(enum, consts) {
		t.Errorf("schema kind enum and schema.Kind* constants diverge:\n  enum:   %v\n  consts: %v", enum, consts)
	}
	if !slices.Equal(enum, registry) {
		t.Errorf("schema kind enum and embed.AllBuiltinKinds diverge:\n  enum:     %v\n  registry: %v", enum, registry)
	}
}
