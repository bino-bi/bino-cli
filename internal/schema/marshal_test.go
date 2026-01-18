package schema

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestQueryField_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		field    QueryField
		expected string
	}{
		{
			name:     "inline query",
			field:    QueryField{Inline: "SELECT * FROM users"},
			expected: "SELECT * FROM users\n",
		},
		{
			name:     "file reference",
			field:    QueryField{File: "queries/users.sql"},
			expected: "$file: queries/users.sql\n",
		},
		{
			name:     "empty field",
			field:    QueryField{},
			expected: "\"\"\n",
		},
		{
			name:     "multiline inline query",
			field:    QueryField{Inline: "SELECT *\nFROM users\nWHERE active = true"},
			expected: "|-\n    SELECT *\n    FROM users\n    WHERE active = true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.field)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("got:\n%s\nwant:\n%s", string(data), tt.expected)
			}
		})
	}
}

func TestQueryField_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantInline   string
		wantFile     string
		wantErr      bool
	}{
		{
			name:       "inline query string",
			yaml:       "SELECT * FROM users",
			wantInline: "SELECT * FROM users",
		},
		{
			name:     "file reference map",
			yaml:     "$file: queries/users.sql",
			wantFile: "queries/users.sql",
		},
		{
			name:       "multiline inline query",
			yaml:       "|\n  SELECT *\n  FROM users",
			wantInline: "SELECT *\nFROM users",
		},
		{
			name:       "empty string",
			yaml:       "\"\"",
			wantInline: "",
		},
		{
			name: "null value",
			yaml: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var q QueryField
			err := yaml.Unmarshal([]byte(tt.yaml), &q)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if q.Inline != tt.wantInline {
				t.Errorf("Inline = %q, want %q", q.Inline, tt.wantInline)
			}
			if q.File != tt.wantFile {
				t.Errorf("File = %q, want %q", q.File, tt.wantFile)
			}
		})
	}
}

func TestQueryField_RoundTrip(t *testing.T) {
	tests := []QueryField{
		{Inline: "SELECT 1"},
		{File: "path/to/query.sql"},
		{Inline: "SELECT *\nFROM table\nWHERE x = 1"},
	}

	for _, original := range tests {
		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var parsed QueryField
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if parsed.Inline != original.Inline {
			t.Errorf("Inline mismatch: got %q, want %q", parsed.Inline, original.Inline)
		}
		if parsed.File != original.File {
			t.Errorf("File mismatch: got %q, want %q", parsed.File, original.File)
		}
	}
}

func TestDataSourceRef_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		ref      DataSourceRef
		expected string
	}{
		{
			name:     "string reference",
			ref:      DataSourceRef{Ref: "my_datasource"},
			expected: "my_datasource\n",
		},
		{
			name: "inline csv definition",
			ref: DataSourceRef{
				Inline: &DataSourceSpec{
					Type: DataSourceTypeCSV,
					Path: "data/sales.csv",
				},
			},
			expected: "type: csv\npath: data/sales.csv\n",
		},
		{
			name:     "empty ref",
			ref:      DataSourceRef{},
			expected: "null\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.ref)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("got:\n%s\nwant:\n%s", string(data), tt.expected)
			}
		})
	}
}

func TestDataSourceRef_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantRef    string
		wantInline bool
		wantType   DataSourceType
	}{
		{
			name:    "string reference",
			yaml:    "my_datasource",
			wantRef: "my_datasource",
		},
		{
			name:       "inline csv definition",
			yaml:       "type: csv\npath: data/sales.csv",
			wantInline: true,
			wantType:   DataSourceTypeCSV,
		},
		{
			name: "null value",
			yaml: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ref DataSourceRef
			err := yaml.Unmarshal([]byte(tt.yaml), &ref)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if ref.Ref != tt.wantRef {
				t.Errorf("Ref = %q, want %q", ref.Ref, tt.wantRef)
			}
			if tt.wantInline {
				if ref.Inline == nil {
					t.Fatal("expected Inline to be set")
				}
				if ref.Inline.Type != tt.wantType {
					t.Errorf("Inline.Type = %q, want %q", ref.Inline.Type, tt.wantType)
				}
			}
		})
	}
}

