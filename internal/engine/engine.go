// Package engine manages template engine version downloads and caching.
package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"bino.bi/bino/internal/archive"
	"bino.bi/bino/internal/pathutil"
)

const (
	// GitHubReleasesURL is the base URL for downloading template engine releases.
	GitHubReleasesURL = "https://github.com/bino-bi/bn-template-engine-releases/releases/download"

	// GitHubLatestURL is the URL to resolve the latest release version.
	GitHubLatestURL = "https://github.com/bino-bi/bn-template-engine-releases/releases/latest"

	// CacheSubdir is the subdirectory under ~/.bino/ where engine versions are cached.
	CacheSubdir = "cdn/bn-template-engine"

	// EntryPoint is the main JavaScript file in the template engine bundle.
	EntryPoint = "bn-template-engine.esm.js"

	// ZipFileName is the name of the zip archive in GitHub releases.
	ZipFileName = "bn-template-engine.zip"

	// downloadTimeout is the maximum time allowed for downloading a release.
	downloadTimeout = 5 * time.Minute
)

// versionPattern matches semver versions with v prefix, optionally with pre-release suffix
// (e.g., v1.2.3, v1.0.0-alpha.2, v1.0.0-beta.1).
var versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// VersionInfo describes a cached template engine version.
type VersionInfo struct {
	// Version is the semver version string (e.g., "v1.2.3").
	Version string
	// Path is the absolute path to the version directory.
	Path string
	// EntryPath is the absolute path to the entry point file.
	EntryPath string
}

// Manager handles template engine version management.
type Manager struct {
	cacheDir   string
	httpClient *http.Client
}

// NewManager creates a new Manager with the default cache location (~/.bino/cdn/bn-template-engine/).
func NewManager() (*Manager, error) {
	cacheDir, err := pathutil.CacheDir(CacheSubdir)
	if err != nil {
		return nil, fmt.Errorf("resolve cache directory: %w", err)
	}
	return &Manager{
		cacheDir:   cacheDir,
		httpClient: &http.Client{Timeout: downloadTimeout},
	}, nil
}

// NewManagerWithClient creates a Manager with a custom HTTP client (useful for testing).
// A nil client falls back to the same default client NewManager builds.
func NewManagerWithClient(cacheDir string, client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	return &Manager{
		cacheDir:   cacheDir,
		httpClient: client,
	}
}

// CacheDir returns the cache directory path.
func (m *Manager) CacheDir() string {
	return m.cacheDir
}

// ListLocalVersions returns all locally cached versions, sorted newest first.
func (m *Manager) ListLocalVersions() ([]VersionInfo, error) {
	entries, err := os.ReadDir(m.cacheDir)
	if os.IsNotExist(err) {
		return nil, nil // No cache directory yet
	}
	if err != nil {
		return nil, fmt.Errorf("read cache directory: %w", err)
	}

	var versions []VersionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !versionPattern.MatchString(name) {
			continue
		}

		versionPath := filepath.Join(m.cacheDir, name)
		entryPath := filepath.Join(versionPath, EntryPoint)

		// Verify entry point exists
		if _, err := os.Stat(entryPath); os.IsNotExist(err) {
			continue // Incomplete or corrupt installation
		}

		versions = append(versions, VersionInfo{
			Version:   name,
			Path:      versionPath,
			EntryPath: entryPath,
		})
	}

	// Sort by semver, newest first
	sort.Slice(versions, func(i, j int) bool {
		return semver.Compare(versions[i].Version, versions[j].Version) > 0
	})

	return versions, nil
}

// LatestLocalVersion returns the newest locally cached version.
// Returns an error if no versions are installed.
func (m *Manager) LatestLocalVersion() (VersionInfo, error) {
	versions, err := m.ListLocalVersions()
	if err != nil {
		return VersionInfo{}, err
	}
	if len(versions) == 0 {
		return VersionInfo{}, fmt.Errorf("no template engine versions installed - run 'bino setup --template-engine' to download")
	}
	return versions[0], nil
}

// ResolveVersion resolves a version string to a local VersionInfo.
// If version is empty, returns the latest local version.
// Returns an error if the version is not found locally.
func (m *Manager) ResolveVersion(version string) (VersionInfo, error) {
	if version == "" {
		return m.LatestLocalVersion()
	}

	// Normalize version to have v prefix
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	if !versionPattern.MatchString(version) {
		return VersionInfo{}, fmt.Errorf("invalid version format %q - expected semver (e.g., v1.2.3 or v1.0.0-alpha.2)", version)
	}

	versionPath := filepath.Join(m.cacheDir, version)
	entryPath := filepath.Join(versionPath, EntryPoint)

	if _, err := os.Stat(entryPath); os.IsNotExist(err) {
		return VersionInfo{}, fmt.Errorf("template engine version %s not found locally", version)
	}

	return VersionInfo{
		Version:   version,
		Path:      versionPath,
		EntryPath: entryPath,
	}, nil
}

