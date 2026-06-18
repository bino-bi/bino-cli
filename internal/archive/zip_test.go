package archive

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name    string
	content string
	mode    os.FileMode // 0 means a default regular-file mode
	symlink bool        // when true, content is the link target
}

func writeZip(t *testing.T, path string, entries []entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		switch {
		case e.symlink:
			hdr.SetMode(os.ModeSymlink | 0o777)
		case e.mode != 0:
			hdr.SetMode(e.mode)
		default:
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTrustedStripFixed(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{
		{name: "bn-template-engine/main.js", content: "hello"},
		{name: "bn-template-engine/sub/x.txt", content: "x"},
	})
	dest := filepath.Join(dir, "dest")
	if err := Extract(zipPath, dest, Options{Strip: StripFixed, FixedPrefix: "bn-template-engine/"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "main.js")); string(got) != "hello" {
		t.Errorf("main.js = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "x.txt")); err != nil {
		t.Errorf("sub/x.txt missing: %v", err)
	}
}

func TestExtractTrustedStripAuto(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{
		{name: "top/a.txt", content: "a"},
		{name: "top/b.txt", content: "b"},
	})
	dest := filepath.Join(dir, "dest")
	if err := Extract(zipPath, dest, Options{Strip: StripAuto}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Errorf("a.txt missing: %v", err)
	}
}

func TestExtractRejectsSiblingDirEscape(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{
		{name: "bn-template-engine/../dest-evil/evil.txt", content: "pwned"},
	})
	dest := filepath.Join(dir, "dest")
	err := Extract(zipPath, dest, Options{Strip: StripFixed, FixedPrefix: "bn-template-engine/"})
	if err == nil || !strings.Contains(err.Error(), "invalid file path in zip") {
		t.Fatalf("expected zip-slip rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dest-evil", "evil.txt")); !os.IsNotExist(statErr) {
		t.Errorf("escape file should not exist")
	}
}

func TestExtractUntrustedRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{{name: "link", content: "/etc/passwd", symlink: true}})
	err := Extract(zipPath, filepath.Join(dir, "dest"), Options{Untrusted: true, Limits: DefaultLimits()})
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("expected ErrSymlink, got %v", err)
	}
}

func TestExtractUntrustedRejectsPerFileBomb(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{{name: "big.txt", content: strings.Repeat("A", 1000)}})
	err := Extract(zipPath, filepath.Join(dir, "dest"), Options{Untrusted: true, Limits: Limits{MaxFileBytes: 100}})
	if !errors.Is(err, ErrFileTooBig) {
		t.Fatalf("expected ErrFileTooBig, got %v", err)
	}
}

func TestExtractUntrustedRejectsTotalBomb(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{
		{name: "a.txt", content: strings.Repeat("A", 90)},
		{name: "b.txt", content: strings.Repeat("B", 90)},
	})
	err := Extract(zipPath, filepath.Join(dir, "dest"), Options{Untrusted: true, Limits: Limits{MaxFileBytes: 100, MaxTotalBytes: 150}})
	if !errors.Is(err, ErrArchiveTooBig) {
		t.Fatalf("expected ErrArchiveTooBig, got %v", err)
	}
}

func TestExtractUntrustedRejectsEntryCountBomb(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{
		{name: "a.txt", content: "a"},
		{name: "b.txt", content: "b"},
		{name: "c.txt", content: "c"},
	})
	err := Extract(zipPath, filepath.Join(dir, "dest"), Options{Untrusted: true, Limits: Limits{MaxEntries: 2}})
	if !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("expected ErrTooManyFiles, got %v", err)
	}
}

func TestExtractUntrustedStripsModeBits(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{{name: "script.sh", content: "#!/bin/sh\n", mode: 0o777}})
	dest := filepath.Join(dir, "dest")
	if err := Extract(zipPath, dest, Options{Untrusted: true, Limits: DefaultLimits()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("exec bits not stripped: mode %o", info.Mode().Perm())
	}
}

func TestExtractTrustedIgnoresLimits(t *testing.T) {
	// Trusted mode must not enforce limits (engine/chrome behavior unchanged).
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, []entry{{name: "big.txt", content: strings.Repeat("A", 1000)}})
	dest := filepath.Join(dir, "dest")
	if err := Extract(zipPath, dest, Options{Strip: StripNone, Limits: Limits{MaxFileBytes: 1}}); err != nil {
		t.Fatalf("trusted mode should ignore limits, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "big.txt")); len(got) != 1000 {
		t.Errorf("expected 1000 bytes, got %d", len(got))
	}
}
