package hooks

import (
	"maps"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// ValidCheckpoints is the set of recognized hook checkpoint names.
var ValidCheckpoints = map[string]struct{}{
	"pre-build":      {},
	"pre-datasource": {},
	"pre-render":     {},
	"post-render":    {},
	"post-build":     {},
	"pre-preview":    {},
	"pre-refresh":    {},
	"pre-serve":      {},
	"pre-request":    {},
}

// Config holds the resolved hooks for a specific command invocation.
type Config struct {
	// Hooks maps checkpoint names to ordered command lists.
	Hooks pathutil.HooksConfig
}

// Resolve merges shared and per-command hooks. Per-command hooks override
// shared hooks for the same checkpoint name (they are not merged).
// Unknown checkpoint names are logged as warnings.
func Resolve(shared, perCommand pathutil.HooksConfig, logger logx.Logger) *Config {
	merged := make(pathutil.HooksConfig, len(shared)+len(perCommand))
	maps.Copy(merged, shared)
	// Per-command overrides shared for the same checkpoint
	maps.Copy(merged, perCommand)

	// Warn about unknown checkpoint names
	for name := range merged {
		if _, ok := ValidCheckpoints[name]; !ok {
			if logger != nil {
				logger.Warnf("unknown hook checkpoint %q in bino.toml (known: pre-build, pre-datasource, pre-render, post-render, post-build, pre-preview, pre-refresh, pre-serve, pre-request)", name)
			}
		}
	}

	return &Config{Hooks: merged}
}
