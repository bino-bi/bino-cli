package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := &State{
		LastUpdateCheck: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
		SetupCompleted:  true,
		ChromeVersion:   "126.0.0",
	}
	if err := SaveState(want); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if !got.LastUpdateCheck.Equal(want.LastUpdateCheck) {
		t.Errorf("LastUpdateCheck = %v, want %v", got.LastUpdateCheck, want.LastUpdateCheck)
	}
	if got.SetupCompleted != want.SetupCompleted {
		t.Errorf("SetupCompleted = %v, want %v", got.SetupCompleted, want.SetupCompleted)
	}
	if got.ChromeVersion != want.ChromeVersion {
		t.Errorf("ChromeVersion = %q, want %q", got.ChromeVersion, want.ChromeVersion)
	}
}

func TestLoadState(t *testing.T) {
	t.Run("missing file returns empty state", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		got, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if got == nil {
			t.Fatal("LoadState() = nil, want empty state")
		}
		if !got.LastUpdateCheck.IsZero() {
			t.Errorf("LastUpdateCheck = %v, want zero", got.LastUpdateCheck)
		}
	})

	t.Run("falls back to legacy location", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		legacyPath, err := legacyStatePath()
		if err != nil {
			t.Fatalf("legacyStatePath() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
			t.Fatalf("mkdir legacy dir: %v", err)
		}
		if err := os.WriteFile(legacyPath, []byte(`{"chrome_version":"legacy"}`), 0o644); err != nil {
			t.Fatalf("write legacy state: %v", err)
		}

		got, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if got.ChromeVersion != "legacy" {
			t.Errorf("ChromeVersion = %q, want legacy", got.ChromeVersion)
		}
	})

	t.Run("corrupt state file errors", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		path, err := getStatePath()
		if err != nil {
			t.Fatalf("getStatePath() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write corrupt state: %v", err)
		}

		if _, err := LoadState(); err == nil {
			t.Error("LoadState() should error on corrupt state file")
		}
	})
}
