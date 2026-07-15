// Package projectlayout is the single source of truth for the canonical
// folder-per-kind on-disk layout of a bino report project. It is consumed by
// `bino add`, the LSP wizard, the MCP authoring tools, and the built-in
// `standard` init template so they all agree on where a manifest of a given
// kind belongs.
package projectlayout

import (
	"sort"

	"bino.bi/bino/internal/schema"
)

// FallbackDir is the directory used for kinds without a dedicated canonical
// folder.
const FallbackDir = "manifests"

// dirForKind maps each manifest kind to its canonical folder. Kinds absent from
// the map fall back to FallbackDir (preserving the previous switch behavior for
// Tree, Grid, and DocumentArtefact).
var dirForKind = map[string]string{
	schema.KindDataSet:              "datasets",
	schema.KindDataSource:           "datasources",
	schema.KindConnectionSecret:     "secrets",
	schema.KindAsset:                "resources",
	schema.KindLayoutPage:           "pages",
	schema.KindLayoutCard:           "pages",
	schema.KindChartStructure:       "components",
	schema.KindChartTime:            "components",
	schema.KindChartScatter:         "components",
	schema.KindChartBubble:          "components",
	schema.KindTable:                "components",
	schema.KindText:                 "components",
	schema.KindComponentStyle:       "styles",
	schema.KindRuleSet:              "styles",
	schema.KindInternationalization: "i18n",
	schema.KindScalingGroup:         "scaling",
	schema.KindReportArtefact:       "reports",
	schema.KindLiveReportArtefact:   "reports",
	schema.KindSigningProfile:       "signing",
}

// DirForKind returns the canonical folder for a manifest kind. Kinds without a
// dedicated folder fall back to FallbackDir.
func DirForKind(kind string) string {
	if dir, ok := dirForKind[kind]; ok {
		return dir
	}
	return FallbackDir
}

// CanonicalFolders returns the sorted, de-duplicated set of canonical folders
// (excluding the FallbackDir). The built-in `standard` template's tree draws
// from this set.
func CanonicalFolders() []string {
	seen := make(map[string]struct{}, len(dirForKind))
	folders := make([]string, 0, len(dirForKind))
	for _, dir := range dirForKind {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		folders = append(folders, dir)
	}
	sort.Strings(folders)
	return folders
}
