package registry

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// CompatWarnings returns non-blocking messages when a package declares semver
// ranges that exclude the running CLI or the project's engine. The ranges come
// from the registry's resolve response (and are recorded in bino.lock so
// 'bino registry install', which works from the lock alone, can still report
// them). Unparsable ranges or versions are skipped silently — compat is
// warn-only and the server already validated the syntax at publish.
func CompatWarnings(pkg, version, compatEngine, compatCLI, cliVersion, engineVersion string) []string {
	var warnings []string
	if w := compatWarning(pkg, version, compatCLI, "compat-cli", "this CLI", cliVersion); w != "" {
		warnings = append(warnings, w)
	}
	if w := compatWarning(pkg, version, compatEngine, "compat-engine", "the project's engine", engineVersion); w != "" {
		warnings = append(warnings, w)
	}
	return warnings
}

func compatWarning(pkg, version, constraintStr, label, subject, actual string) string {
	if constraintStr == "" || actual == "" {
		return ""
	}
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return ""
	}
	v, err := semver.NewVersion(actual)
	if err != nil {
		return ""
	}
	if constraint.Check(v) {
		return ""
	}
	return fmt.Sprintf("%s@%s declares %s %q which does not include %s (%s)", pkg, version, label, constraintStr, subject, actual)
}