// EnsureVersion ensures a version is available locally, downloading if needed.
// If version is empty, uses the latest local version (does not download).
func (m *Manager) EnsureVersion(ctx context.Context, version string) (VersionInfo, error) {
	if version == "" {
		return m.LatestLocalVersion()
	}

	// Normalize version
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	// Check if already cached
	info, err := m.ResolveVersion(version)
	if err == nil {
		return info, nil
	}

	// Not cached - download it
	return m.Download(ctx, version)
}

// Download downloads a specific version from GitHub releases.
func (m *Manager) Download(ctx context.Context, version string) (VersionInfo, error) {
	// Normalize version
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	if !versionPattern.MatchString(version) {
		return VersionInfo{}, fmt.Errorf("invalid version format %q - expected semver (e.g., v1.2.3 or v1.0.0-alpha.2)", version)
	}

	zipFileName := fmt.Sprintf("bn-template-engine-%s.zip", version)
	downloadURL := fmt.Sprintf("%s/%s/%s", GitHubReleasesURL, version, zipFileName)

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "bn-template-engine-*.zip")
	if err != nil {
		return VersionInfo{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download the zip file. The shared client follows redirects, which
	// GitHub release downloads rely on.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
	if err != nil {
		_ = tmpFile.Close() //nolint:errcheck // best-effort close on the error path; the primary error is returned
		return VersionInfo{}, fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		_ = tmpFile.Close() //nolint:errcheck // best-effort close on the error path; the primary error is returned
		return VersionInfo{}, fmt.Errorf("download template engine %s: %w", version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		_ = tmpFile.Close() //nolint:errcheck // best-effort close on the error path; the primary error is returned
		return VersionInfo{}, fmt.Errorf("template engine version %s not found on GitHub", version)
	}
	if resp.StatusCode != http.StatusOK {
		_ = tmpFile.Close() //nolint:errcheck // best-effort close on the error path; the primary error is returned
		return VersionInfo{}, fmt.Errorf("download template engine %s: HTTP %d", version, resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close() //nolint:errcheck // best-effort close on the error path; the primary error is returned
		return VersionInfo{}, fmt.Errorf("write download: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return VersionInfo{}, fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}

	// Extract the zip file
	versionPath := filepath.Join(m.cacheDir, version)
	if err := m.extractZip(tmpPath, versionPath); err != nil {
		os.RemoveAll(versionPath) // Clean up partial extraction
		return VersionInfo{}, fmt.Errorf("extract zip: %w", err)
	}

	entryPath := filepath.Join(versionPath, EntryPoint)
	if _, err := os.Stat(entryPath); os.IsNotExist(err) {
		os.RemoveAll(versionPath)
		return VersionInfo{}, fmt.Errorf("extracted archive missing entry point %s", EntryPoint)
	}

	return VersionInfo{
		Version:   version,
		Path:      versionPath,
		EntryPath: entryPath,
	}, nil
}

// extractZip extracts a zip file to the destination directory.
// The zip is expected to contain a bn-template-engine/ folder; contents are extracted
// directly to destDir (stripping the bn-template-engine/ prefix).
func (m *Manager) extractZip(zipPath, destDir string) error {
	// Engine releases are first-party signed archives wrapping a fixed
	// bn-template-engine/ directory, so trusted mode + a fixed prefix strip.
	return archive.Extract(zipPath, destDir, archive.Options{
		Strip:       archive.StripFixed,
		FixedPrefix: "bn-template-engine/",
	})
}

// FetchLatestRemoteVersion queries GitHub for the latest release tag.
func (m *Manager) FetchLatestRemoteVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, GitHubLatestURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// GitHub answers with a redirect to the release tag; the version is read
	// from the Location header, so use a per-request copy of the client that
	// does not follow the redirect.
	client := *m.httpClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("unexpected response from GitHub: HTTP %d", resp.StatusCode)
	}

	// Extract version from redirect URL (e.g., .../releases/tag/v1.2.3)
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no redirect location in GitHub response")
	}

	// Parse version from URL like "https://github.com/.../releases/tag/v1.2.3"
	parts := strings.Split(location, "/tag/")
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected redirect URL format: %s", location)
	}

	version := parts[1]
	if !versionPattern.MatchString(version) {
		return "", fmt.Errorf("invalid version in redirect URL: %s", version)
	}

	return version, nil
}
