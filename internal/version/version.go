package version

import "fmt"

// Version metadata is injected at build time via -ldflags when available.
var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// BuildSummary returns a human-readable representation of the current build.
func BuildSummary() string {
	return fmt.Sprintf("bino %s (commit %s, built %s)", Version, Commit, Date)
}
