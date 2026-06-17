package duckdb

import (
	"context"
	"testing"
)

func TestBuiltinSignUDFs(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	defer s.Close()

	cases := []struct {
		query string
		want  string
	}{
		{"SELECT op(500)", "+"},
		{"SELECT op(-500)", "-"},
		{"SELECT op(0)", ""},
		{"SELECT iop(500)", "-"},
		{"SELECT iop(-500)", "+"},
		// Integer / BIGINT / DECIMAL inputs must implicitly cast to DOUBLE.
		{"SELECT op(CAST(-500 AS BIGINT))", "-"},
		{"SELECT op(CAST(-12.5 AS DECIMAL(18,2)))", "-"},
		// op composes with the built-in abs() to render a signed magnitude.
		{"SELECT op(-500) || abs(-500)::VARCHAR", "-500"},
		{"SELECT op(500) || abs(500)::VARCHAR", "+500"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			var got string
			if err := s.DB().QueryRowContext(ctx, tc.query).Scan(&got); err != nil {
				t.Fatalf("query %q: %v", tc.query, err)
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.query, got, tc.want)
			}
		})
	}

	// NULL in -> NULL out (the DuckDB default null handling).
	var nullResult *string
	if err := s.DB().QueryRowContext(ctx, "SELECT op(NULL::DOUBLE)").Scan(&nullResult); err != nil {
		t.Fatalf("op(NULL): %v", err)
	}
	if nullResult != nil {
		t.Errorf("op(NULL) = %q, want NULL", *nullResult)
	}
}