func TestDataSourceRef_RoundTrip(t *testing.T) {
	tests := []DataSourceRef{
		{Ref: "my_source"},
		{Inline: &DataSourceSpec{Type: DataSourceTypeCSV, Path: "data.csv"}},
		{Inline: &DataSourceSpec{Type: DataSourceTypeParquet, Path: "data.parquet"}},
		{Inline: &DataSourceSpec{
			Type: DataSourceTypePostgresQuery,
			Connection: &ConnectionSpec{
				Host:     "localhost",
				Port:     5432,
				Database: "mydb",
			},
			Query: "SELECT * FROM users",
		}},
	}

	for _, original := range tests {
		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var parsed DataSourceRef
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if parsed.Ref != original.Ref {
			t.Errorf("Ref mismatch: got %q, want %q", parsed.Ref, original.Ref)
		}
		if (parsed.Inline == nil) != (original.Inline == nil) {
			t.Errorf("Inline nil mismatch: got %v, want %v", parsed.Inline == nil, original.Inline == nil)
		}
		if parsed.Inline != nil && original.Inline != nil {
			if parsed.Inline.Type != original.Inline.Type {
				t.Errorf("Inline.Type mismatch: got %q, want %q", parsed.Inline.Type, original.Inline.Type)
			}
			if parsed.Inline.Path != original.Inline.Path {
				t.Errorf("Inline.Path mismatch: got %q, want %q", parsed.Inline.Path, original.Inline.Path)
			}
		}
	}
}

func TestDatasetRef_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		ref      DatasetRef
		expected string
	}{
		{
			name:     "string reference",
			ref:      DatasetRef{Ref: "my_dataset"},
			expected: "my_dataset\n",
		},
		{
			name: "inline definition with query",
			ref: DatasetRef{
				Inline: &DataSetSpec{
					Query: &QueryField{Inline: "SELECT 1"},
				},
			},
			expected: "query: SELECT 1\n",
		},
		{
			name:     "empty ref",
			ref:      DatasetRef{},
			expected: "null\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.ref)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("got:\n%s\nwant:\n%s", string(data), tt.expected)
			}
		})
	}
}

func TestDatasetRef_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantRef    string
		wantInline bool
	}{
		{
			name:    "string reference",
			yaml:    "my_dataset",
			wantRef: "my_dataset",
		},
		{
			name:       "inline definition",
			yaml:       "query: SELECT 1",
			wantInline: true,
		},
		{
			name: "null value",
			yaml: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ref DatasetRef
			err := yaml.Unmarshal([]byte(tt.yaml), &ref)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if ref.Ref != tt.wantRef {
				t.Errorf("Ref = %q, want %q", ref.Ref, tt.wantRef)
			}
			if tt.wantInline && ref.Inline == nil {
				t.Error("expected Inline to be set")
			}
		})
	}
}

func TestDatasetRef_RoundTrip(t *testing.T) {
	tests := []DatasetRef{
		{Ref: "my_dataset"},
		{Inline: &DataSetSpec{Query: &QueryField{Inline: "SELECT 1"}}},
		{Inline: &DataSetSpec{Query: &QueryField{File: "query.sql"}}},
		{Inline: &DataSetSpec{
			Query: &QueryField{Inline: "SELECT * FROM sales"},
			Dependencies: []DataSourceRef{
				{Ref: "sales_csv"},
			},
		}},
	}

	for _, original := range tests {
		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var parsed DatasetRef
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if parsed.Ref != original.Ref {
			t.Errorf("Ref mismatch: got %q, want %q", parsed.Ref, original.Ref)
		}
		if (parsed.Inline == nil) != (original.Inline == nil) {
			t.Errorf("Inline nil mismatch: got %v, want %v", parsed.Inline == nil, original.Inline == nil)
		}
	}
}

func TestDocument_MarshalYAML(t *testing.T) {
	doc := &Document{
		APIVersion: APIVersion,
		Kind:       KindDataSet,
		Metadata: Metadata{
			Name:        "test_dataset",
			Description: "A test dataset",
			Constraints: ConstraintListFromStrings([]string{"env == production"}),
		},
		Spec: &DataSetSpec{
			Query: &QueryField{Inline: "SELECT 1"},
			Dependencies: []DataSourceRef{
				{Ref: "my_source"},
			},
		},
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify key fields are present
	yamlStr := string(data)
	checks := []string{
		"apiVersion: bino.bi/v1alpha1",
		"kind: DataSet",
		"name: test_dataset",
		"description: A test dataset",
		"query: SELECT 1",
	}
	for _, check := range checks {
		if !contains(yamlStr, check) {
			t.Errorf("expected YAML to contain %q, got:\n%s", check, yamlStr)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
