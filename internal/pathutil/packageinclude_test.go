package pathutil

import (
	"path/filepath"
	"testing"
)

func TestIncludeSetDefaults(t *testing.T) {
	pkg := &PackageConfig{Name: "@acme/kit"}
	set := pkg.IncludeSet("/proj")
	if set == nil {
		t.Fatal("expected non-nil include set")
	}
	if len(set.entries) != len(DefaultPackageIncludeDirs()) {
		t.Fatalf("entries = %v, want the default list %v", set.entries, DefaultPackageIncludeDirs())
	}
	if !set.Contains("/proj/components/x.yaml") {
		t.Fatal("expected the default set to contain components/x.yaml")
	}
}

func TestIncludeSetContainsDefaults(t *testing.T) {
	set := (&PackageConfig{Name: "@acme/kit"}).IncludeSet("/proj")
	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "component", path: "/proj/components/x.yaml", want: true},
		{name: "nested style", path: "/proj/styles/a/b.yaml", want: true},
		{name: "asset", path: "/proj/resources/assets/logo.svg", want: true},
		{name: "manifests fallback", path: "/proj/manifests/x.yaml", want: true},
		{name: "secrets included by default", path: "/proj/secrets/x.yaml", want: true},
		{name: "signing included by default", path: "/proj/signing/x.yaml", want: true},
		{name: "mocks excluded", path: "/proj/mocks/x.yaml", want: false},
		{name: "reports excluded", path: "/proj/reports/x.yaml", want: false},
		{name: "installed dependency excluded", path: "/proj/.bino/registry/acme/kit/x.yaml", want: false},
		{name: "undeclared folder", path: "/proj/docs/x.md", want: false},
		{name: "outside the project", path: "/other/x.yaml", want: false},
		{name: "escaping the project", path: "/proj/../other/components/x.yaml", want: false},
		{name: "prefix of a folder name", path: "/proj/components-old/x.yaml", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := set.Contains(tc.path); got != tc.want {
				t.Fatalf("Contains(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestIncludeSetContainsExplicitEntries(t *testing.T) {
	pkg := &PackageConfig{
		Name:    "@acme/kit",
		Include: []string{"components", "styles/corporate.yaml", "/etc", "../escape"},
	}
	set := pkg.IncludeSet("/proj")
	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "declared dir", path: "/proj/components/x.yaml", want: true},
		{name: "declared single file", path: "/proj/styles/corporate.yaml", want: true},
		{name: "sibling of declared file", path: "/proj/styles/other.yaml", want: false},
		{name: "undeclared default dir", path: "/proj/datasets/x.yaml", want: false},
		{name: "invalid entries dropped", path: "/etc/passwd", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := set.Contains(tc.path); got != tc.want {
				t.Fatalf("Contains(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestIncludeSetContainsWholeProject(t *testing.T) {
	set := (&PackageConfig{Name: "@acme/kit", Include: []string{"."}}).IncludeSet("/proj")
	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "any folder", path: "/proj/docs/x.md", want: true},
		{name: "root file", path: "/proj/bino.toml", want: true},
		{name: "mocks still excluded", path: "/proj/mocks/x.yaml", want: false},
		{name: "reports still excluded", path: "/proj/reports/x.yaml", want: false},
		{name: "bino state still excluded", path: "/proj/.bino/registry/acme/kit/x.yaml", want: false},
		// "." matches every relative path, so it is the one entry for which the
		// escape guard is load-bearing: without it any absolute path anywhere
		// on the filesystem would be reported as package content.
		{name: "outside the project", path: "/other/x.yaml", want: false},
		{name: "escaping the project", path: "/proj/../other/x.yaml", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := set.Contains(tc.path); got != tc.want {
				t.Fatalf("Contains(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestIncludeSetNative(t *testing.T) {
	// Built with filepath.Join so Windows CI exercises the separator handling.
	root := filepath.Join(string(filepath.Separator), "proj")
	set := (&PackageConfig{Name: "@acme/kit"}).IncludeSet(root)
	if !set.Contains(filepath.Join(root, "components", "x.yaml")) {
		t.Fatal("expected a natively joined component path to be included")
	}
	if set.Contains(filepath.Join(root, "mocks", "x.yaml")) {
		t.Fatal("expected a natively joined mocks path to be excluded")
	}
}

func TestIncludeSetNil(t *testing.T) {
	var pkg *PackageConfig
	set := pkg.IncludeSet("/proj")
	if set != nil {
		t.Fatalf("expected nil include set for a nil receiver, got %+v", set)
	}
	if set.Contains("/proj/components/x.yaml") {
		t.Fatal("expected a nil include set to contain nothing")
	}
}
