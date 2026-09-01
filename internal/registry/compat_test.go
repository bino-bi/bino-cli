package registry

import (
	"strings"
	"testing"
)

func TestCompatWarnings(t *testing.T) {
	tests := []struct {
		name                      string
		compatEngine, compatCLI   string
		cliVersion, engineVersion string
		wantCount                 int
		wantSubstr                string
	}{
		{name: "no ranges declared", cliVersion: "0.90.0", engineVersion: "1.0.0"},
		{
			name: "cli outside the range", compatCLI: ">=0.95.0",
			cliVersion: "0.90.0", engineVersion: "1.0.0",
			wantCount: 1, wantSubstr: "compat-cli",
		},
		{
			name: "engine outside the range", compatEngine: ">=2.0.0",
			cliVersion: "0.95.0", engineVersion: "1.0.0",
			wantCount: 1, wantSubstr: "the project's engine",
		},
		{
			name: "both outside", compatCLI: ">=0.95.0", compatEngine: ">=2.0.0",
			cliVersion: "0.90.0", engineVersion: "1.0.0", wantCount: 2,
		},
		{
			name: "both satisfied", compatCLI: ">=0.90.0", compatEngine: ">=1.0.0",
			cliVersion: "0.90.0", engineVersion: "1.0.0",
		},
		{
			name: "unparsable range is skipped", compatCLI: "not-a-range",
			cliVersion: "0.90.0", engineVersion: "1.0.0",
		},
		{
			name: "unknown local version is skipped", compatCLI: ">=0.95.0",
			cliVersion: "", engineVersion: "1.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompatWarnings("@acme/kit", "1.0.0", tt.compatEngine, tt.compatCLI, tt.cliVersion, tt.engineVersion)
			if len(got) != tt.wantCount {
				t.Fatalf("warnings = %v, want %d", got, tt.wantCount)
			}
			if tt.wantSubstr != "" && !strings.Contains(got[0], tt.wantSubstr) {
				t.Errorf("warning %q does not mention %q", got[0], tt.wantSubstr)
			}
		})
	}
}
