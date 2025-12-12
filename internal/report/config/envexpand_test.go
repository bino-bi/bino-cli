package config

import (
	"os"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		env            map[string]string
		wantExpanded   string
		wantMissingLen int
		wantMissing    []string
	}{
		{
			name:           "no variables",
			input:          "plain text without any variables",
			env:            nil,
			wantExpanded:   "plain text without any variables",
			wantMissingLen: 0,
		},
		{
			name:           "basic variable set",
			input:          "hello ${NAME}",
			env:            map[string]string{"NAME": "world"},
			wantExpanded:   "hello world",
			wantMissingLen: 0,
		},
		{
			name:           "basic variable not set",
			input:          "hello ${NAME}",
			env:            nil,
			wantExpanded:   "hello ",
			wantMissingLen: 1,
			wantMissing:    []string{"NAME"},
		},
		{
			name:           "variable with default - var set",
			input:          "host: ${DB_HOST:localhost}",
			env:            map[string]string{"DB_HOST": "production.db"},
			wantExpanded:   "host: production.db",
			wantMissingLen: 0,
		},
		{
			name:           "variable with default - var not set",
			input:          "host: ${DB_HOST:localhost}",
			env:            nil,
			wantExpanded:   "host: localhost",
			wantMissingLen: 0,
		},
		{
			name:           "variable with empty default - var not set",
			input:          "value: ${OPT:}",
			env:            nil,
			wantExpanded:   "value: ",
			wantMissingLen: 0,
		},
		{
			name:           "multiple variables",
			input:          "${PROTO}://${HOST}:${PORT}/${PATH}",
			env:            map[string]string{"PROTO": "https", "HOST": "api.example.com", "PORT": "443", "PATH": "v1"},
			wantExpanded:   "https://api.example.com:443/v1",
			wantMissingLen: 0,
		},
		{
			name:           "multiple missing variables",
			input:          "${A} and ${B} and ${C}",
			env:            nil,
			wantExpanded:   " and  and ",
			wantMissingLen: 3,
			wantMissing:    []string{"A", "B", "C"},
		},
		{
			name:           "mixed set and missing",
			input:          "${SET} and ${MISSING}",
			env:            map[string]string{"SET": "present"},
			wantExpanded:   "present and ",
			wantMissingLen: 1,
			wantMissing:    []string{"MISSING"},
		},
		{
			name:           "escaped variable",
			input:          `literal \${VAR} stays`,
			env:            map[string]string{"VAR": "should-not-appear"},
			wantExpanded:   "literal ${VAR} stays",
			wantMissingLen: 0,
		},
		{
			name:           "escaped and real variable",
			input:          `\${ESCAPED} and ${REAL}`,
			env:            map[string]string{"REAL": "expanded", "ESCAPED": "ignored"},
			wantExpanded:   "${ESCAPED} and expanded",
			wantMissingLen: 0,
		},
		{
			name:           "same variable multiple times",
			input:          "${X}${X}${X}",
			env:            nil,
			wantExpanded:   "",
			wantMissingLen: 1,
			wantMissing:    []string{"X"},
		},
		{
			name:           "variable in YAML context",
			input:          "spec:\n  path: ${DATA_DIR:./data}/sales.csv\n  host: ${DB_HOST}",
			env:            map[string]string{"DB_HOST": "localhost"},
			wantExpanded:   "spec:\n  path: ./data/sales.csv\n  host: localhost",
			wantMissingLen: 0,
		},
		{
			name:           "empty input",
			input:          "",
			env:            nil,
			wantExpanded:   "",
			wantMissingLen: 0,
		},
		{
			name:           "dollar without brace",
			input:          "$VAR and $OTHER",
			env:            map[string]string{"VAR": "value"},
			wantExpanded:   "$VAR and $OTHER",
			wantMissingLen: 0,
		},
		{
			name:           "unclosed brace",
			input:          "${VAR",
			env:            map[string]string{"VAR": "value"},
			wantExpanded:   "${VAR",
			wantMissingLen: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all test env vars first
			testVars := []string{"NAME", "DB_HOST", "PROTO", "HOST", "PORT", "PATH", "A", "B", "C", "SET", "MISSING", "VAR", "ESCAPED", "REAL", "X", "DATA_DIR", "OPT"}
			for _, v := range testVars {
				os.Unsetenv(v)
			}

			// Set env vars for this test
			for k, v := range tc.env {
				os.Setenv(k, v)
			}

			expanded, missing := ExpandEnvVars(tc.input)

			if expanded != tc.wantExpanded {
				t.Errorf("expanded = %q, want %q", expanded, tc.wantExpanded)
			}

			if len(missing) != tc.wantMissingLen {
				t.Errorf("len(missing) = %d, want %d; missing = %v", len(missing), tc.wantMissingLen, missing)
			}

			if tc.wantMissing != nil {
				for i, want := range tc.wantMissing {
					if i >= len(missing) {
						t.Errorf("missing[%d] not present, want %q", i, want)
						continue
					}
					if missing[i] != want {
						t.Errorf("missing[%d] = %q, want %q", i, missing[i], want)
					}
				}
			}

			// Cleanup
			for k := range tc.env {
				os.Unsetenv(k)
			}
		})
	}
}
