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
//
// The second range is the plain release line from v0.50.0 on, see
// ReleaseLineMinVersion.
var SupportedEngineRanges = []string{
	">=1.0.0-alpha.19, <2.0.0-0",
	">=0.50.0, <1.0.0-0",
}

// ReleaseLineMinVersion is the first engine of the plain release line. The
// engine left the 1.0.0 pre-releases at v1.0.0-next.88 and went on with
// v0.50.0, which holds all of next.88. Per semver every 0.x sorts below a 1.0.0
// pre-release, so CompareVersions orders engines by line first.
const ReleaseLineMinVersion = "v0.50.0"

// Release lines of engine versions, oldest first.
const (
	lineOld        = iota // 0.x below v0.50.0
	linePreRelease        // 1.0.0-alpha.N, 1.0.0-next.N
	lineRelease           // 0.x from v0.50.0
	lineMajor             // 1.0.0 and later
)

func releaseLine(v *semver.Version) int {
	switch {
	case v.Major() == 0 && v.LessThan(semver.MustParse(ReleaseLineMinVersion)):
		return lineOld
	case v.Major() == 0:
		return lineRelease
	case v.Major() == 1 && v.Minor() == 0 && v.Patch() == 0 && v.Prerelease() != "":
		return linePreRelease
	default:
		return lineMajor
	}
}

// compareVersions orders a and b by release line, then by semver.
func compareVersions(a, b *semver.Version) int {
	la, lb := releaseLine(a), releaseLine(b)
	if la != lb {
		if la < lb {
			return -1
		}
		return 1
	}
	return a.Compare(b)
}

