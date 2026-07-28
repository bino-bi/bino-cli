package engine

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	"bino.bi/bino/internal/version"
)

// SupportedEngineRanges declares the template engine versions this CLI release
// is compatible with. Each entry is an npm-style semver range; an engine
// version satisfies the contract if it matches ANY entry (logical OR).
//
// Range syntax (per Masterminds/semver/v3):
//   - exact:       "1.2.3", "=1.2.3"
//   - comparators: ">", ">=", "<", "<=", "!="
//   - hyphen:      "1.2.3 - 1.5.0"
//   - x-range:     "1.2.x", "1.x"
//   - tilde/caret: "~1.2.3", "^1.2.3"
//   - wildcard:    "*"
//   - AND within a range: comma- or space-separated
//
// Pre-release inclusion follows npm semantics: a pre-release version only
// satisfies a range when at least one comparator in that range explicitly
// mentions a pre-release token. The default range below mentions a pre-release
// so every matching 1.x pre-release / release matches, while 2.0.0+ is excluded.
//
// The alpha.19 floor is required by i18n: this CLI stores Internationalization
// content in the engine's "_system" namespace by default, which is only safe
// since the engine merges bundles instead of replacing them (older engines
// would wipe the built-in labels on a partial override).
var SupportedEngineRanges = []string{
	">=1.0.0-alpha.19, <2.0.0-0",
}

// CompatibilityError indicates that the resolved template engine version is
// not within any supported range. Callers can use errors.As to extract the
// fields and format the error per channel (CLI stderr, LSP diagnostic, lint
// finding).
type CompatibilityError struct {
	CLIVersion    string
	EngineVersion string
	Ranges        []string
}

func (e *CompatibilityError) Error() string {
	cli := e.CLIVersion
	if !strings.HasPrefix(cli, "v") {
		cli = "v" + cli
	}
	eng := e.EngineVersion
	if !strings.HasPrefix(eng, "v") {
		eng = "v" + eng
	}
	return fmt.Sprintf(
		"template engine %s is not supported by this CLI (bino %s).\n"+
			"Supported ranges: %s\n"+
			"Pin a compatible engine in bino.toml: engine-version = \"v1.x.y\"\n"+
			"Or upgrade the CLI: https://bino.bi/docs/install",
		eng, cli, strings.Join(e.Ranges, ", "),
	)
}

// CheckCompatibility returns nil if engineVersion satisfies any range in
// SupportedEngineRanges, a *CompatibilityError if it doesn't, or a parse
// error if engineVersion or any configured range is malformed.
//
// A leading "v" is accepted on engineVersion. Empty input returns a parse
// error so callers can distinguish "no engine resolved" from "unsupported".
func CheckCompatibility(engineVersion string) error {
	if engineVersion == "" {
		return fmt.Errorf("engine version is empty")
	}
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return fmt.Errorf("parse engine version %q: %w", engineVersion, err)
	}
	for _, raw := range SupportedEngineRanges {
		c, err := semver.NewConstraint(raw)
		if err != nil {
			return fmt.Errorf("parse supported range %q: %w", raw, err)
		}
		if c.Check(v) {
			return nil
		}
	}
	return &CompatibilityError{
		CLIVersion:    version.Version,
		EngineVersion: engineVersion,
		Ranges:        append([]string(nil), SupportedEngineRanges...),
	}
}
