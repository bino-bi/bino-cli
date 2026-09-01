package projectlayout

import (
	"reflect"
	"sort"
	"testing"

	"bino.bi/bino/internal/pathutil"
)

// TestDefaultPackageIncludeMatchesLayout guards the literal default include
// list in internal/pathutil against the canonical layout. pathutil is the
// config leaf and must not import projectlayout, so the parity check lives
// here. Adding a kind with a new canonical folder fails this test until the
// default include list is updated deliberately.
func TestDefaultPackageIncludeMatchesLayout(t *testing.T) {
	want := []string{FallbackDir}
	for _, folder := range CanonicalFolders() {
		if folder == "reports" {
			continue
		}
		want = append(want, folder)
	}
	sort.Strings(want)

	got := pathutil.DefaultPackageIncludeDirs()
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultPackageIncludeDirs() = %v, want %v", got, want)
	}
}