// CompareVersions returns -1, 0 or 1 when engine version a is older than, the
// same as or newer than b, in release order. An unreadable version sorts
// below a readable one.
func CompareVersions(a, b string) int {
	va, errA := semver.NewVersion(a)
	vb, errB := semver.NewVersion(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	return compareVersions(va, vb)
}

// RangeAccepts reports whether a semver range accepts engine version v. An
// engine from ReleaseLineMinVersion on also counts as every 1.0.0 pre-release,
// whose features it holds, so ">=1.0.0-next.40" accepts v0.50.0.
func RangeAccepts(c *semver.Constraints, v *semver.Version) bool {
	if c.Check(v) {
		return true
	}
	return releaseLine(v) >= lineRelease && c.Check(semver.MustParse(lastPreRelease))
}

// lastPreRelease is the last engine of the 1.0.0 pre-release line.
const lastPreRelease = "v1.0.0-next.88"

// before reports whether v is older than the engine release floor.
func before(v *semver.Version, floor string) bool {
	return compareVersions(v, semver.MustParse(floor)) < 0
}

// pinFor returns the engine version to pin for the features of floor: a
// pre-release floor is held by ReleaseLineMinVersion.
func pinFor(floor string) string {
	if releaseLine(semver.MustParse(floor)) == linePreRelease {
		return ReleaseLineMinVersion
	}
	return floor
}

// AnnotationsMinVersion is the first engine release with bn-annotation-layer,
// bn-annotation-list and the annotations slot of bn-layout-page. It is no floor
// of SupportedEngineRanges: a project without annotations keeps working on
// older engines.
const AnnotationsMinVersion = "v1.0.0-next.30"

// AnnotationNamesMinVersion is the first engine release that writes the name
// of an annotation to data-bino-annotation on its mark and list entry, and
// whose bn-annotation-layer has the interactive prop. It is a feature floor,
// not an error: below it the preview works, and a click on a mark in annotate
// mode lands on the component under it. The VS Code extension keeps a copy
// for its notice (vscode-bino/src/annotations/annotationModel.ts).
const AnnotationNamesMinVersion = "v1.0.0-next.88"

// CheckAnnotationSupport returns an error when a project with annotations uses
// an engine older than AnnotationsMinVersion. An empty or unreadable version
// is no finding: there is nothing to compare, and CheckCompatibility reports it.
func CheckAnnotationSupport(engineVersion string) error {
	v, err := semver.NewVersion(engineVersion)
	if err != nil {
		return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
	}
	if before(v, AnnotationsMinVersion) {
		return fmt.Errorf("annotations need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", AnnotationsMinVersion, engineVersion, pinFor(AnnotationsMinVersion))
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
	if before(v, GridCellLayoutMinVersion) {
		return fmt.Errorf("grid cell size, alignment and captions need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", GridCellLayoutMinVersion, engineVersion, pinFor(GridCellLayoutMinVersion))
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
	if before(v, SmallMultipleCardMinVersion) {
		return fmt.Errorf("SmallMultiple card needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", SmallMultipleCardMinVersion, engineVersion, pinFor(SmallMultipleCardMinVersion))
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
	if before(v, OperationSentimentMinVersion) {
		return fmt.Errorf("the variance sentiments _op and _opinv need template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", OperationSentimentMinVersion, engineVersion, pinFor(OperationSentimentMinVersion))
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
	if before(v, TableOrderMeasureMinVersion) {
		return fmt.Errorf("a Table order by a variance or an @ interval block needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", TableOrderMeasureMinVersion, engineVersion, pinFor(TableOrderMeasureMinVersion))
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
	if before(v, CategoryIntervalPositionMinVersion) {
		return fmt.Errorf("a Table categoryIntervalPosition needs template engine %s or newer, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", CategoryIntervalPositionMinVersion, engineVersion, pinFor(CategoryIntervalPositionMinVersion))
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
		if before(v, TableCalculationSchemeMinVersion) {
			return fmt.Errorf("a Table with %s needs template engine %s or newer, this project uses %s.\n"+
				"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(props, ", "), TableCalculationSchemeMinVersion, engineVersion, pinFor(TableCalculationSchemeMinVersion))
		}
		return nil
	}
}

// SmallMultipleSpanMinVersion is the first engine release whose bn-grid knows
// row-span, column-span and cell-background on a cell. The same release adds
// the scenario switch, the variance only mode and the axis title position of
// the charts, and two style keys. Older engines
// ignore all of them and render without, with no error. It is no floor of
// SupportedEngineRanges: a project without them keeps working.
const SmallMultipleSpanMinVersion = "v1.0.0-next.46"

// SmallMultipleSpanCheck returns the engine check for a project that uses the
// given features of SmallMultipleSpanMinVersion. The check returns an error on
// an older engine. An empty or unreadable version is no finding.
func SmallMultipleSpanCheck(features []string) func(string) error {
	return func(engineVersion string) error {
		v, err := semver.NewVersion(engineVersion)
		if err != nil {
			return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
		}
		if before(v, SmallMultipleSpanMinVersion) {
			return fmt.Errorf("%s: template engine %s or newer is needed, this project uses %s.\n"+
				"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(features, ", "), SmallMultipleSpanMinVersion, engineVersion, pinFor(SmallMultipleSpanMinVersion))
		}
		return nil
	}
}

// StackSeriesMinVersion is the first engine release that stacks a time chart
// by series from the category column with the scenario notation, the legend
// side, the 100 % mark and the two-row axis labels, and that knows the
// difference mark and the frame text of an annotation. Older engines ignore
// all of them and render without, with no error. It is no floor of
// SupportedEngineRanges: a project without them keeps working.
const StackSeriesMinVersion = "v1.0.0-next.49"

// StackSeriesCheck returns the engine check for a project that uses the given
// features of StackSeriesMinVersion. The check returns an error on an older
// engine. An empty or unreadable version is no finding.
func StackSeriesCheck(features []string) func(string) error {
	return func(engineVersion string) error {
		v, err := semver.NewVersion(engineVersion)
		if err != nil {
			return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
		}
		if before(v, StackSeriesMinVersion) {
			return fmt.Errorf("%s: template engine %s or newer is needed, this project uses %s.\n"+
				"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(features, ", "), StackSeriesMinVersion, engineVersion, pinFor(StackSeriesMinVersion))
		}
		return nil
	}
}

// StructureSumsMinVersion is the first engine release with sum rows, the 100 %
// mark, value labels, rows by total and a reference line in a structure chart,
// with the overlay area of a card, the group separator of a document, and with
// the marks pointer and bracket and the difference of two bars of an
// annotation. Older engines ignore all of them and render without, with no
// error. It is no floor of
// SupportedEngineRanges: a project without them keeps working.
const StructureSumsMinVersion = "v1.0.0-next.51"

// StructureWaterfallMinVersion is the first engine release with the variance
// waterfall of a structure chart, its order by a variance, its labels of
// relative variances without the percent sign, and with scenarios that a
// structure chart or a time chart draws at its total only.
const StructureWaterfallMinVersion = "v1.0.0-next.69"

// StructureRowLinesMinVersion is the first engine release with the row lines
// and the mark of the order of a structure chart.
const StructureRowLinesMinVersion = "v1.0.0-next.71"

// StructureRowLinesListMinVersion is the first engine release that takes a list
// of rows for the row lines of a structure chart.
const StructureRowLinesListMinVersion = "v1.0.0-next.72"

// structureFeatureFloors holds the features of StructureSumsCheck that need a
// newer engine than StructureSumsMinVersion.
var structureFeatureFloors = map[string]string{
	"ChartStructure varianceWaterfall":     StructureWaterfallMinVersion,
	"ChartStructure dataFormatPercentSign": StructureWaterfallMinVersion,
	"ChartStructure order by a variance":   StructureWaterfallMinVersion,
	"ChartStructure sumScenarios":          StructureWaterfallMinVersion,
	"ChartStructure sumDataset":            StructureWaterfallMinVersion,
	"ChartStructure rowLines":              StructureRowLinesMinVersion,
	"ChartStructure orderMark":             StructureRowLinesMinVersion,
	"ChartStructure rowLines as a list":    StructureRowLinesListMinVersion,
}

// StructureSumsCheck returns the engine check for a project that uses the given
// features: the waterfall fields need StructureWaterfallMinVersion, the others
// StructureSumsMinVersion. The check returns an error on an older engine and
// names the features that this engine does not have. An empty or unreadable
// version is no finding.
func StructureSumsCheck(features []string) func(string) error {
	return func(engineVersion string) error {
		v, err := semver.NewVersion(engineVersion)
		if err != nil {
			return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
		}
		floor := ""
		var missing []string
		for _, feature := range features {
			need := StructureSumsMinVersion
			if own, ok := structureFeatureFloors[feature]; ok {
				need = own
			}
			if !before(v, need) {
				continue
			}
			missing = append(missing, feature)
			if floor == "" || CompareVersions(need, floor) > 0 {
				floor = need
			}
		}
		if len(missing) == 0 {
			return nil
		}
		return fmt.Errorf("%s: template engine %s or newer is needed, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(missing, ", "), floor, engineVersion, pinFor(floor))
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
			"Pin a compatible engine in bino.toml: engine-version = %q\n"+
			"Or upgrade the CLI: https://bino.bi/docs/install",
		eng, cli, strings.Join(e.Ranges, ", "), ReleaseLineMinVersion,
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

// TimeTotalsMinVersion is the first engine release with the total columns of
// a time chart in the form the CLI passes on. TimeIndicatorMinVersion is the
// first one with the scaling indicator of a time chart and with the labels of
// relative variances without the percent sign. Older engines ignore them and
// render without, with no error. Neither is a floor of SupportedEngineRanges:
// a project without them keeps working.
const (
	TimeTotalsMinVersion    = "v1.0.0-next.64"
	TimeIndicatorMinVersion = "v1.0.0-next.60"
)

// TimeWaterfallMinVersion is the first engine release with the variance
// waterfall of a time chart, its reference column, and totals at the scale of
// the chart (no unitScaling in totals).
const TimeWaterfallMinVersion = "v1.0.0-next.68"

// TimeAreaSeriesMinVersion is the first engine release that draws a stacked
// area by series, with period lines and the point position, and that knows
// the two-row axis labels in line mode.
const TimeAreaSeriesMinVersion = "v1.0.0-next.73"

// timeFeatureFloors holds the features of TimeTotalsCheck that do not need
// TimeIndicatorMinVersion, with the engine release they need.
var timeFeatureFloors = map[string]string{
	TimeTotalsFeature:                       TimeTotalsMinVersion,
	"ChartTime varianceWaterfall":           TimeWaterfallMinVersion,
	"ChartTime referenceColumn":             TimeWaterfallMinVersion,
	"ChartTime totals without unitScaling":  TimeWaterfallMinVersion,
	"ChartTime totals.scenarios":            StructureWaterfallMinVersion,
	"ChartTime stacked area by series":      TimeAreaSeriesMinVersion,
	"ChartTime two-row labels in line mode": TimeAreaSeriesMinVersion,
	"ChartTime periodLines":                 TimeAreaSeriesMinVersion,
	"ChartTime pointPosition":               TimeAreaSeriesMinVersion,
}

// TimeTotalsFeature is the name of the totals of a time chart in the features
// of TimeTotalsCheck. Only it needs TimeTotalsMinVersion.
const TimeTotalsFeature = "ChartTime totals"

// TimeTotalsCheck returns the engine check for a project that uses the given
// features: the totals need TimeTotalsMinVersion, the waterfall fields
// TimeWaterfallMinVersion, the others TimeIndicatorMinVersion. The check returns an error on an older engine and
// names the features that this engine does not have. An empty or unreadable
// version is no finding.
func TimeTotalsCheck(features []string) func(string) error {
	return func(engineVersion string) error {
		v, err := semver.NewVersion(engineVersion)
		if err != nil {
			return nil //nolint:nilerr // CheckCompatibility reports an unreadable version
		}
		floor := ""
		var missing []string
		for _, feature := range features {
			need := TimeIndicatorMinVersion
			if floor, ok := timeFeatureFloors[feature]; ok {
				need = floor
			}
			if !before(v, need) {
				continue
			}
			missing = append(missing, feature)
			if floor == "" || CompareVersions(need, floor) > 0 {
				floor = need
			}
		}
		if len(missing) == 0 {
			return nil
		}
		return fmt.Errorf("%s: template engine %s or newer is needed, this project uses %s.\n"+
			"Pin a newer engine in bino.toml: engine-version = %q", strings.Join(missing, ", "), floor, engineVersion, pinFor(floor))
	}
}
