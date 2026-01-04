// Package engine manages template engine version downloads and caching.
package engine

import (
	"archive/zip"
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

	"bino.bi/bino/internal/pathutil"
)

const (
	// GitHubReleasesURL is the base URL for downloading template engine releases.
	GitHubReleasesURL = "https://github.com/bino-bi/bn-template-engine-releases/releases/download"

	// GitHubLatestURL is the URL to resolve the latest release version.
	GitHubLatestURL = "https://github.com/bino-bi/bn-template-engine-releases/releases/latest"

	// CacheSubdir is the subdirectory under ~/.bn/ where engine versions are cached.
	CacheSubdir = "cdn/bn-template-engine"

	// EntryPoint is the main JavaScript file in the template engine bundle.
	EntryPoint = "bn-template-engine.esm.js"

	// ZipFileName is the name of the zip archive in GitHub releases.
	ZipFileName = "bn-template-engine.zip"

	// downloadTimeout is the maximum time allowed for downloading a release.
	downloadTimeout = 5 * time.Minute
)

// versionPattern matches semver versions with v prefix (e.g., v1.2.3).
var versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

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

// NewManager creates a new Manager with the default cache location (~/.bn/cdn/bn-template-engine/).
func NewManager() (*Manager, error) {
	cacheDir, err := pathutil.CacheDir(CacheSubdir)
	if err != nil {
		return nil, fmt.Errorf("resolve cache directory: %w", err)
	}
	return &Manager{
		cacheDir: cacheDir,
		httpClient: &http.Client{
			Timeout: downloadTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically for version resolution
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// NewManagerWithClient creates a Manager with a custom HTTP client (useful for testing).
func NewManagerWithClient(cacheDir string, client *http.Client) *Manager {
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
		return VersionInfo{}, fmt.Errorf("invalid version format %q - expected v#.#.# (e.g., v1.2.3)", version)
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
		return VersionInfo{}, fmt.Errorf("invalid version format %q - expected v#.#.# (e.g., v1.2.3)", version)
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

	// Download the zip file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("create request: %w", err)
	}

	// Use a client that follows redirects for downloads
	downloadClient := &http.Client{Timeout: downloadTimeout}
	resp, err := downloadClient.Do(req)
	if err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("download template engine %s: %w", version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("template engine version %s not found on GitHub", version)
	}
	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("download template engine %s: HTTP %d", version, resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("write download: %w", err)
	}
	tmpFile.Close()

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
// directly to destDir (stripping the top-level folder).
func (m *Manager) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// Find common prefix to strip (e.g., "bn-template-engine/")
	var prefix string
	for _, f := range r.File {
		name := f.Name
		if idx := strings.Index(name, "/"); idx > 0 {
			candidate := name[:idx+1]
			if prefix == "" {
				prefix = candidate
			} else if prefix != candidate {
				// Multiple top-level directories, don't strip
				prefix = ""
				break
			}
		}
	}

	for _, f := range r.File {
		name := f.Name

		// Strip common prefix
		if prefix != "" && strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
		}

		if name == "" {
			continue
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(name))

		// Prevent path traversal
		if !strings.HasPrefix(targetPath, destDir) {
			return fmt.Errorf("invalid file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", name, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", name, err)
		}

		if err := extractFile(f, targetPath); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}

	return nil
}

func extractFile(f *zip.File, destPath string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// FetchLatestRemoteVersion queries GitHub for the latest release tag.
func (m *Manager) FetchLatestRemoteVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, GitHubLatestURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
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
