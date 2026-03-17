package datasource

import (
	"fmt"
	"strings"

	"bino.bi/bino/internal/report/filehash"
)

func escapeSQLString(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// defaultGlobForType is a thin wrapper around filehash.DefaultGlobForType
// that uses the package-local source type constants.
func defaultGlobForType(sourceType string) string {
	return filehash.DefaultGlobForType(sourceType)
}

// buildPostgresConnection builds a connection string for postgres_query.
// Credentials are not embedded; they come from DuckDB secrets.
func buildPostgresConnection(conn sqlConnection) string {
	parts := make([]string, 0, 4)
	parts = append(parts,
		formatConnKV("host", conn.Host),
		formatConnKV("port", fmt.Sprintf("%d", conn.Port)),
		formatConnKV("dbname", conn.Database),
		formatConnKV("user", conn.User),
	)
	return strings.Join(parts, " ")
}

// buildMySQLConnection builds a connection string for mysql_query.
// Credentials are not embedded; they come from DuckDB secrets.
func buildMySQLConnection(conn sqlConnection) string {
	parts := make([]string, 0, 4)
	parts = append(parts,
		formatConnKV("host", conn.Host),
		formatConnKV("port", fmt.Sprintf("%d", conn.Port)),
		formatConnKV("user", conn.User),
		formatConnKV("database", conn.Database),
	)
	return strings.Join(parts, " ")
}

func formatConnKV(key, value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "'", "\\'")
	return fmt.Sprintf("%s='%s'", key, escaped)
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(val)
	default:
		return val
	}
}

// extensionForSource returns the DuckDB extension name required for a source type.
func extensionForSource(sourceType string) string {
	switch sourceType {
	case sourceTypePostgresQuery:
		return "postgres"
	case sourceTypeMySQLQuery:
		return "mysql"
	default:
		return ""
	}
}
