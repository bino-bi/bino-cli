package embed

import (
	"slices"
	"testing"
)

// The root-renderable set backs the two root-component switches in
// render/html.go; it deliberately differs from componentKinds (LayoutCard and
// Image render at root but are not embeddable manifest kinds; Tree and Grid
// are embeddable via the preview's synthetic page but have no direct root
// render path).
func TestIsRootRenderable(t *testing.T) {
	for _, kind := range []string{"LayoutCard", "Text", "Table", "ChartStructure", "ChartTime", "ChartScatter", "ChartBubble", "ChartBullet", "Image"} {
		if !IsRootRenderable(kind) {
			t.Errorf("IsRootRenderable(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"Tree", "Grid", "LayoutPage", "DataSet", "Bogus"} {
		if IsRootRenderable(kind) {
			t.Errorf("IsRootRenderable(%q) = true, want false", kind)
		}
	}
}

// Every root-renderable kind except the layout-child-only Image must be a
// known built-in kind — the set cannot drift ahead of the registry.
func TestRootRenderableKindsAreKnown(t *testing.T) {
	known := AllBuiltinKinds()
	for _, kind := range RootRenderableKinds() {
		if kind == "Image" {
			continue // layout-child kind, never a top-level manifest
		}
		if !slices.Contains(known, kind) {
			t.Errorf("root-renderable kind %q is not in the builtin kind registry", kind)
		}
	}
}
