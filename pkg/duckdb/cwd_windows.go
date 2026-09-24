//go:build windows

package duckdb

// cwdWritable assumes a writable working directory on Windows, so DuckDB
// keeps its default spill directory there.
func cwdWritable() bool { return true }
