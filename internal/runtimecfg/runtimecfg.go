package runtimecfg

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds runtime configuration limits loaded from environment variables.
type Config struct {
	MaxManifestFiles int
	MaxManifestDocs  int
	MaxManifestBytes int64
	MaxQueryRows     int
	MaxQueryDuration time.Duration
	MaxCDNBytes      int64
	CDNTimeout       time.Duration
}

// configWarnings stores warnings from the last config load.
var (
	configWarnings   []string
	configWarningsMu sync.RWMutex
)

const (
	envMaxManifestFiles = "BNR_MAX_MANIFEST_FILES"
	envMaxManifestDocs  = "BNR_MAX_MANIFEST_DOCS"
	envMaxManifestBytes = "BNR_MAX_MANIFEST_BYTES"
	envMaxQueryRows     = "BNR_MAX_QUERY_ROWS"
	envMaxQueryDuration = "BNR_MAX_QUERY_DURATION_MS"
	envMaxCDNBytes      = "BNR_CDN_MAX_BYTES"
	envCDNTimeout       = "BNR_CDN_TIMEOUT_MS"

	defaultMaxManifestFiles = 500
	defaultMaxManifestDocs  = 0                 // 0 means unlimited
	defaultMaxManifestBytes = int64(10_000_000) // 10 MB
	defaultMaxQueryRows     = 100_000
	defaultMaxQueryDuration = 60 * time.Second
	defaultMaxCDNBytes      = int64(50_000_000) // 50 MB
	defaultCDNTimeout       = 10 * time.Second
)

var active atomic.Value

func init() {
	active.Store(load())
}

func Current() Config {
	if cfg, ok := active.Load().(Config); ok {
		return cfg
	}
	cfg := load()
	active.Store(cfg)
	return cfg
}

func Reload() Config {
	cfg := load()
	active.Store(cfg)
	return cfg
}

func SetForTests(cfg Config) func() {
	prev := Current()
	active.Store(cfg)
	return func() {
		active.Store(prev)
	}
}

// Warnings returns any warnings generated during the last config load.
// These indicate environment variables with invalid values (using defaults)
// or values that were clamped to min/max bounds.
func Warnings() []string {
	configWarningsMu.RLock()
	defer configWarningsMu.RUnlock()
	if len(configWarnings) == 0 {
		return nil
	}
	result := make([]string, len(configWarnings))
	copy(result, configWarnings)
	return result
}

func load() Config {
	var warnings []string
	addWarning := func(msg string) {
		warnings = append(warnings, msg)
	}

	cfg := Config{
		MaxManifestFiles: readIntEnv(envMaxManifestFiles, defaultMaxManifestFiles, 1, 100_000, addWarning),
		MaxManifestDocs:  readIntEnv(envMaxManifestDocs, defaultMaxManifestDocs, 0, 10_000, addWarning),
		MaxManifestBytes: readInt64Env(envMaxManifestBytes, defaultMaxManifestBytes, 1, 1_000_000_000, addWarning),
		MaxQueryRows:     readIntEnv(envMaxQueryRows, defaultMaxQueryRows, 0, 10_000_000, addWarning),
		MaxQueryDuration: readDurationMsEnv(envMaxQueryDuration, defaultMaxQueryDuration, 0, 24*time.Hour, addWarning),
		MaxCDNBytes:      readInt64Env(envMaxCDNBytes, defaultMaxCDNBytes, 1, 1_000_000_000, addWarning),
		CDNTimeout:       readDurationMsEnv(envCDNTimeout, defaultCDNTimeout, time.Second, time.Hour, addWarning),
	}

	configWarningsMu.Lock()
	configWarnings = warnings
	configWarningsMu.Unlock()

	return cfg
}

func readIntEnv(name string, def, lo, hi int, warn func(string)) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		warn(fmt.Sprintf("%s: invalid integer %q, using default %d", name, raw, def))
		return def
	}
	if hi > 0 && value > hi {
		warn(fmt.Sprintf("%s: value %d exceeds maximum %d, clamped", name, value, hi))
		return hi
	}
	if value < lo {
		warn(fmt.Sprintf("%s: value %d below minimum %d, clamped", name, value, lo))
		return lo
	}
	return value
}

func readInt64Env(name string, def, lo, hi int64, warn func(string)) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		warn(fmt.Sprintf("%s: invalid integer %q, using default %d", name, raw, def))
		return def
	}
	if hi > 0 && value > hi {
		warn(fmt.Sprintf("%s: value %d exceeds maximum %d, clamped", name, value, hi))
		return hi
	}
	if value < lo {
		warn(fmt.Sprintf("%s: value %d below minimum %d, clamped", name, value, lo))
		return lo
	}
	return value
}

func readDurationMsEnv(name string, def, lo, hi time.Duration, warn func(string)) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		warn(fmt.Sprintf("%s: invalid integer %q, using default %dms", name, raw, def.Milliseconds()))
		return def
	}
	duration := time.Duration(value) * time.Millisecond
	if hi > 0 && duration > hi {
		warn(fmt.Sprintf("%s: value %dms exceeds maximum %dms, clamped", name, duration.Milliseconds(), hi.Milliseconds()))
		return hi
	}
	if duration < lo {
		warn(fmt.Sprintf("%s: value %dms below minimum %dms, clamped", name, duration.Milliseconds(), lo.Milliseconds()))
		return lo
	}
	return duration
}
