package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePathGuard(t *testing.T) {
	root := t.TempDir()
	bad := []string{
		"@../x", "@a/..", "@a/../../etc", "@A/b", "@a/B", "@a/b/c", "@a", "a/b", "@a/", "@/b", "@a/b.yml",
	}
	for _, name := range bad {
		if _, _, err := StorePath(root, name); err == nil {
			t.Errorf("StorePath(%q): expected error", name)
		}
	}
	abs, rel, err := StorePath(root, "@acme/revenue-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != ".bino/registry/acme/revenue-table.yml" {
		t.Errorf("rel = %q", rel)
	}
	if !strings.HasPrefix(abs, StoreDir(root)) {
		t.Errorf("abs %q not under store dir", abs)
	}
}

func TestWriteAndRemovePackage(t *testing.T) {
	root := t.TempDir()
	rel, err := WritePackage(root, "@acme/greeting", []byte("kind: Text\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil || string(data) != "kind: Text\n" {
		t.Fatalf("read back: %v, %q", err, data)
	}
	// No temp residue.
	entries, _ := os.ReadDir(filepath.Dir(abs))
	if len(entries) != 1 {
		t.Errorf("unexpected files in scope dir: %v", entries)
	}
	if err := RemovePackage(root, "@acme/greeting"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Error("file still present after remove")
	}
	// Empty scope dir pruned.
	if _, err := os.Stat(filepath.Dir(abs)); !os.IsNotExist(err) {
		t.Error("empty scope dir not pruned")
	}
	// Removing a missing package is not an error.
	if err := RemovePackage(root, "@acme/greeting"); err != nil {
		t.Errorf("remove missing: %v", err)
	}
}

func TestRemovePackageKeepsNonEmptyScopeDir(t *testing.T) {
	root := t.TempDir()
	if _, err := WritePackage(root, "@acme/a", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePackage(root, "@acme/b", []byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := RemovePackage(root, "@acme/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(StoreDir(root), "acme", "b.yml")); err != nil {
		t.Errorf("sibling package lost: %v", err)
	}
}
