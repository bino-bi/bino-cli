package registry

import (
	"os"
	"path/filepath"
	"runtime"
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
	if rel != ".bino/registry/acme/revenue-table/revenue-table.yml" {
		t.Errorf("rel = %q", rel)
	}
	if !strings.HasPrefix(abs, StoreDir(root)) {
		t.Errorf("abs %q not under store dir", abs)
	}
}

func TestPackageDir(t *testing.T) {
	root := t.TempDir()
	abs, rel, err := PackageDir(root, "@acme/revenue-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != ".bino/registry/acme/revenue-table" {
		t.Errorf("rel = %q", rel)
	}
	if !strings.HasPrefix(abs, StoreDir(root)) {
		t.Errorf("abs %q not under store dir", abs)
	}
	for _, name := range []string{"@../x", "@a/..", "@A/b"} {
		if _, _, err := PackageDir(root, name); err == nil {
			t.Errorf("PackageDir(%q): expected error", name)
		}
	}
}

func TestResourcePathGuard(t *testing.T) {
	root := t.TempDir()
	bad := []string{
		"../../etc/passwd", "/etc/passwd", "..", "a..b", "..sales.csv",
		".sales.csv", "", strings.Repeat("a", 256), "sales/csv", "sales\\csv",
	}
	for _, name := range bad {
		if _, _, err := ResourcePath(root, "@acme/revenue-table", name); err == nil {
			t.Errorf("ResourcePath(%q): expected error", name)
		}
	}
	abs, rel, err := ResourcePath(root, "@acme/revenue-table", "sales.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != ".bino/registry/acme/revenue-table/sales.csv" {
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
	// No temp residue in the package dir.
	pkgDir := filepath.Dir(abs)
	entries, _ := os.ReadDir(pkgDir)
	if len(entries) != 1 {
		t.Errorf("unexpected files in package dir: %v", entries)
	}
	if err := RemovePackage(root, "@acme/greeting"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Error("file still present after remove")
	}
	// Whole package dir removed.
	if _, err := os.Stat(pkgDir); !os.IsNotExist(err) {
		t.Error("package dir not removed")
	}
	// Empty scope dir pruned.
	if _, err := os.Stat(filepath.Dir(pkgDir)); !os.IsNotExist(err) {
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
	if _, err := os.Stat(filepath.Join(StoreDir(root), "acme", "b", "b.yml")); err != nil {
		t.Errorf("sibling package lost: %v", err)
	}
}

func TestWriteResource(t *testing.T) {
	root := t.TempDir()
	if _, err := WritePackage(root, "@acme/revenue-table", []byte("kind: DataSource\n")); err != nil {
		t.Fatal(err)
	}
	if err := WriteResource(root, "@acme/revenue-table", "sales.csv", []byte("id,amount\n1,10\n")); err != nil {
		t.Fatalf("write resource: %v", err)
	}
	abs, _, err := ResourcePath(root, "@acme/revenue-table", "sales.csv")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(abs)
	if err != nil || string(data) != "id,amount\n1,10\n" {
		t.Fatalf("read back: %v, %q", err, data)
	}
	// Sits next to the package's document.
	pkgAbs, _, err := StorePath(root, "@acme/revenue-table")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(abs) != filepath.Dir(pkgAbs) {
		t.Errorf("resource dir %q != package doc dir %q", filepath.Dir(abs), filepath.Dir(pkgAbs))
	}
}

func TestRemovePackageRemovesResources(t *testing.T) {
	root := t.TempDir()
	if _, err := WritePackage(root, "@acme/revenue-table", []byte("kind: DataSource\n")); err != nil {
		t.Fatal(err)
	}
	if err := WriteResource(root, "@acme/revenue-table", "sales.csv", []byte("id,amount\n1,10\n")); err != nil {
		t.Fatal(err)
	}
	pkgAbs, _, err := PackageDir(root, "@acme/revenue-table")
	if err != nil {
		t.Fatal(err)
	}
	if err := RemovePackage(root, "@acme/revenue-table"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(pkgAbs); !os.IsNotExist(err) {
		t.Error("package dir (with resource) not removed")
	}
}

func TestWriteTreeFileMaterializesASubdirectory(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct{ path, body string }{
		{"kit.yaml", "kind: LayoutPage"},
		{"components/sales.yaml", "kind: Table"},
		{"resources/logo.png", "png"},
	} {
		rel, err := WriteTreeFile(dir, "@acme/kit", f.path, []byte(f.body))
		if err != nil {
			t.Fatalf("WriteTreeFile(%s): %v", f.path, err)
		}
		if want := ".bino/registry/acme/kit/" + f.path; rel != want {
			t.Errorf("rel = %s, want %s", rel, want)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(data) != f.body {
			t.Errorf("read %s: %q, %v", rel, data, err)
		}
	}
	files, err := ListPackageFiles(dir, "@acme/kit")
	if err != nil {
		t.Fatalf("ListPackageFiles: %v", err)
	}
	want := []string{"components/sales.yaml", "kit.yaml", "resources/logo.png"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("files = %v, want %v", files, want)
	}
}

func TestTreeFilePathRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"../evil.yaml", "../../../../etc/passwd", "/etc/passwd", "a/../../b",
		"a/b/c.yaml", `..\evil`, "..", "",
	} {
		if _, _, err := TreeFilePath(dir, "@acme/kit", p); err == nil {
			t.Errorf("TreeFilePath(%q) = nil error, want a rejection", p)
		}
		if _, err := WriteTreeFile(dir, "@acme/kit", p, []byte("x")); err == nil {
			t.Errorf("WriteTreeFile(%q) = nil error, want a rejection", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.yaml")); !os.IsNotExist(err) {
		t.Error("a rejected path still wrote a file")
	}
}

// storeContain is lexical, so a symlinked directory component would otherwise
// let MkdirAll walk out of the store and the write land outside the project.
func TestWriteTreeFileRefusesToWriteThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	pkgDir := filepath.Join(dir, ".bino", "registry", "acme", "kit")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pkgDir, "resources")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteTreeFile(dir, "@acme/kit", "resources/logo.png", []byte("png")); err == nil {
		t.Fatal("expected the write to be refused")
	}
	if _, err := os.Stat(filepath.Join(outside, "logo.png")); !os.IsNotExist(err) {
		t.Error("the payload landed outside the store")
	}
}

func TestRemoveTreeFilePrunesEmptyDirectories(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteTreeFile(dir, "@acme/kit", "components/sales.yaml", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTreeFile(dir, "@acme/kit", "components/sales.yaml"); err != nil {
		t.Fatalf("RemoveTreeFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "kit", "components")); !os.IsNotExist(err) {
		t.Error("the emptied directory was not pruned")
	}
	// The package directory itself stays; only the lock decides whether a
	// package is installed.
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "kit")); err != nil {
		t.Errorf("package directory removed: %v", err)
	}
	// Removing a file that is already gone is not an error.
	if err := RemoveTreeFile(dir, "@acme/kit", "components/sales.yaml"); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestListPackageFilesOfAMissingPackage(t *testing.T) {
	files, err := ListPackageFiles(t.TempDir(), "@acme/kit")
	if err != nil {
		t.Fatalf("ListPackageFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}
