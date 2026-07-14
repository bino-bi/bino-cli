package datasource

import (
	"strings"
	"testing"
)

// TestBuildAttachSQLQuotesSpecialNames proves that a DataSource name
// containing characters outside a bare SQL identifier (as allowed for a
// "@scope/name" registry identity, e.g. "@acme/revenue-table") produces a
// correctly quoted ATTACH statement rather than a broken one. Before this
// fix, postgresAttachName/mysqlAttachName spliced the name straight into
// "AS <name>" unquoted, which only stayed safe because the schema forbade
// hyphens, '@', and '/' in a DataSource name — now that scoped names are
// allowed, the AS clause must be quoted.
func TestBuildAttachSQLQuotesSpecialNames(t *testing.T) {
	tests := []struct {
		name           string
		wantAttachName string
		build          func(sourceSpec) (string, string)
	}{
		{"postgres, no secret", "_pg_@acme/revenue-table", func(s sourceSpec) (string, string) {
			return buildPostgresAttachSQL(s)
		}},
		{"mysql, no secret", "_mysql_@acme/revenue-table", func(s sourceSpec) (string, string) {
			return buildMySQLAttachSQL(s)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := sourceSpec{
				Name:       "@acme/revenue-table",
				Connection: &sqlConnection{Host: "db.example.com", Database: "app"},
			}
			attachName, attachSQL := tt.build(spec)
			if attachName != tt.wantAttachName {
				t.Errorf("attachName = %q, want %q", attachName, tt.wantAttachName)
			}
			wantClause := `AS "` + tt.wantAttachName + `"`
			if !strings.Contains(attachSQL, wantClause) {
				t.Errorf("attachSQL = %q, want it to contain quoted %q", attachSQL, wantClause)
			}
		})
	}
}

func TestBuildAttachSQLQuotesSpecialNamesWithSecret(t *testing.T) {
	spec := sourceSpec{
		Name:       "@acme/revenue-table",
		Connection: &sqlConnection{Host: "db.example.com", Database: "app", Secret: "acme-secret"},
	}
	attachName, attachSQL := buildPostgresAttachSQL(spec)
	wantAttachName := "_pg_@acme/revenue-table"
	if attachName != wantAttachName {
		t.Errorf("attachName = %q, want %q", attachName, wantAttachName)
	}
	if !strings.Contains(attachSQL, `AS "`+wantAttachName+`"`) {
		t.Errorf("attachSQL = %q, want a quoted AS clause", attachSQL)
	}
	if !strings.Contains(attachSQL, `SECRET "acme-secret"`) {
		t.Errorf("attachSQL = %q, want a quoted SECRET clause", attachSQL)
	}
}
