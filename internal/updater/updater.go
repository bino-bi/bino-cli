package updater

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"bino.bi/bino/internal/version"
	"github.com/blang/semver"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
)

const (
	repoOwner = "bino-bi"
	repoName  = "bino-cli-releases"
)

// CheckResult holds the result of an update check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateURL      string
	ReleaseNotes   string
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

	slug := fmt.Sprintf("%s/%s", repoOwner, repoName)
	latest, found, err := selfupdate.DetectLatest(slug)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
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

	if latest.Version.GT(currentVersion) {
		return &CheckResult{
			CurrentVersion: version.Version,
			LatestVersion:  latest.Version.String(),
			UpdateURL:      latest.URL,
			ReleaseNotes:   latest.ReleaseNotes,
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

	// Match asset name: bino-cli_{OS}_{ARCH}.tar.gz
	assetName := fmt.Sprintf("bino-cli_%s_%s", osName, archName)

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Filters: []string{
			assetName,
		},
	})
	if err != nil {
		return nil, err
	}

	slug := fmt.Sprintf("%s/%s", repoOwner, repoName)
	latest, err := updater.UpdateSelf(currentVersion, slug)
	if err != nil {
		return nil, err
	}

	if latest.Version.EQ(currentVersion) {
		return nil, nil // Already up to date
	}

	return &UpdateResult{
		PreviousVersion: version.Version,
		NewVersion:      latest.Version.String(),
		ReleaseNotes:    latest.ReleaseNotes,
	}, nil
}
