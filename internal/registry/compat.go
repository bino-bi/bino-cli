package registry

import (
	"encoding/json"
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// Compat label keys, as published by the registry (see the server's
// domain.ParseLabels): semver ranges declaring what the package supports.
const (
	labelCompatCLI    = "registry.compat.cli"
	labelCompatEngine = "registry.compat.engine"
)

// CompatWarnings returns non-blocking messages when a downloaded document
// declares registry.compat.* ranges that exclude the running CLI or the
// project's engine. The body is the registry's canonical JSON. Unparsable
// ranges or versions are skipped silently — compat is warn-only and the
// server already validated range syntax at publish.
func CompatWarnings(pkg, version string, body []byte, cliVersion, engineVersion string) []string {
	var doc struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || len(doc.Metadata.Labels) == 0 {
		return nil
	}
	var warnings []string
	if w := compatWarning(pkg, version, doc.Metadata.Labels[labelCompatCLI], labelCompatCLI, "this CLI", cliVersion); w != "" {
		warnings = append(warnings, w)
	}
	if w := compatWarning(pkg, version, doc.Metadata.Labels[labelCompatEngine], labelCompatEngine, "the project's engine", engineVersion); w != "" {
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
