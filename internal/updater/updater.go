package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"bino.bi/bino/internal/version"
	"github.com/blang/semver"
)

const (
	repoOwner = "bino-bi"
	repoName  = "bino-cli-releases"
)

// baseURL is the base URL for releases.
var baseURL = fmt.Sprintf("https://github.com/%s/%s/releases", repoOwner, repoName)

// versionRegex extracts version from GitHub release URL.
var versionRegex = regexp.MustCompile(`/download/(v[0-9]+\.[0-9]+\.[0-9]+)/`)

// CheckResult holds the result of an update check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateURL      string
	ReleaseNotes   string
}

// getAssetName returns the asset name for the current OS and architecture.
func getAssetName() string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "Darwin"
	case "linux":
		osName = "Linux"
	case "windows":
		osName = "Windows"
	}

	archName := runtime.GOARCH
	switch archName {
	case "amd64":
		archName = "x86_64"
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}

	return fmt.Sprintf("bino-cli_%s_%s.%s", osName, archName, ext)
}

// getLatestDownloadURL returns the latest download URL for the current platform.
func getLatestDownloadURL() string {
	return fmt.Sprintf("%s/latest/download/%s", baseURL, getAssetName())
}

// resolveLatestVersion resolves the latest version by following the redirect
// from the "latest" URL and extracting the version from the final URL.
func resolveLatestVersion(ctx context.Context) (latestVersion string, downloadURL string, err error) {
	latestURL := getLatestDownloadURL()

	// Create a client that doesn't follow redirects automatically
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("checking latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", "", errors.New("no redirect location found")
	}

	// Extract version from the redirect URL
	matches := versionRegex.FindStringSubmatch(location)
	if len(matches) < 2 {
		return "", "", fmt.Errorf("could not extract version from URL: %s", location)
	}

	return matches[1], location, nil
}

// CheckForUpdate checks if a new version is available.
// It returns the check result if an update is available, or nil if up to date.
// It respects a 24-hour cache interval.
func CheckForUpdate(ctx context.Context) (*CheckResult, error) {
	// Load state to check frequency
	state, err := LoadState()
	if err == nil {
		if time.Since(state.LastUpdateCheck) < 24*time.Hour {
			return nil, nil
		}
	}

	latestVersionStr, downloadURL, err := resolveLatestVersion(ctx)
	if err != nil {
		return nil, err
	}

	// Update state
	if state == nil {
		state = &State{}
	}
	state.LastUpdateCheck = time.Now()
	_ = SaveState(state)

	vStr := strings.TrimPrefix(version.Version, "v")
	currentVersion, err := semver.Parse(vStr)
	if err != nil {
		// If current version is invalid (e.g. "dev"), we can't reliably compare.
		return nil, nil
	}

	latestVersion, err := semver.Parse(strings.TrimPrefix(latestVersionStr, "v"))
	if err != nil {
		return nil, fmt.Errorf("parsing latest version: %w", err)
	}

	if latestVersion.GT(currentVersion) {
		return &CheckResult{
			CurrentVersion: version.Version,
			LatestVersion:  latestVersionStr,
			UpdateURL:      downloadURL,
		}, nil
	}

	return nil, nil
}

// UpdateResult holds the result of an update operation.
type UpdateResult struct {
	PreviousVersion string
	NewVersion      string
	ReleaseNotes    string
}

// Update performs the self-update to the latest version.
func Update(ctx context.Context) (*UpdateResult, error) {
	vStr := strings.TrimPrefix(version.Version, "v")
	currentVersion, err := semver.Parse(vStr)
	if err != nil {
		// Fallback for dev builds to allow testing update flow
		currentVersion = semver.MustParse("0.0.0")
	}

	latestVersionStr, downloadURL, err := resolveLatestVersion(ctx)
	if err != nil {
		return nil, err
	}

	latestVersion, err := semver.Parse(strings.TrimPrefix(latestVersionStr, "v"))
	if err != nil {
		return nil, fmt.Errorf("parsing latest version: %w", err)
	}

	if latestVersion.LTE(currentVersion) {
		return nil, nil // Already up to date
	}

	// Download and apply the update with checksum verification
	if err := downloadAndApply(ctx, latestVersionStr, downloadURL); err != nil {
		return nil, fmt.Errorf("applying update: %w", err)
	}

	return &UpdateResult{
		PreviousVersion: version.Version,
		NewVersion:      latestVersionStr,
	}, nil
}

