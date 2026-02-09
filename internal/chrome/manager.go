// Package chrome manages downloading, caching, and resolving chrome-headless-shell
// binaries for PDF rendering and screenshot capture via the Chrome DevTools Protocol.
package chrome

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"bino.bi/bino/internal/pathutil"
)

const (
	// CacheSubdir is the subdirectory under ~/.bn/ where chrome-headless-shell versions are cached.
	CacheSubdir = "chrome-headless-shell"

	// lastKnownGoodURL is the Chrome for Testing endpoint that returns the latest stable version.
	lastKnownGoodURL = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"

	// downloadTimeout is the maximum time allowed for downloading a release.
	downloadTimeout = 10 * time.Minute
)

// VersionInfo describes a cached chrome-headless-shell version.
type VersionInfo struct {
	// Version is the Chrome version string (e.g., "133.0.6943.53").
	Version string
	// Path is the absolute path to the version directory.
	Path string
	// ExecPath is the absolute path to the chrome-headless-shell binary.
	ExecPath string
}

// InstallOptions configures the chrome-headless-shell installation.
type InstallOptions struct {
	DryRun bool
	Quiet  bool
	Stdout io.Writer
	Stderr io.Writer
}

// Manager handles chrome-headless-shell version management.
type Manager struct {
	cacheDir   string
	httpClient *http.Client
}

// NewManager creates a new Manager with the default cache location (~/.bn/chrome-headless-shell/).
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

// platformKey returns the Chrome for Testing platform identifier for the current OS/arch.
func platformKey() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "mac-arm64", nil
	case "darwin/amd64":
		return "mac-x64", nil
	case "linux/amd64":
		return "linux64", nil
	case "linux/arm64":
		return "", fmt.Errorf("chrome-headless-shell has no official linux/arm64 build — set CHROME_PATH to a compatible binary")
	case "windows/amd64":
		return "win64", nil
	case "windows/386":
		return "win32", nil
	default:
		return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// lastKnownGoodResponse is the JSON structure from the Chrome for Testing API.
type lastKnownGoodResponse struct {
	Channels struct {
		Stable struct {
			Version   string `json:"version"`
			Downloads struct {
				ChromeHeadlessShell []downloadEntry `json:"chrome-headless-shell"`
			} `json:"downloads"`
		} `json:"Stable"`
	} `json:"channels"`
}

type downloadEntry struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

// FetchLatestStableVersion queries the Chrome for Testing API and returns the latest
// stable version string.
func (m *Manager) FetchLatestStableVersion(ctx context.Context) (string, error) {
	resp, err := m.fetchLastKnownGood(ctx)
	if err != nil {
		return "", err
	}
	return resp.Channels.Stable.Version, nil
}

// FetchDownloadURL returns the version and download URL for chrome-headless-shell
// on the current platform.
func (m *Manager) FetchDownloadURL(ctx context.Context) (version, downloadURL string, err error) {
	platform, err := platformKey()
	if err != nil {
		return "", "", err
	}

	resp, err := m.fetchLastKnownGood(ctx)
	if err != nil {
		return "", "", err
	}

	for _, entry := range resp.Channels.Stable.Downloads.ChromeHeadlessShell {
		if entry.Platform == platform {
			return resp.Channels.Stable.Version, entry.URL, nil
		}
	}

	return "", "", fmt.Errorf("no chrome-headless-shell download found for platform %s", platform)
}

func (m *Manager) fetchLastKnownGood(ctx context.Context) (*lastKnownGoodResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lastKnownGoodURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch chrome versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch chrome versions: HTTP %d", resp.StatusCode)
	}

	var result lastKnownGoodResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse chrome versions: %w", err)
	}

	if result.Channels.Stable.Version == "" {
		return nil, fmt.Errorf("no stable version found in Chrome for Testing response")
	}

	return &result, nil
}

// Download downloads and extracts chrome-headless-shell for the given version and URL.
func (m *Manager) Download(ctx context.Context, version, downloadURL string) (VersionInfo, error) {
	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "chrome-headless-shell-*.zip")
	if err != nil {
		return VersionInfo{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download the zip file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("create download request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("download chrome-headless-shell %s: %w", version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("download chrome-headless-shell %s: HTTP %d", version, resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return VersionInfo{}, fmt.Errorf("write download: %w", err)
	}
	tmpFile.Close()

	// Extract the zip file
	versionPath := filepath.Join(m.cacheDir, version)
	if err := extractZipStrippingTopDir(tmpPath, versionPath); err != nil {
		os.RemoveAll(versionPath)
		return VersionInfo{}, fmt.Errorf("extract zip: %w", err)
	}

	// Find and chmod the binary
	execPath := resolveExecInDir(versionPath)
	if execPath == "" {
		os.RemoveAll(versionPath)
		return VersionInfo{}, fmt.Errorf("chrome-headless-shell binary not found in extracted archive")
	}

	if err := os.Chmod(execPath, 0o755); err != nil {
		return VersionInfo{}, fmt.Errorf("chmod binary: %w", err)
	}

	// On macOS, remove quarantine attribute
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", execPath).Run()
	}

	return VersionInfo{
		Version:  version,
		Path:     versionPath,
		ExecPath: execPath,
	}, nil
}

