package projectlayout

import (
	"reflect"
	"testing"

	"bino.bi/bino/internal/schema"
)

func TestDirForKind(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		// Aligned mappings (changed from the previous switch).
		{schema.KindAsset, "resources"},
		{schema.KindLayoutPage, "pages"},
		{schema.KindLayoutCard, "pages"},
		// Unchanged mappings (spot checks across each folder).
		{schema.KindDataSet, "datasets"},
		{schema.KindDataSource, "datasources"},
		{schema.KindConnectionSecret, "secrets"},
		{schema.KindChartStructure, "components"},
		{schema.KindChartTime, "components"},
		{schema.KindTable, "components"},
		{schema.KindText, "components"},
		{schema.KindComponentStyle, "styles"},
		{schema.KindInternationalization, "i18n"},
		{schema.KindScalingGroup, "scaling"},
		{schema.KindReportArtefact, "reports"},
		{schema.KindLiveReportArtefact, "reports"},
		{schema.KindSigningProfile, "signing"},
		// Kinds without a dedicated folder fall back (preserves prior behavior).
		{schema.KindTree, FallbackDir},
		{schema.KindGrid, FallbackDir},
		{schema.KindDocumentArtefact, FallbackDir},
		{"NotAKind", FallbackDir},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := DirForKind(tt.kind); got != tt.want {
				t.Errorf("DirForKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestCanonicalFolders(t *testing.T) {
	want := []string{
		"components",
		"datasets",
		"datasources",
		"i18n",
		"pages",
		"reports",
		"resources",
		"scaling",
		"secrets",
		"signing",
		"styles",
	}
	if got := CanonicalFolders(); !reflect.DeepEqual(got, want) {
		t.Errorf("CanonicalFolders() = %v, want %v", got, want)
	}
}
