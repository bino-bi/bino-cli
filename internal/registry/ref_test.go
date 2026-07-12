package registry

import "testing"

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    Spec
		wantErr bool
	}{
		{in: "@acme/revenue-table", want: Spec{Name: "@acme/revenue-table", Scope: "acme", Base: "revenue-table"}},
		{in: "@acme/revenue-table@1.2.3", want: Spec{Name: "@acme/revenue-table", Scope: "acme", Base: "revenue-table", Ref: "1.2.3"}},
		{in: "@acme/revenue-table@latest", want: Spec{Name: "@acme/revenue-table", Scope: "acme", Base: "revenue-table", Ref: "latest"}},
		{in: "@a1/b_2@stable", want: Spec{Name: "@a1/b_2", Scope: "a1", Base: "b_2", Ref: "stable"}},
		{in: "@acme/tbl@2.0.0-rc.1", want: Spec{Name: "@acme/tbl", Scope: "acme", Base: "tbl", Ref: "2.0.0-rc.1"}},
		{in: "acme/revenue-table", wantErr: true},  // missing @
		{in: "@acme", wantErr: true},               // missing name segment
		{in: "@Acme/table", wantErr: true},         // uppercase scope
		{in: "@acme/a/b", wantErr: true},           // nested slash
		{in: "@/table", wantErr: true},             // empty scope
		{in: "@acme/-table", wantErr: true},        // leading hyphen
		{in: "@acme/table@", wantErr: true},        // empty ref
		{in: "@acme/table@Bad Tag", wantErr: true}, // invalid tag chars
		{in: "@acme/table@1.2", wantErr: true},     // not semver, not token (dot)
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseSpec(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseSpec(%q): expected error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSpec(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestIsPin(t *testing.T) {
	// Mirrors the server's resolve-route semverRe, including prerelease/build.
	pins := []string{"1.2.3", "0.0.1", "2.2.0-rc.1", "1.0.0+build.5", "1.0.0-alpha+001"}
	tags := []string{"latest", "stable", "beta", "v1.2.3", "1.2", "1.2.3.4", ""}
	for _, p := range pins {
		if !IsPin(p) {
			t.Errorf("IsPin(%q) = false, want true", p)
		}
	}
	for _, tag := range tags {
		if IsPin(tag) {
			t.Errorf("IsPin(%q) = true, want false", tag)
		}
	}
}
