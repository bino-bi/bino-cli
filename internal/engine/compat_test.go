package engine

import (
	"errors"
	"testing"
)

func TestCheckCompatibility_DefaultRanges(t *testing.T) {
	t.Helper()
	cases := []struct {
		version string
		ok      bool
	}{
		{"v1.0.0-alpha.19", true},
		{"1.0.0-alpha.19", true}, // leading v optional
		{"v1.0.0-alpha.20", true},
		{"v1.0.0-rc.1", true},
		{"v1.0.0", true},
		{"v1.5.2", true},
		{"v1.0.0-alpha.14", false}, // pre-merge i18n engines are unsafe
		{"v1.0.0-alpha", false},
		{"v0.9.0", false},
		{"v2.0.0-alpha.1", false},
		{"v2.0.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			err := CheckCompatibility(tc.version)
			if tc.ok && err != nil {
				t.Fatalf("expected %s to be supported, got %v", tc.version, err)
			}
			if !tc.ok {
				cErr, ok := errors.AsType[*CompatibilityError](err)
				if !ok {
					t.Fatalf("expected CompatibilityError for %s, got %v", tc.version, err)
				}
				if cErr.EngineVersion != tc.version {
					t.Errorf("EngineVersion = %q, want %q", cErr.EngineVersion, tc.version)
				}
				if len(cErr.Ranges) == 0 {
					t.Error("Ranges should not be empty")
				}
			}
		})
	}
}

func TestCheckCompatibility_RangeMatcher(t *testing.T) {
	t.Helper()
	prev := SupportedEngineRanges
	t.Cleanup(func() { SupportedEngineRanges = prev })

	type tc struct {
		name    string
		ranges  []string
		version string
		ok      bool
	}
	cases := []tc{
		// exact
		{"exact match", []string{"1.2.3"}, "1.2.3", true},
		{"exact mismatch", []string{"1.2.3"}, "1.2.4", false},
		{"exact eq operator", []string{"=1.2.3"}, "1.2.3", true},

		// comparators
		{">=", []string{">=1.2.0"}, "1.2.0", true},
		{">= reject", []string{">=1.2.0"}, "1.1.9", false},
		{">", []string{">1.2.0"}, "1.2.1", true},
		{"> reject equal", []string{">1.2.0"}, "1.2.0", false},
		{"<", []string{"<2.0.0"}, "1.9.9", true},
		{"<= equal", []string{"<=1.2.0"}, "1.2.0", true},
		{"!=", []string{"!=1.2.3, >=1.0.0"}, "1.2.4", true},
		{"!= reject", []string{"!=1.2.3, >=1.0.0"}, "1.2.3", false},

		// hyphen
		{"hyphen lower", []string{"1.0.0 - 1.5.0"}, "1.0.0", true},
		{"hyphen upper", []string{"1.0.0 - 1.5.0"}, "1.5.0", true},
		{"hyphen mid", []string{"1.0.0 - 1.5.0"}, "1.3.0", true},
		{"hyphen above", []string{"1.0.0 - 1.5.0"}, "1.5.1", false},
		{"hyphen below", []string{"1.0.0 - 1.5.0"}, "0.9.9", false},

		// x-range
		{"x-range patch", []string{"1.2.x"}, "1.2.99", true},
		{"x-range patch reject", []string{"1.2.x"}, "1.3.0", false},
		{"x-range minor", []string{"1.x"}, "1.99.0", true},
		{"x-range minor reject", []string{"1.x"}, "2.0.0", false},
		{"wildcard", []string{"*"}, "9.9.9", true},

		// tilde
		{"tilde patch ok", []string{"~1.2.3"}, "1.2.99", true},
		{"tilde minor reject", []string{"~1.2.3"}, "1.3.0", false},

		// caret
		{"caret minor ok", []string{"^1.2.3"}, "1.99.0", true},
		{"caret major reject", []string{"^1.2.3"}, "2.0.0", false},
		{"caret 0.x", []string{"^0.2.3"}, "0.2.99", true},
		{"caret 0.x reject minor", []string{"^0.2.3"}, "0.3.0", false},

		// AND within range, OR across array
		{"AND within", []string{">=1.0.0, <2.0.0"}, "1.5.0", true},
		{"AND within reject upper", []string{">=1.0.0, <2.0.0"}, "2.0.0", false},
		{"OR across", []string{"1.2.3", ">=2.0.0"}, "2.5.0", true},
		{"OR neither", []string{"1.2.3", ">=2.0.0"}, "1.5.0", false},

		// pre-release inclusion (npm semantics via Masterminds)
		{"pre vs >=1.0.0 rejected", []string{">=1.0.0"}, "1.0.0-alpha.14", false},
		{"pre vs >=1.0.0-0 accepted", []string{">=1.0.0-0"}, "1.0.0-alpha.14", true},
		{"pre vs >=1.0.0-alpha accepted", []string{">=1.0.0-alpha"}, "1.0.0-alpha.14", true},
		{"pre vs >=1.0.0-alpha.14 accepted", []string{">=1.0.0-alpha.14"}, "1.0.0-alpha.14", true},
		{"pre vs >=1.0.0-alpha.14 reject older", []string{">=1.0.0-alpha.14"}, "1.0.0-alpha.13", false},
		{"pre 2.0 vs <2.0.0-0", []string{"<2.0.0-0"}, "2.0.0-alpha.1", false},
		{"pre 2.0 vs <2.0.0", []string{"<2.0.0"}, "2.0.0-alpha.1", false}, // npm: no pre-release token in range → reject

		// leading v on input
		{"v prefix on version", []string{">=1.0.0-0"}, "v1.0.0-alpha.14", true},
		{"v prefix on range", []string{">=v1.0.0-0"}, "1.0.0-alpha.14", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			SupportedEngineRanges = c.ranges
			err := CheckCompatibility(c.version)
			got := err == nil
			if got != c.ok {
				t.Fatalf("ranges=%v version=%s: got ok=%v err=%v, want ok=%v",
					c.ranges, c.version, got, err, c.ok)
			}
		})
	}
}

