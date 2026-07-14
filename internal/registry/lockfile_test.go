package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleLock() *Lockfile {
	return &Lockfile{
		LockfileVersion: CurrentLockfileVersion,
		Packages: []Entry{
			{
				Name: "@bino/style_a", Version: "1.4.0", Digest: "sha256:bbbb", Kind: "ComponentStyle",
				Path: ".bino/registry/bino/style_a.yml", Direct: false, Dependencies: []string{},
			},
			{
				Name: "@acme/revenue-table", Version: "2.1.0", Tag: "latest", Digest: "sha256:aaaa", Kind: "Table",
				Path: ".bino/registry/acme/revenue-table.yml", Direct: true,
				Dependencies: []string{"@bino/style_a"},
			},
		},
	}
}

func TestLockfileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLockfile(dir, sampleLock()); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Packages) != 2 || got.LockfileVersion != CurrentLockfileVersion {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// Sorted by name on save.
	if got.Packages[0].Name != "@acme/revenue-table" || got.Packages[1].Name != "@bino/style_a" {
		t.Errorf("packages not sorted: %s, %s", got.Packages[0].Name, got.Packages[1].Name)
	}
	e := got.Packages[0]
	if e.Version != "2.1.0" || e.Tag != "latest" || e.IsPinned() {
		t.Errorf("entry mismatch: %+v", e)
	}
	if !got.Packages[1].IsPinned() {
		t.Error("entry without tag should be pinned")
	}
}

func TestLockfileDeterministicOutput(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	a := sampleLock()
	b := sampleLock()
	// Reverse insertion order in b.
	b.Packages[0], b.Packages[1] = b.Packages[1], b.Packages[0]
	if err := SaveLockfile(dirA, a); err != nil {
		t.Fatal(err)
	}
	if err := SaveLockfile(dirB, b); err != nil {
		t.Fatal(err)
	}
	bytesA, _ := os.ReadFile(filepath.Join(dirA, LockfileName))
	bytesB, _ := os.ReadFile(filepath.Join(dirB, LockfileName))
	if string(bytesA) != string(bytesB) {
		t.Errorf("output not byte-stable across insertion orders:\n%s\n---\n%s", bytesA, bytesB)
	}
}

func TestLockfilePinnedWritesNoTagKey(t *testing.T) {
	dir := t.TempDir()
	lf := &Lockfile{LockfileVersion: 1, Packages: []Entry{{
		Name: "@acme/x", Version: "1.0.0", Digest: "sha256:cc", Kind: "Text", Path: ".bino/registry/acme/x.yml", Direct: true,
	}}}
	if err := SaveLockfile(dir, lf); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LockfileName))
	if strings.Contains(string(data), "tag") {
		t.Errorf("pinned entry must not write a tag key:\n%s", data)
	}
}

func TestLockfileMissingFileIsEmpty(t *testing.T) {
	lf, err := LoadLockfile(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lf.LockfileVersion != CurrentLockfileVersion || len(lf.Packages) != 0 {
		t.Errorf("expected empty current-version lockfile, got %+v", lf)
	}
}

func TestLockfileMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LockfileName), []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLockfile(dir); !errors.Is(err, ErrMalformedLockfile) {
		t.Errorf("expected ErrMalformedLockfile, got %v", err)
	}
}

func TestLockfileFutureVersionRoundTrips(t *testing.T) {
	dir := t.TempDir()
	content := "lockfile_version = 99\n\n[[package]]\nname = \"@acme/x\"\nversion = \"1.0.0\"\ndigest = \"sha256:cc\"\nkind = \"Text\"\npath = \".bino/registry/acme/x.yml\"\ndirect = true\ndependencies = []\nfuture_field = \"ignored\"\n"
	if err := os.WriteFile(filepath.Join(dir, LockfileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lf, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lf.LockfileVersion != 99 {
		t.Errorf("LockfileVersion = %d, want 99 round-tripped", lf.LockfileVersion)
	}
	if err := SaveLockfile(dir, lf); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LockfileName))
	if !strings.Contains(string(data), "lockfile_version = 99") {
		t.Errorf("future version not round-tripped:\n%s", data)
	}
}

func TestLockfileResourcesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lf := &Lockfile{LockfileVersion: CurrentLockfileVersion, Packages: []Entry{
		{
			Name: "@acme/revenue-table", Version: "1.0.0", Digest: "sha256:aaaa", Kind: "DataSource",
			Path: ".bino/registry/acme/revenue-table/revenue-table.yml", Direct: true,
			Resources: []ResourceEntry{
				{Name: "sales.csv", ContentHash: "sha256:cccc"},
				{Name: "notes.txt", ContentHash: "sha256:bbbb"},
			},
		},
	}}
	if err := SaveLockfile(dir, lf); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e := got.Get("@acme/revenue-table")
	if e == nil || len(e.Resources) != 2 {
		t.Fatalf("entry: %+v", e)
	}
	// Sorted by name on save.
	if e.Resources[0].Name != "notes.txt" || e.Resources[1].Name != "sales.csv" {
		t.Errorf("resources not sorted: %+v", e.Resources)
	}
	if e.Resources[1].ContentHash != "sha256:cccc" {
		t.Errorf("resource content hash mismatch: %+v", e.Resources[1])
	}
}

func TestLockfileMissingResourcesKeyIsNil(t *testing.T) {
	dir := t.TempDir()
	content := "lockfile_version = 1\n\n[[package]]\nname = \"@acme/x\"\nversion = \"1.0.0\"\ndigest = \"sha256:cc\"\nkind = \"Text\"\npath = \".bino/registry/acme/x/x.yml\"\ndirect = true\ndependencies = []\n"
	if err := os.WriteFile(filepath.Join(dir, LockfileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lf, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e := lf.Get("@acme/x")
	if e == nil || e.Resources != nil {
		t.Errorf("expected nil resources for an old-shape lockfile, got %+v", e)
	}
}

func TestLockfileUpsertRemove(t *testing.T) {
	lf := &Lockfile{LockfileVersion: 1}
	lf.Upsert(Entry{Name: "@a/x", Version: "1.0.0"})
	lf.Upsert(Entry{Name: "@a/x", Version: "2.0.0"})
	if len(lf.Packages) != 1 || lf.Get("@a/x").Version != "2.0.0" {
		t.Errorf("upsert did not dedup by name: %+v", lf.Packages)
	}
	if !lf.Remove("@a/x") || lf.Remove("@a/x") {
		t.Error("remove should report existence")
	}
	if lf.Get("@a/x") != nil {
		t.Error("entry still present after remove")
	}
}
