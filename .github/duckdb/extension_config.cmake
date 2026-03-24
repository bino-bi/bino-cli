# Custom DuckDB extension configuration for bino-cli static builds.
#
# This replaces BUILD_EXTENSIONS to:
# - Use correct cmake names (postgres_scanner, mysql_scanner)
# - Remove DONT_LINK so extensions are statically linked into the bundle
# - Remove NOT MINGW guards so extensions build for Windows cross-compilation
#
# GIT_TAG values are pinned to DuckDB v1.4.4 compatible commits.

# Built-in extension (in DuckDB's extension/ directory)
duckdb_extension_load(autocomplete)

# External extensions (fetched from GitHub)
duckdb_extension_load(excel
    GIT_URL https://github.com/duckdb/duckdb-excel
    GIT_TAG 9421a2d75bd7544336caa73e5f9e6063cc7f6992
)

duckdb_extension_load(postgres_scanner
    GIT_URL https://github.com/duckdb/duckdb-postgres
    GIT_TAG b9fce43bc5d36bc6db70844f28b7b146e756eb22
)

duckdb_extension_load(mysql_scanner
    GIT_URL https://github.com/duckdb/duckdb-mysql
    GIT_TAG 35d1b2cd51800096271802cfedf68e13bf7fa8cb
)

duckdb_extension_load(httpfs
    GIT_URL https://github.com/duckdb/duckdb-httpfs
    GIT_TAG 13f8a814d41a978c3f19eb1dc76069489652ea6f
)
