package runtimecfg

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Config struct {
	MaxManifestFiles int
	MaxManifestDocs  int
	MaxManifestBytes int64
	MaxQueryRows     int
	MaxQueryDuration time.Duration
	MaxCDNBytes      int64
	CDNTimeout       time.Duration
}

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

func load() Config {
	return Config{
		MaxManifestFiles: readIntEnv(envMaxManifestFiles, defaultMaxManifestFiles, 1, 100_000),
		MaxManifestDocs:  readIntEnv(envMaxManifestDocs, defaultMaxManifestDocs, 0, 10_000),
		MaxManifestBytes: readInt64Env(envMaxManifestBytes, defaultMaxManifestBytes, 1, 1_000_000_000),
		MaxQueryRows:     readIntEnv(envMaxQueryRows, defaultMaxQueryRows, 0, 10_000_000),
		MaxQueryDuration: readDurationMsEnv(envMaxQueryDuration, defaultMaxQueryDuration, 0, 24*time.Hour),
		MaxCDNBytes:      readInt64Env(envMaxCDNBytes, defaultMaxCDNBytes, 1, 1_000_000_000),
		CDNTimeout:       readDurationMsEnv(envCDNTimeout, defaultCDNTimeout, time.Second, time.Hour),
	}
}

func readIntEnv(name string, def, min, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if max > 0 && value > max {
		return max
	}
	if value < min {
		return min
	}
	return value
}

func readInt64Env(name string, def, min, max int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	if max > 0 && value > max {
		return max
	}
	if value < min {
		return min
	}
	return value
}

func readDurationMsEnv(name string, def, min, max time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	duration := time.Duration(value) * time.Millisecond
	if max > 0 && duration > max {
		return max
	}
	if duration < min {
		return min
	}
	return duration
}
