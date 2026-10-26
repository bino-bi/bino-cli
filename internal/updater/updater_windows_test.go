package updater

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestInstallFilesWithLoadedDLL replaces a DLL that this process has loaded,
// the same situation as `bino update` with duckdb.dll in use.
func TestInstallFilesWithLoadedDLL(t *testing.T) {
	f := newInstallFixture(t)

	// Any real DLL works. Copy a system one so it loads under the name duckdb.dll.
	src, err := os.Open(filepath.Join(os.Getenv("SystemRoot"), "System32", "version.dll"))
	if err != nil {
		t.Fatalf("open system dll: %v", err)
	}
	defer src.Close()
	dst, err := os.Create(f.dll)
	if err != nil {
		t.Fatalf("create dll: %v", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatalf("copy dll: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	dll, err := syscall.LoadDLL(f.dll)
	if err != nil {
		t.Fatalf("load dll: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = dll.Release()
		}
	}
	defer release()

	// Writing in place is what the old updater did. It must fail, or this
	// test does not hold the lock it is about.
	if err := os.WriteFile(f.dll, []byte("x"), 0o644); err == nil {
		t.Fatal("overwriting a loaded DLL succeeded; the test does not reproduce the lock")
	}

	if err := installFiles(f.exe, f.newExe, f.newDLL); err != nil {
		t.Fatalf("installFiles() error = %v", err)
	}
	assertContent(t, f.exe, "new-exe")
	assertContent(t, f.dll, "new-dll")

	// The loaded backup cannot be deleted yet. It is gone after the next start.
	if _, err := os.Stat(f.dll + ".old"); err != nil {
		t.Fatalf("expected %s.old to remain while loaded: %v", dllName, err)
	}
	release()
	removeOldFiles(f.exe)
	assertMissing(t, f.dll+".old")
}
