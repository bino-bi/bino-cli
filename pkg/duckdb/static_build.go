//go:build duckdb_use_static_lib

package duckdb

// staticBuild is true when linking against a custom libduckdb_bundle.a
// that has extensions compiled in. Bundled extensions only need LOAD
// (not INSTALL) since they are part of the static library.
const staticBuild = true
