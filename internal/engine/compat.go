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

// AnnotationsMinVersion is the first engine release with bn-annotation-layer,
// bn-annotation-list and the annotations slot of bn-layout-page. It is no floor
// of SupportedEngineRanges: a project without annotations keeps working on
// older engines.
const AnnotationsMinVersion = "v1.0.0-next.30"

// CheckAnnotationSupport returns an error when a project with annotations uses
// an engine older than AnnotationsMinVersion. An empty or unreadable version
// is no finding: there is nothing to compare, and CheckCompatibility reports it.
func CheckAnnotationSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(AnnotationsMinVersion)) {
		return fmt.Errorf("annotations need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", AnnotationsMinVersion, engineVersion, AnnotationsMinVersion)
	}
	return nil
}

// GridCellLayoutMinVersion is the first engine release whose bn-grid knows
// cell-height, cell-width, cell-vertical-align, cell-horizontal-align and
// cell-captions. Older engines ignore these attributes without an error. It is
// no floor of SupportedEngineRanges: a grid without them keeps working.
const GridCellLayoutMinVersion = "v1.0.0-next.32"

// CheckGridCellLayoutSupport returns an error when a project that uses the grid
// cell layout uses an engine older than GridCellLayoutMinVersion. An empty or
// unreadable version is no finding, as in CheckAnnotationSupport.
func CheckGridCellLayoutSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(GridCellLayoutMinVersion)) {
		return fmt.Errorf("grid cell size, alignment and captions need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", GridCellLayoutMinVersion, engineVersion, GridCellLayoutMinVersion)
	}
	return nil
}

// SmallMultipleCardMinVersion is the first engine release whose bn-grid aligns
// on the component inside a bn-layout-card. Older engines draw the cards but
// leave their cells unaligned.
const SmallMultipleCardMinVersion = "v1.0.0-next.33"

// CheckSmallMultipleCardSupport is CheckGridCellLayoutSupport for a project
// with a SmallMultiple card.
func CheckSmallMultipleCardSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(SmallMultipleCardMinVersion)) {
		return fmt.Errorf("SmallMultiple card needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", SmallMultipleCardMinVersion, engineVersion, SmallMultipleCardMinVersion)
	}
	return nil
}

// OperationSentimentMinVersion is the first engine release that knows the
// variance sentiment suffixes _op and _opinv. Older engines draw them grey in
// charts and drop them in tables without an error. It is no floor of
// SupportedEngineRanges: a project without them keeps working.
const OperationSentimentMinVersion = "v1.0.0-next.34"

// CheckOperationSentimentSupport returns an error when a project that uses _op
// or _opinv uses an engine older than OperationSentimentMinVersion. An empty or
// unreadable version is no finding, as in CheckAnnotationSupport.
func CheckOperationSentimentSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(OperationSentimentMinVersion)) {
		return fmt.Errorf("the variance sentiments _op and _opinv need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", OperationSentimentMinVersion, engineVersion, OperationSentimentMinVersion)
	}
	return nil
}

// TableOrderMeasureMinVersion is the first engine release whose Table order
// takes a variance or an @<setname> interval block. Older engines sort by
// category instead, without an error. It is no floor of SupportedEngineRanges:
// a project without these order keys keeps working.
const TableOrderMeasureMinVersion = "v1.0.0-next.35"

// CheckTableOrderMeasureSupport returns an error when a project that sorts a
// Table by a variance or an interval block uses an engine older than
// TableOrderMeasureMinVersion. An empty or unreadable version is no finding.
func CheckTableOrderMeasureSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(TableOrderMeasureMinVersion)) {
		return fmt.Errorf("a Table order by a variance or an @ interval block needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", TableOrderMeasureMinVersion, engineVersion, TableOrderMeasureMinVersion)
	}
	return nil
}

// CategoryIntervalPositionMinVersion is the first engine release whose Table
// knows categoryIntervalPosition. Older engines ignore the attribute and keep
// the category column first, without an error. It is no floor of
// SupportedEngineRanges: a project without it keeps working.
const CategoryIntervalPositionMinVersion = "v1.0.0-next.39"

// CheckCategoryIntervalPositionSupport returns an error when a project that
// sets a Table categoryIntervalPosition uses an engine older than
// CategoryIntervalPositionMinVersion. An empty or unreadable version is no
// finding.
func CheckCategoryIntervalPositionSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if v.LessThan(semver.MustParse(CategoryIntervalPositionMinVersion)) {
		return fmt.Errorf("a Table categoryIntervalPosition needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", CategoryIntervalPositionMinVersion, engineVersion, CategoryIntervalPositionMinVersion)
	}
	return nil
}

// TableCalculationSchemeMinVersion is the first engine release whose Table
// knows runningSum, thereofPosition and varianceWaterfall. Older engines
// ignore the attributes and render the plain table, without an error. It is
// no floor of SupportedEngineRanges: a project without them keeps working.
const TableCalculationSchemeMinVersion = "v1.0.0-next.40"

// TableCalculationSchemeCheck returns the engine check for a project whose
// Tables turn on the given calculation scheme fields. The check returns an
// error on an engine older than TableCalculationSchemeMinVersion. An empty or
// unreadable version is no finding.
func TableCalculationSchemeCheck(props []string) func(string) error {
	return func(engineVersion string) error {
		v, err := semver.NewVersion(engineVersion)
		if err != nil {
			return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
		}
		if v.LessThan(semver.MustParse(TableCalculationSchemeMinVersion)) {
			return fmt.Errorf("a Table with %s needs template engine %s or newer, this project uses %s.\n"+
				"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(props, ", "), TableCalculationSchemeMinVersion, engineVersion, TableCalculationSchemeMinVersion)
		}
		return nil
	}
}

// GridCellLayoutCheck is the engine check for what the documents use: the
// card check when a SmallMultiple has a card, else the grid cell layout check.
func GridCellLayoutCheck(usesCard bool) func(string) error {
	if usesCard {
		return CheckSmallMultipleCardSupport
	}
	return CheckGridCellLayoutSupport
}

// ResolveVersion returns the engine version to check from a non-downloading
// source: the pin if set, otherwise the latest locally cached version. It is ""
// when nothing is resolvable, and callers should then skip the check.
func ResolveVersion(pinnedVersion string) string {
	if pinnedVersion != "" {
		return pinnedVersion
	}
	mgr, err := NewManager()
	if err != nil {
		return ""
	}
	info, err := mgr.LatestLocalVersion()
	if err != nil {
		return ""
	}
	return info.Version
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
