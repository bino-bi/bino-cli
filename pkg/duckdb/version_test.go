package duckdb

import (
	"os"
	"regexp"
	"testing"
)

// The Windows release links the DuckDB DLL named by DUCKDB_VERSION in
// release.yml, while every other platform uses the library bundled with the
// duckdb-go bindings in go.mod. Nothing ties the two together, so a go.mod bump
// alone would ship Windows with an older DuckDB.
func TestReleaseDuckDBVersionMatchesBindings(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s+DUCKDB_VERSION:\s*"([^"]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no DUCKDB_VERSION in release.yml")
	}

	ctx, s := openTestSession(t)
	var linked string
	if err := s.DB().QueryRowContext(ctx, "SELECT version()").Scan(&linked); err != nil {
		t.Fatalf("query version: %v", err)
	}
	if string(m[1]) != linked {
		t.Errorf("release.yml DUCKDB_VERSION is %s, but the duckdb-go bindings in go.mod link DuckDB %s", m[1], linked)
	}
}
