package duckdb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestDefaultExtensions(t *testing.T) {
	ext := DefaultExtensions()

	// excel and httpfs should always be present
	has := make(map[string]bool)
	for _, e := range ext {
		has[e] = true
	}
	if !has["excel"] || !has["httpfs"] {
		t.Errorf("DefaultExtensions() missing required extensions, got %v", ext)
	}

	// Verify it returns a new slice each call
	ext2 := DefaultExtensions()
	ext2[0] = "modified"
	fresh := DefaultExtensions()
	if fresh[0] == "modified" {
		t.Error("DefaultExtensions() should return a new slice each call")
	}
}

func TestCommunityExtensions(t *testing.T) {
	ext := CommunityExtensions()
	if len(ext) != len(communityExtensions) {
		t.Errorf("CommunityExtensions() returned %d extensions, want %d", len(ext), len(communityExtensions))
	}

	// Verify it returns a copy, not the original slice
	ext[0] = "modified"
	if communityExtensions[0] == "modified" {
		t.Error("CommunityExtensions() should return a copy, not the original slice")
	}
}

func TestExtensionNamePattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid lowercase", "excel", true},
		{"valid with numbers", "ext123", true},
		{"valid with underscore", "my_extension", true},
		{"valid mixed", "ext_123_abc", true},
		{"invalid uppercase", "Excel", false},
		{"invalid dash", "my-extension", false},
		{"invalid space", "my extension", false},
		{"invalid special chars", "ext@name", false},
		{"invalid dot", "ext.name", false},
		{"empty string", "", false},
		{"sql injection attempt", "excel; DROP TABLE users;--", false},
		{"path traversal attempt", "../../../etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extensionNamePattern.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("extensionNamePattern.MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts, err := DefaultOptions()
	if err != nil {
		t.Fatalf("DefaultOptions() error = %v", err)
	}

	if opts.CacheDir == "" {
		t.Error("DefaultOptions() should set CacheDir")
	}

	// Path should be empty (in-memory mode)
	if opts.Path != "" {
		t.Errorf("DefaultOptions() Path = %q, want empty string for in-memory mode", opts.Path)
	}
}

func TestOpenSession(t *testing.T) {
	ctx := context.Background()

	// Create temp dir for cache
	tmpDir, err := os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		CacheDir: tmpDir,
	}

	session, err := OpenSession(ctx, opts)
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer session.Close()

	// Verify DB is accessible
	if session.DB() == nil {
		t.Error("OpenSession() should create a valid DB connection")
	}

	// Verify we can execute a simple query
	var result int
	err = session.DB().QueryRowContext(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Errorf("failed to execute simple query: %v", err)
	}
	if result != 1 {
		t.Errorf("SELECT 1 returned %d, want 1", result)
	}
}

func TestSession_Close(t *testing.T) {
	// Test nil session
	var nilSession *Session
	if err := nilSession.Close(); err != nil {
		t.Errorf("Close() on nil session should not error, got %v", err)
	}

	// Test session with nil db
	emptySession := &Session{}
	if err := emptySession.Close(); err != nil {
		t.Errorf("Close() on session with nil db should not error, got %v", err)
	}

	// Test normal close
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	session, err := OpenSession(ctx, Options{CacheDir: tmpDir})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}

	if err := session.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// recheckCwd makes cwdWritableOnce evaluate again in the test's cwd.
func recheckCwd(t *testing.T) {
	t.Helper()
	orig := cwdWritableOnce
	cwdWritableOnce = sync.OnceValue(cwdWritable)
	t.Cleanup(func() { cwdWritableOnce = orig })
}

func tempDirectorySetting(ctx context.Context, t *testing.T, s *Session) string {
	t.Helper()
	var got string
	if err := s.DB().QueryRowContext(ctx, "SELECT current_setting('temp_directory')").Scan(&got); err != nil {
		t.Fatalf("read temp_directory: %v", err)
	}
	return got
}