// Install is the top-level installation method: fetches the latest version and downloads it.
func (m *Manager) Install(ctx context.Context, opts InstallOptions) (VersionInfo, error) {
	if err := ctx.Err(); err != nil {
		return VersionInfo{}, err
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	if !opts.Quiet {
		fmt.Fprintln(stdout, "      Resolving latest stable Chrome version...")
	}

	version, downloadURL, err := m.FetchDownloadURL(ctx)
	if err != nil {
		return VersionInfo{}, fmt.Errorf("fetch download URL: %w", err)
	}

	if !opts.Quiet {
		fmt.Fprintf(stdout, "      Version: %s\n", version)
	}

	// Check if already cached
	existing := resolveExecInDir(filepath.Join(m.cacheDir, version))
	if existing != "" {
		if !opts.Quiet {
			fmt.Fprintf(stdout, "      Already installed at: %s\n", filepath.Join(m.cacheDir, version))
		}
		return VersionInfo{
			Version:  version,
			Path:     filepath.Join(m.cacheDir, version),
			ExecPath: existing,
		}, nil
	}

	if opts.DryRun {
		fmt.Fprintf(stdout, "      [dry-run] Would download chrome-headless-shell %s\n", version)
		return VersionInfo{Version: version}, nil
	}

	if !opts.Quiet {
		fmt.Fprintf(stdout, "      Downloading chrome-headless-shell %s...\n", version)
	}

	info, err := m.Download(ctx, version, downloadURL)
	if err != nil {
		return VersionInfo{}, err
	}

	if !opts.Quiet {
		fmt.Fprintf(stdout, "      Installed to: %s\n", info.Path)
	}

	return info, nil
}

// ListLocalVersions returns all locally cached versions, sorted newest first (lexicographic).
func (m *Manager) ListLocalVersions() ([]VersionInfo, error) {
	entries, err := os.ReadDir(m.cacheDir)
	if os.IsNotExist(err) {
		return nil, nil
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
		versionPath := filepath.Join(m.cacheDir, name)
		execPath := resolveExecInDir(versionPath)
		if execPath == "" {
			continue
		}
		versions = append(versions, VersionInfo{
			Version:  name,
			Path:     versionPath,
			ExecPath: execPath,
		})
	}

	// Sort by version string descending (lexicographic — Chrome versions sort correctly this way)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

// LatestLocalVersion returns the newest locally cached version.
func (m *Manager) LatestLocalVersion() (VersionInfo, error) {
	versions, err := m.ListLocalVersions()
	if err != nil {
		return VersionInfo{}, err
	}
	if len(versions) == 0 {
		return VersionInfo{}, fmt.Errorf("no chrome-headless-shell versions installed — run 'bino setup' to download")
	}
	return versions[0], nil
}

// ResolveExecPath returns the path to the chrome-headless-shell binary using the
// following priority:
//  1. CHROME_PATH environment variable
//  2. Latest locally cached version from ~/.bn/chrome-headless-shell/
//
// Returns an error if no binary can be found.
func (m *Manager) ResolveExecPath() (string, error) {
	// Priority 1: CHROME_PATH environment variable
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("CHROME_PATH %q: %w", envPath, err)
		}
		return envPath, nil
	}

	// Priority 2: latest local cached version
	info, err := m.LatestLocalVersion()
	if err != nil {
		return "", err
	}
	return info.ExecPath, nil
}

// resolveExecInDir returns the path to the chrome-headless-shell binary within a version directory,
// or empty string if not found.
func resolveExecInDir(dir string) string {
	name := "chrome-headless-shell"
	if runtime.GOOS == "windows" {
		name = "chrome-headless-shell.exe"
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// extractZipStrippingTopDir extracts a zip file to destDir, stripping the top-level directory.
// Chrome for Testing zips contain a single top-level directory (e.g., "chrome-headless-shell-mac-arm64/").
func extractZipStrippingTopDir(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// Detect common prefix (top-level directory)
	var prefix string
	for _, f := range r.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		candidate := parts[0] + "/"
		if prefix == "" {
			prefix = candidate
		} else if prefix != candidate {
			prefix = "" // no common prefix
			break
		}
	}

	for _, f := range r.File {
		name := f.Name
		if prefix != "" {
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

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", name, err)
		}

		if err := extractZipFile(f, targetPath); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}

	return nil
}

func extractZipFile(f *zip.File, destPath string) error {
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