// downloadAndApplbny downloads the tarball from the given URL, verifies its
// SHA-256 checksum against checksums.txt, and replaces the current executable.
func downloadAndApply(ctx context.Context, versionTag, downloadURL string) error {
	assetName := getAssetName()

	// Fetch and parse checksums.txt for this release
	expectedChecksum, err := fetchExpectedChecksum(ctx, versionTag, assetName)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Download archive to temp file and compute checksum
	archiveFile, err := os.CreateTemp("", "bino-archive-*")
	if err != nil {
		return fmt.Errorf("creating temp archive file: %w", err)
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(archiveFile, hasher), resp.Body); err != nil {
		archiveFile.Close()
		return fmt.Errorf("downloading archive: %w", err)
	}
	archiveFile.Close()

	// Verify checksum
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// Create a temporary file for the new binary
	tmpFile, err := os.CreateTemp("", "bino-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Extract the binary from the archive
	if runtime.GOOS == "windows" {
		// Windows uses zip archives
		if err := extractBinaryFromZip(archivePath, tmpFile); err != nil {
			tmpFile.Close()
			return fmt.Errorf("extracting binary: %w", err)
		}
	} else {
		// Re-open archive for extraction (Unix uses tar.gz)
		archiveFile, err = os.Open(archivePath)
		if err != nil {
			return fmt.Errorf("reopening archive: %w", err)
		}
		defer archiveFile.Close()

		if err := extractBinaryFromTarGz(archiveFile, tmpFile); err != nil {
			tmpFile.Close()
			return fmt.Errorf("extracting binary: %w", err)
		}
	}
	tmpFile.Close()

	// Make the new binary executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Replace the old binary with the new one
	// First, rename the old binary to a backup
	backupPath := execPath + ".old"
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("backing up old binary: %w", err)
	}

	// Move the new binary to the executable path
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Try to restore the backup
		_ = os.Rename(backupPath, execPath)
		return fmt.Errorf("replacing binary: %w", err)
	}

	// Remove the backup
	_ = os.Remove(backupPath)

	return nil
}

// fetchExpectedChecksum downloads checksums.txt for the given release tag
// and returns the expected SHA-256 checksum for the specified asset.
func fetchExpectedChecksum(ctx context.Context, versionTag, assetName string) (string, error) {
	checksumsURL := fmt.Sprintf("%s/download/%s/checksums.txt", baseURL, versionTag)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching checksums.txt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt not found (status %d)", resp.StatusCode)
	}

	checksums, err := parseChecksums(resp.Body)
	if err != nil {
		return "", fmt.Errorf("parsing checksums.txt: %w", err)
	}

	checksum, ok := checksums[assetName]
	if !ok {
		return "", fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
	}

	return checksum, nil
}

// parseChecksums parses a sha256sum-style checksums file and returns
// a map of filename to hex-encoded checksum.
func parseChecksums(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: "<hex>  <filename>" or "<hex> <filename>"
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		checksum := parts[0]
		filename := parts[1]

		// Validate checksum is a valid hex string (64 chars for SHA-256)
		if len(checksum) != 64 {
			continue
		}

		checksums[filename] = checksum
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return checksums, nil
}

// extractBinaryFromTarGz extracts the "bino" binary from a tar.gz archive.
func extractBinaryFromTarGz(r io.Reader, w io.Writer) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Look for the bino binary (could be "bino" or "bino.exe" on Windows)
		if header.Typeflag == tar.TypeReg &&
			(header.Name == "bino" || header.Name == "bino.exe") {
			if _, err := io.Copy(w, tr); err != nil {
				return fmt.Errorf("extracting binary: %w", err)
			}
			return nil
		}
	}

	return errors.New("bino binary not found in archive")
}

// extractBinaryFromZip extracts the "bino.exe" binary from a zip archive.
func extractBinaryFromZip(archivePath string, w io.Writer) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("opening zip archive: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		// Look for the bino binary
		if f.Name == "bino" || f.Name == "bino.exe" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("opening file in zip: %w", err)
			}
			defer rc.Close()

			if _, err := io.Copy(w, rc); err != nil {
				return fmt.Errorf("extracting binary: %w", err)
			}
			return nil
		}
	}

	return errors.New("bino binary not found in archive")
}
