package datasource

import (
	"os"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestBuildCSVParams(t *testing.T) {
	tests := []struct {
		name string
		spec sourceSpec
		want string
	}{
		{
			name: "no options",
			spec: sourceSpec{Type: sourceTypeCSV},
			want: "",
		},
		{
			name: "delimiter only",
			spec: sourceSpec{Type: sourceTypeCSV, Delimiter: ";"},
			want: "delim = ';'",
		},
		{
			name: "header true",
			spec: sourceSpec{Type: sourceTypeCSV, Header: boolPtr(true)},
			want: "header = true",
		},
		{
			name: "header false",
			spec: sourceSpec{Type: sourceTypeCSV, Header: boolPtr(false)},
			want: "header = false",
		},
		{
			name: "skipRows",
			spec: sourceSpec{Type: sourceTypeCSV, SkipRows: 3},
			want: "skip = 3",
		},
		{
			name: "skipRows zero is omitted",
			spec: sourceSpec{Type: sourceTypeCSV, SkipRows: 0},
			want: "",
		},
		{
			name: "thousands",
			spec: sourceSpec{Type: sourceTypeCSV, Thousands: "."},
			want: "thousands = '.'",
		},
		{
			name: "decimalSeparator",
			spec: sourceSpec{Type: sourceTypeCSV, DecimalSeparator: ","},
			want: "decimal_separator = ','",
		},
		{
			name: "dateFormat",
			spec: sourceSpec{Type: sourceTypeCSV, DateFormat: "%d/%m/%Y"},
			want: "dateformat = '%d/%m/%Y'",
		},
		{
			name: "columns map",
			spec: sourceSpec{Type: sourceTypeCSV, Columns: map[string]string{
				"amount": "DECIMAL(10,2)",
				"name":   "VARCHAR",
			}},
			want: "columns = {'amount': 'DECIMAL(10,2)', 'name': 'VARCHAR'}",
		},
		{
			name: "columnNames",
			spec: sourceSpec{Type: sourceTypeCSV, ColumnNames: []string{"a", "b", "c"}},
			want: "names = ['a', 'b', 'c']",
		},
		{
			name: "combined European format",
			spec: sourceSpec{
				Type:             sourceTypeCSV,
				Delimiter:        ";",
				Thousands:        ".",
				DecimalSeparator: ",",
				DateFormat:       "%d/%m/%Y",
			},
			want: "delim = ';', thousands = '.', decimal_separator = ',', dateformat = '%d/%m/%Y'",
		},
		{
			name: "escaping single quotes in delimiter",
			spec: sourceSpec{Type: sourceTypeCSV, Delimiter: "'"},
			want: "delim = ''''",
		},
		{
			name: "escaping single quotes in column names",
			spec: sourceSpec{Type: sourceTypeCSV, ColumnNames: []string{"it's", "ok"}},
			want: "names = ['it''s', 'ok']",
		},
		{
			name: "escaping single quotes in columns map",
			spec: sourceSpec{Type: sourceTypeCSV, Columns: map[string]string{
				"col'1": "VARCHAR",
			}},
			want: "columns = {'col''1': 'VARCHAR'}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCSVParams(tt.spec)
			if got != tt.want {
				t.Errorf("buildCSVParams() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuckDBStruct(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		want string
	}{
		{
			name: "empty",
			m:    map[string]string{},
			want: "{}",
		},
		{
			name: "single entry",
			m:    map[string]string{"id": "INTEGER"},
			want: "{'id': 'INTEGER'}",
		},
		{
			name: "multiple sorted",
			m:    map[string]string{"b": "VARCHAR", "a": "INT"},
			want: "{'a': 'INT', 'b': 'VARCHAR'}",
		},
		{
			name: "escaping",
			m:    map[string]string{"it's": "VARCHAR(10)"},
			want: "{'it''s': 'VARCHAR(10)'}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuckDBStruct(tt.m)
			if got != tt.want {
				t.Errorf("formatDuckDBStruct() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuckDBList(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{
			name:  "empty",
			items: []string{},
			want:  "[]",
		},
		{
			name:  "single",
			items: []string{"col1"},
			want:  "['col1']",
		},
		{
			name:  "multiple",
			items: []string{"a", "b", "c"},
			want:  "['a', 'b', 'c']",
		},
		{
			name:  "escaping",
			items: []string{"it's", "fine"},
			want:  "['it''s', 'fine']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuckDBList(tt.items)
			if got != tt.want {
				t.Errorf("formatDuckDBList() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCSVSourceSQL(t *testing.T) {
	// Create a temp dir with a CSV file for local path tests
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.csv"
	if err := writeTestFile(tmpFile, "a,b\n1,2\n"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		spec         sourceSpec
		wantPrefix   string // check that output starts with this
		wantSuffix   string // check that output ends with this
		wantContains []string
		wantErr      bool
	}{
		{
			name: "no options uses read_csv_auto",
			spec: sourceSpec{
				Type:    sourceTypeCSV,
				Path:    tmpFile,
				BaseDir: tmpDir,
			},
			wantPrefix: "SELECT * FROM read_csv_auto('",
		},
		{
			name: "with options uses read_csv",
			spec: sourceSpec{
				Type:      sourceTypeCSV,
				Path:      tmpFile,
				BaseDir:   tmpDir,
				Delimiter: ";",
			},
			wantPrefix:   "SELECT * FROM read_csv('",
			wantContains: []string{"delim = ';'"},
		},
		{
			name: "URL with options",
			spec: sourceSpec{
				Type:             sourceTypeCSV,
				Path:             "https://example.com/data.csv",
				Thousands:        ".",
				DecimalSeparator: ",",
			},
			wantPrefix:   "SELECT * FROM read_csv('https://example.com/data.csv',",
			wantContains: []string{"thousands = '.'", "decimal_separator = ','"},
		},
		{
			name: "URL without options",
			spec: sourceSpec{
				Type: sourceTypeCSV,
				Path: "https://example.com/data.csv",
			},
			wantPrefix: "SELECT * FROM read_csv_auto('https://example.com/data.csv')",
		},
		{
			name: "glob with options",
			spec: sourceSpec{
				Type:    sourceTypeCSV,
				Path:    tmpDir + "/*.csv",
				BaseDir: tmpDir,
				Header:  boolPtr(false),
			},
			wantPrefix:   "SELECT * FROM read_csv('",
			wantContains: []string{"*.csv'", "header = false"},
		},
		{
			name: "all options combined",
			spec: sourceSpec{
				Type:             sourceTypeCSV,
				Path:             tmpFile,
				BaseDir:          tmpDir,
				Delimiter:        "\t",
				Header:           boolPtr(true),
				SkipRows:         2,
				Thousands:        ".",
				DecimalSeparator: ",",
				DateFormat:       "%d/%m/%Y",
				Columns:          map[string]string{"id": "INTEGER", "name": "VARCHAR"},
				ColumnNames:      []string{"x", "y"},
			},
			wantPrefix: "SELECT * FROM read_csv('",
			wantContains: []string{
				"delim = '\t'",
				"header = true",
				"skip = 2",
				"thousands = '.'",
				"decimal_separator = ','",
				"dateformat = '%d/%m/%Y'",
				"columns = {'id': 'INTEGER', 'name': 'VARCHAR'}",
				"names = ['x', 'y']",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCSVSourceSQL(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildCSVSourceSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("buildCSVSourceSQL() = %q, want prefix %q", got, tt.wantPrefix)
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("buildCSVSourceSQL() = %q, want suffix %q", got, tt.wantSuffix)
			}
			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildCSVSourceSQL() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
