//go:build !duckdb_use_static_lib

package duckdb

// staticBuild is false for default builds that use the pre-built
// duckdb-go-bindings. Extensions must be downloaded via INSTALL
// before they can be LOADed.
const staticBuild = false
