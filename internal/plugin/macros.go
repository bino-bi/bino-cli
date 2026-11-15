package plugin

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"bino.bi/bino/internal/schema"
	"bino.bi/bino/pkg/duckdb"
)

// SessionMacro is a DuckDB macro a plugin wants in every session.
type SessionMacro struct {
	Name     string
	SQL      string
	SmokeSQL string
}

var (
	macroPluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	macroNamePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// reservedMacroPrefixes are kept for bino's own session functions and views.
var reservedMacroPrefixes = []string{"bino_", "_bino_"}

// checkBuiltinKinds refuses a plugin kind that has the name of a built-in
// kind. The built-in renders first, so the plugin kind would never be used.
func checkBuiltinKinds(m PluginManifest) error {
	for _, k := range m.Kinds {
		if schema.IsBuiltinKind(k.KindName) {
			return fmt.Errorf("plugin %q registers kind %q, which is a built-in kind; rename it (use one prefix for all kinds of the plugin)", m.Name, k.KindName)
		}
	}
	return nil
}

// checkSessionMacros validates the macros of one plugin and runs them once in
// a throwaway DuckDB session. It returns the macros in the form sessions use.
func checkSessionMacros(ctx context.Context, m PluginManifest) ([]duckdb.PluginMacro, error) {
	if len(m.SessionMacros) == 0 {
		return nil, nil
	}
	if !macroPluginNamePattern.MatchString(m.Name) {
		return nil, fmt.Errorf("plugin %q may not register DuckDB macros: its name must match %s", m.Name, macroPluginNamePattern)
	}
	prefix := m.Name + "_"
	seen := make(map[string]struct{}, len(m.SessionMacros))
	macros := make([]duckdb.PluginMacro, 0, len(m.SessionMacros))
	for _, sm := range m.SessionMacros {
		if err := validateSessionMacro(prefix, sm); err != nil {
			return nil, fmt.Errorf("macro %q: %w", sm.Name, err)
		}
		if _, dup := seen[sm.Name]; dup {
			return nil, fmt.Errorf("macro %q is declared twice", sm.Name)
		}
		seen[sm.Name] = struct{}{}
		macros = append(macros, duckdb.PluginMacro{Plugin: m.Name, Name: sm.Name, SQL: sm.SQL, SmokeSQL: sm.SmokeSQL})
	}
	if err := duckdb.CheckPluginMacros(ctx, macros); err != nil {
		return nil, err
	}
	return macros, nil
}

func validateSessionMacro(prefix string, sm SessionMacro) error {
	if !macroNamePattern.MatchString(sm.Name) {
		return fmt.Errorf("the name must match %s", macroNamePattern)
	}
	for _, reserved := range reservedMacroPrefixes {
		if strings.HasPrefix(sm.Name, reserved) {
			return fmt.Errorf("names starting with %q are reserved for bino", reserved)
		}
	}
	if !strings.HasPrefix(sm.Name, prefix) {
		return fmt.Errorf("the name must start with %q", prefix)
	}
	create := regexp.MustCompile(`(?is)^\s*CREATE\s+OR\s+REPLACE\s+MACRO\s+` + regexp.QuoteMeta(sm.Name) + `\s*\(`)
	if !create.MatchString(sm.SQL) {
		return fmt.Errorf("sql must be one \"CREATE OR REPLACE MACRO %s(...)\" statement", sm.Name)
	}
	if !singleStatement(sm.SQL) {
		return fmt.Errorf("sql must be exactly one statement")
	}
	if sm.SmokeSQL != "" && !singleStatement(sm.SmokeSQL) {
		return fmt.Errorf("smoke_sql must be exactly one statement")
	}
	return nil
}

// singleStatement reports whether sql holds one statement: no semicolon
// outside quotes and comments, except at the very end. DuckDB runs every
// statement of a string, so the host has to check this itself.
func singleStatement(sql string) bool {
	rest := strings.TrimRight(sql, " \t\r\n")
	rest = strings.TrimSuffix(rest, ";")
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; {
		case c == ';':
			return false
		case c == '\'' || c == '"':
			// A doubled quote inside is two adjacent literals to this scanner,
			// which gives the same answer.
			end := strings.IndexByte(rest[i+1:], c)
			if end < 0 {
				return false
			}
			i += end + 1
		case c == '-' && strings.HasPrefix(rest[i:], "--"):
			end := strings.IndexByte(rest[i:], '\n')
			if end < 0 {
				return true
			}
			i += end
		case c == '/' && strings.HasPrefix(rest[i:], "/*"):
			end := strings.Index(rest[i+2:], "*/")
			if end < 0 {
				return false
			}
			i += end + 3
		case c == '$':
			// Dollar-quoted string: $$...$$ or $tag$...$tag$.
			tagEnd := strings.IndexByte(rest[i+1:], '$')
			if tagEnd < 0 || !dollarTag(rest[i+1:i+1+tagEnd]) {
				continue
			}
			tag := rest[i : i+tagEnd+2]
			end := strings.Index(rest[i+len(tag):], tag)
			if end < 0 {
				return false
			}
			i += len(tag) + end + len(tag) - 1
		}
	}
	return true
}

func dollarTag(s string) bool {
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