func TestOpenSession_SpillsOutsideReadOnlyCwd(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix non-root user so a 0o555 dir is not writable")
	}
	dir := t.TempDir()
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("restore dir mode: %v", err)
		}
	})
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Chdir(dir)
	// Resolved so the check below holds even if DuckDB reports a real path (macOS /var).
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	recheckCwd(t)

	ctx := context.Background()
	s, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// A sort far larger than the memory limit must spill to disk.
	mustExec(ctx, t, s, "SET memory_limit='40MB'; SET threads=1; SET preserve_insertion_order=false;")
	var n int
	q := "SELECT count(*) FROM (SELECT i, md5(i::VARCHAR) h FROM range(6000000) t(i) ORDER BY h)"
	if err := s.DB().QueryRowContext(ctx, q).Scan(&n); err != nil {
		t.Fatalf("spilling query: %v", err)
	}
	if n != 6000000 {
		t.Errorf("count = %d, want 6000000", n)
	}

	spillDir := tempDirectorySetting(ctx, t, s)
	if filepath.Dir(spillDir) != tmp || !strings.HasPrefix(filepath.Base(spillDir), "bino-duckdb-") {
		t.Fatalf("temp_directory = %q, want a bino-duckdb-* dir in %q", spillDir, tmp)
	}
	if _, err := os.Stat(spillDir); err != nil {
		t.Errorf("spill dir after query: %v", err)
	}

	other, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("second OpenSession() error = %v", err)
	}
	otherDir := tempDirectorySetting(ctx, t, other)
	if err := other.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if otherDir == spillDir || filepath.Dir(otherDir) != tmp {
		t.Errorf("second session temp_directory = %q, want its own dir in %q (first: %q)", otherDir, tmp, spillDir)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	left, err := filepath.Glob(filepath.Join(tmp, "bino-duckdb-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("spill dirs left after Close: %v", left)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("session wrote into the read-only cwd: %v", entries)
	}
}

func TestOpenSession_KeepsDefaultSpillDirInWritableCwd(t *testing.T) {
	t.Chdir(t.TempDir())
	recheckCwd(t)

	ctx, s := openTestSession(t)
	if got := tempDirectorySetting(ctx, t, s); got != ".tmp" {
		t.Errorf("temp_directory = %q, want .tmp", got)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("session wrote into the cwd: %v", entries)
	}
}

func TestSession_ScratchDir(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	s := &Session{}

	dir, err := s.ScratchDir()
	if err != nil {
		t.Fatalf("ScratchDir() error = %v", err)
	}
	again, err := s.ScratchDir()
	if err != nil {
		t.Fatalf("second ScratchDir() error = %v", err)
	}
	if again != dir {
		t.Errorf("second ScratchDir() = %q, want %q", again, dir)
	}
	if filepath.Dir(dir) != os.TempDir() {
		t.Errorf("ScratchDir() = %q, want a dir in %q", dir, os.TempDir())
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("scratch dir missing: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("scratch dir after Close: stat err = %v, want not exist", err)
	}

	if late, err := s.ScratchDir(); err == nil {
		t.Errorf("ScratchDir() after Close = %q, want an error", late)
	}
	if entries, err := os.ReadDir(os.TempDir()); err != nil || len(entries) != 0 {
		t.Errorf("temp dir after Close: entries %v, err %v", entries, err)
	}
}

func TestSession_ScratchDirConcurrent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	s := &Session{}

	dirs := make([]string, 8)
	var wg sync.WaitGroup
	for i := range dirs {
		wg.Go(func() {
			dir, err := s.ScratchDir()
			if err != nil {
				t.Errorf("ScratchDir() error = %v", err)
			}
			dirs[i] = dir
		})
	}
	wg.Wait()
	for _, dir := range dirs {
		if dir != dirs[0] {
			t.Fatalf("ScratchDir() returned different dirs: %v", dirs)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if entries, err := os.ReadDir(os.TempDir()); err != nil || len(entries) != 0 {
		t.Errorf("temp dir after Close: entries %v, err %v", entries, err)
	}
}

func TestSession_LogQuery(t *testing.T) {
	var logged string
	logger := func(query string) {
		logged = query
	}

	session := &Session{queryLogger: logger}
	session.LogQuery("SELECT * FROM test")

	if logged != "SELECT * FROM test" {
		t.Errorf("LogQuery() logged %q, want %q", logged, "SELECT * FROM test")
	}

	// Test nil session
	var nilSession *Session
	nilSession.LogQuery("test") // Should not panic

	// Test session with nil logger
	sessionNoLogger := &Session{}
	sessionNoLogger.LogQuery("test") // Should not panic
}

func TestSession_LogQueryExec(t *testing.T) {
	var loggedMeta QueryExecMeta
	logger := func(meta QueryExecMeta) {
		loggedMeta = meta
	}

	session := &Session{queryExecLogger: logger}
	meta := QueryExecMeta{
		Query:     "SELECT * FROM test",
		QueryType: "test_query",
		RowCount:  5,
	}
	session.LogQueryExec(meta)

	if loggedMeta.Query != "SELECT * FROM test" {
		t.Errorf("LogQueryExec() logged query %q, want %q", loggedMeta.Query, "SELECT * FROM test")
	}
	if loggedMeta.RowCount != 5 {
		t.Errorf("LogQueryExec() logged row count %d, want 5", loggedMeta.RowCount)
	}

	// Test nil session
	var nilSession *Session
	nilSession.LogQueryExec(meta) // Should not panic

	// Test session with nil logger
	sessionNoLogger := &Session{}
	sessionNoLogger.LogQueryExec(meta) // Should not panic
}

func TestReadProxyEnv(t *testing.T) {
	// Save original env values
	origProxy := os.Getenv("http_proxy")
	origUsername := os.Getenv("http_proxy_username")
	origPassword := os.Getenv("http_proxy_password")
	defer func() {
		os.Setenv("http_proxy", origProxy)
		os.Setenv("http_proxy_username", origUsername)
		os.Setenv("http_proxy_password", origPassword)
	}()

	tests := []struct {
		name     string
		proxy    string
		username string
		password string
	}{
		{
			name:     "all values set",
			proxy:    "http://proxy.example.com:8080",
			username: "user",
			password: "pass",
		},
		{
			name:     "only proxy set",
			proxy:    "http://proxy.example.com:8080",
			username: "",
			password: "",
		},
		{
			name:     "no values set",
			proxy:    "",
			username: "",
			password: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("http_proxy", tt.proxy)
			os.Setenv("http_proxy_username", tt.username)
			os.Setenv("http_proxy_password", tt.password)

			cfg := readProxyEnv()

			if cfg.proxy != tt.proxy {
				t.Errorf("readProxyEnv() proxy = %q, want %q", cfg.proxy, tt.proxy)
			}
			if cfg.username != tt.username {
				t.Errorf("readProxyEnv() username = %q, want %q", cfg.username, tt.username)
			}
			if cfg.password != tt.password {
				t.Errorf("readProxyEnv() password = %q, want %q", cfg.password, tt.password)
			}
		})
	}
}

func TestInstallAndLoadExtensions_InvalidName(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	session, err := OpenSession(ctx, Options{CacheDir: tmpDir})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer session.Close()

	// Test invalid extension names
	invalidNames := []string{
		"Excel",              // uppercase
		"my-extension",       // dash
		"ext; DROP TABLE x;", // SQL injection
	}

	for _, name := range invalidNames {
		err := session.InstallAndLoadExtensions(ctx, []string{name})
		if err == nil {
			t.Errorf("InstallAndLoadExtensions(%q) should return error for invalid name", name)
		}
	}
}

func TestInstallAndLoadExtensions_NilSession(t *testing.T) {
	ctx := context.Background()

	var nilSession *Session
	err := nilSession.InstallAndLoadExtensions(ctx, []string{"excel"})
	if err == nil {
		t.Error("InstallAndLoadExtensions() on nil session should return error")
	}

	emptySession := &Session{}
	err = emptySession.InstallAndLoadExtensions(ctx, []string{"excel"})
	if err == nil {
		t.Error("InstallAndLoadExtensions() on session with nil db should return error")
	}
}

func TestInstallAndLoadCommunityExtensions_InvalidName(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	session, err := OpenSession(ctx, Options{CacheDir: tmpDir})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer session.Close()

	err = session.InstallAndLoadCommunityExtensions(ctx, []string{"Invalid-Name"})
	if err == nil {
		t.Error("InstallAndLoadCommunityExtensions() should return error for invalid name")
	}
}

func TestSession_ConcurrentBookkeeping(t *testing.T) {
	// Regression test for the unsynchronized map access that made a shared
	// session unsafe across net/http goroutines in `bino serve`.
	// Fails under -race (and with "concurrent map writes") without the
	// Session mutex.
	session := &Session{
		loadedExts:  make(map[string]struct{}),
		attachedDBs: make(map[string]struct{}),
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 200 {
				name := fmt.Sprintf("db_%d", i%10)
				session.MarkDBAttached(name)
				session.IsDBAttached(name)
				session.markExtLoaded(name)
				session.isExtLoaded(name)
			}
		})
	}
	wg.Wait()

	if !session.IsDBAttached("db_0") {
		t.Error("IsDBAttached(db_0) = false after MarkDBAttached")
	}
	if !session.isExtLoaded("db_0") {
		t.Error("isExtLoaded(db_0) = false after markExtLoaded")
	}
}

func TestInstallAndLoadCommunityExtensions_NilSession(t *testing.T) {
	ctx := context.Background()

	var nilSession *Session
	err := nilSession.InstallAndLoadCommunityExtensions(ctx, []string{"prql"})
	if err == nil {
		t.Error("InstallAndLoadCommunityExtensions() on nil session should return error")
	}
}