func TestCheckCompatibility_MalformedInput(t *testing.T) {
	err := CheckCompatibility("not-a-version")
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
	if _, ok := errors.AsType[*CompatibilityError](err); ok {
		t.Errorf("malformed input should not produce CompatibilityError, got %v", err)
	}
}

func TestCheckCompatibility_EmptyInput(t *testing.T) {
	err := CheckCompatibility("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if _, ok := errors.AsType[*CompatibilityError](err); ok {
		t.Errorf("empty input should not produce CompatibilityError, got %v", err)
	}
}

func TestCheckCompatibility_MalformedRange(t *testing.T) {
	prev := SupportedEngineRanges
	t.Cleanup(func() { SupportedEngineRanges = prev })
	SupportedEngineRanges = []string{"!!!"}
	err := CheckCompatibility("1.0.0")
	if err == nil {
		t.Fatal("expected error for malformed range")
	}
	if _, ok := errors.AsType[*CompatibilityError](err); ok {
		t.Errorf("malformed range should not produce CompatibilityError, got %v", err)
	}
}

func TestCompatibilityError_Message(t *testing.T) {
	cErr := &CompatibilityError{
		CLIVersion:    "0.9.2",
		EngineVersion: "v0.5.0",
		Ranges:        []string{">=1.0.0-alpha, <2.0.0-0"},
	}
	msg := cErr.Error()
	for _, want := range []string{
		"template engine v0.5.0",
		"bino v0.9.2",
		">=1.0.0-alpha, <2.0.0-0",
		"engine-version",
	} {
		if !contains(msg, want) {
			t.Errorf("message missing %q\nfull: %s", want, msg)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
