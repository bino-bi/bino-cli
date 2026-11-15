package duckdb

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// PluginMacro is one SQL macro a plugin adds to every session.
type PluginMacro struct {
	// Plugin is the name of the plugin that declares the macro.
	Plugin string
	Name   string
	// SQL is one "CREATE OR REPLACE MACRO <Name> ..." statement.
	SQL string
	// SmokeSQL is an optional query that calls the macro. Only
	// CheckPluginMacros runs it.
	SmokeSQL string
}

// pluginMacroState is process-wide on purpose: sessions are opened in many
// places that know nothing about plugins, and every one of them must see the
// same functions, or a DataSet works in build and fails in the editor.
var pluginMacroState struct {
	mu       sync.Mutex
	macros   []PluginMacro
	sessions map[*Session]struct{}
}

// PluginMacros returns the macros every session gets, sorted by name.
func PluginMacros() []PluginMacro {
	pluginMacroState.mu.Lock()
	defer pluginMacroState.mu.Unlock()
	return append([]PluginMacro(nil), pluginMacroState.macros...)
}

// SetPluginMacros replaces the macros every session gets. Sessions opened
// later register them after the built-in functions. Open sessions are brought
// in line right away: removed macros are dropped, new and changed ones are
// created. Errors from open sessions are joined; the new set is kept anyway.
func SetPluginMacros(ctx context.Context, macros []PluginMacro) error {
	next := append([]PluginMacro(nil), macros...)
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })

	pluginMacroState.mu.Lock()
	defer pluginMacroState.mu.Unlock()

	keep := make(map[string]string, len(next))
	for _, m := range next {
		keep[m.Name] = m.SQL
	}
	var drop []string
	current := make(map[string]string, len(pluginMacroState.macros))
	for _, m := range pluginMacroState.macros {
		current[m.Name] = m.SQL
		if _, ok := keep[m.Name]; !ok {
			drop = append(drop, m.Name)
		}
	}
	var create []PluginMacro
	for _, m := range next {
		if sql, ok := current[m.Name]; !ok || sql != m.SQL {
			create = append(create, m)
		}
	}
	pluginMacroState.macros = next

	var errs []error
	for s := range pluginMacroState.sessions {
		for _, name := range drop {
			if err := s.dropMacro(ctx, name); err != nil {
				errs = append(errs, err)
			}
		}
		if err := s.createMacros(ctx, create); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CheckPluginMacros creates the macros in a throwaway session and runs each
// SmokeSQL. DuckDB binds at creation only what does not depend on the
// arguments (a table read through query_table is bound in the call), so the
// smoke query is what finds a wrong column there. A macro must not replace a
// function that already exists.
func CheckPluginMacros(ctx context.Context, macros []PluginMacro) error {
	if len(macros) == 0 {
		return nil
	}
	s, err := openSession(ctx, Options{}, false)
	if err != nil {
		return err
	}
	defer s.Close() //nolint:errcheck // throwaway in-memory session

	for _, m := range macros {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_functions() WHERE function_name = ?", m.Name).Scan(&n); err != nil {
			return fmt.Errorf("macro %q: look up existing functions: %w", m.Name, err)
		}
		if n > 0 {
			return fmt.Errorf("macro %q: a function with this name already exists", m.Name)
		}
		if _, err := s.db.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("macro %q: %w", m.Name, err)
		}
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_functions() WHERE function_name = ?", m.Name).Scan(&n); err != nil {
			return fmt.Errorf("macro %q: look up the new macro: %w", m.Name, err)
		}
		if n == 0 {
			return fmt.Errorf("macro %q: the statement did not create a macro with this name", m.Name)
		}
	}
	// After all are created, so a smoke query may use the plugin's other macros.
	for _, m := range macros {
		if m.SmokeSQL == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, m.SmokeSQL)
		if err != nil {
			return fmt.Errorf("macro %q: smoke query: %w", m.Name, err)
		}
		for rows.Next() { //nolint:revive // drain: some errors only show while reading rows
		}
		err = rows.Err()
		rows.Close() //nolint:errcheck,sqlclosecheck // rows.Err is already read
		if err != nil {
			return fmt.Errorf("macro %q: smoke query: %w", m.Name, err)
		}
	}
	return nil
}

// trackPluginMacros registers the current plugin macros in s and keeps s in
// line with later SetPluginMacros calls until it is closed.
func (s *Session) trackPluginMacros(ctx context.Context) error {
	pluginMacroState.mu.Lock()
	defer pluginMacroState.mu.Unlock()
	if err := s.createMacros(ctx, pluginMacroState.macros); err != nil {
		return err
	}
	if pluginMacroState.sessions == nil {
		pluginMacroState.sessions = make(map[*Session]struct{})
	}
	pluginMacroState.sessions[s] = struct{}{}
	return nil
}

func (s *Session) untrackPluginMacros() {
	pluginMacroState.mu.Lock()
	delete(pluginMacroState.sessions, s)
	pluginMacroState.mu.Unlock()
}

func (s *Session) createMacros(ctx context.Context, macros []PluginMacro) error {
	for _, m := range macros {
		if _, err := s.db.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("register macro %q of plugin %q: %w", m.Name, m.Plugin, err)
		}
	}
	return nil
}

// dropMacro removes a scalar or table macro. The name comes from a validated
// plugin manifest (lower-case letters, digits and underscores).
func (s *Session) dropMacro(ctx context.Context, name string) error {
	for _, stmt := range []string{"DROP MACRO IF EXISTS " + name, "DROP MACRO TABLE IF EXISTS " + name} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop macro %q: %w", name, err)
		}
	}
	return nil
}
