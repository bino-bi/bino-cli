package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := map[string]struct {
		name    string
		wantErr bool
	}{
		"valid simple":            {name: "sales", wantErr: false},
		"valid with underscore":   {name: "sales_data", wantErr: false},
		"valid with number":       {name: "sales2024", wantErr: false},
		"valid long":              {name: "this_is_a_very_long_but_still_valid_name", wantErr: false},
		"empty":                   {name: "", wantErr: true},
		"starts with number":      {name: "2024sales", wantErr: true},
		"starts with underscore":  {name: "_sales", wantErr: true},
		"contains hyphen":         {name: "sales-data", wantErr: true},
		"contains uppercase":      {name: "salesData", wantErr: true},
		"contains space":          {name: "sales data", wantErr: true},
		"reserved prefix":         {name: "_inline_data", wantErr: true},
		"too long":                {name: "a123456789012345678901234567890123456789012345678901234567890123456789", wantErr: true},
		"exactly 64 chars":        {name: "a234567890123456789012345678901234567890123456789012345678901234", wantErr: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateName(tc.name)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestSuggestNameFix(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"already valid":          {input: "sales_data", want: "sales_data"},
		"uppercase":              {input: "SalesData", want: "salesdata"},
		"hyphens":                {input: "sales-data", want: "sales_data"},
		"spaces":                 {input: "sales data", want: "sales_data"},
		"mixed":                  {input: "Sales-Data 2024", want: "sales_data_2024"},
		"starts with number":     {input: "2024sales", want: "ds_2024sales"},
		"empty":                  {input: "", want: ""},
		"only special chars":     {input: "---", want: "ds_"},
		"trailing underscore":    {input: "sales_", want: "sales"},
		"consecutive underscores": {input: "sales__data", want: "sales_data"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := SuggestNameFix(tc.input)
			if got != tc.want {
				t.Errorf("SuggestNameFix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseDataSourceType(t *testing.T) {
	tests := map[string]struct {
		input string
		want  DataSourceType
	}{
		"postgres":    {input: "postgres", want: DataSourceTypePostgres},
		"postgresql":  {input: "postgresql", want: DataSourceTypePostgres},
		"POSTGRES":    {input: "POSTGRES", want: DataSourceTypePostgres},
		"mysql":       {input: "mysql", want: DataSourceTypeMySQL},
		"csv":         {input: "csv", want: DataSourceTypeCSV},
		"parquet":     {input: "parquet", want: DataSourceTypeParquet},
		"excel":       {input: "excel", want: DataSourceTypeExcel},
		"xlsx":        {input: "xlsx", want: DataSourceTypeExcel},
		"json":        {input: "json", want: DataSourceTypeJSON},
		"inline":      {input: "inline", want: DataSourceTypeInline},
		"unknown":     {input: "unknown", want: DataSourceTypeNone},
		"empty":       {input: "", want: DataSourceTypeNone},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := ParseDataSourceType(tc.input)
			if got != tc.want {
				t.Errorf("ParseDataSourceType(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestDataSourceTypeString(t *testing.T) {
	tests := map[DataSourceType]string{
		DataSourceTypePostgres: "PostgreSQL",
		DataSourceTypeMySQL:    "MySQL",
		DataSourceTypeCSV:      "CSV file",
		DataSourceTypeParquet:  "Parquet file",
		DataSourceTypeExcel:    "Excel file",
		DataSourceTypeJSON:     "JSON file",
		DataSourceTypeInline:   "Inline data",
		DataSourceTypeNone:     "None",
	}

	for dsType, want := range tests {
		t.Run(want, func(t *testing.T) {
			got := dsType.String()
			if got != want {
				t.Errorf("DataSourceType(%d).String() = %q, want %q", dsType, got, want)
			}
		})
	}
}

func TestDataSourceTypeTypeString(t *testing.T) {
	tests := map[DataSourceType]string{
		DataSourceTypePostgres: "postgres_query",
		DataSourceTypeMySQL:    "mysql_query",
		DataSourceTypeCSV:      "csv",
		DataSourceTypeParquet:  "parquet",
		DataSourceTypeExcel:    "excel",
		DataSourceTypeJSON:     "json",
		DataSourceTypeInline:   "inline",
		DataSourceTypeNone:     "",
	}

	for dsType, want := range tests {
		t.Run(want, func(t *testing.T) {
			got := dsType.TypeString()
			if got != want {
				t.Errorf("DataSourceType(%d).TypeString() = %q, want %q", dsType, got, want)
			}
		})
	}
}

func TestFilterByKind(t *testing.T) {
	manifests := []ManifestInfo{
		{Name: "ds1", Kind: "DataSource"},
		{Name: "ds2", Kind: "DataSource"},
		{Name: "set1", Kind: "DataSet"},
		{Name: "page1", Kind: "LayoutPage"},
	}

	t.Run("filter DataSource", func(t *testing.T) {
		got := FilterByKind(manifests, "DataSource")
		if len(got) != 2 {
			t.Errorf("expected 2 DataSources, got %d", len(got))
		}
	})

	t.Run("filter DataSet and DataSource", func(t *testing.T) {
		got := FilterByKind(manifests, "DataSet", "DataSource")
		if len(got) != 3 {
			t.Errorf("expected 3 results, got %d", len(got))
		}
	})

	t.Run("filter none returns all", func(t *testing.T) {
		got := FilterByKind(manifests)
		if len(got) != len(manifests) {
			t.Errorf("expected all manifests, got %d", len(got))
		}
	})
}

func TestIsNameUnique(t *testing.T) {
	manifests := []ManifestInfo{
		{Name: "sales", Kind: "DataSource"},
		{Name: "sales", Kind: "DataSet"},
	}

	t.Run("unique in kind", func(t *testing.T) {
		if !IsNameUnique(manifests, "DataSource", "products") {
			t.Error("expected products to be unique in DataSource")
		}
	})

	t.Run("not unique in kind", func(t *testing.T) {
		if IsNameUnique(manifests, "DataSource", "sales") {
			t.Error("expected sales to not be unique in DataSource")
		}
	})

	t.Run("same name different kind is unique", func(t *testing.T) {
		if !IsNameUnique(manifests, "LayoutPage", "sales") {
			t.Error("expected sales to be unique in LayoutPage")
		}
	})
}

func TestDetectFilePattern(t *testing.T) {
	t.Run("empty manifests", func(t *testing.T) {
		pattern := DetectFilePattern(nil, "DataSet")
		if pattern.Mode != "separate-files" {
			t.Errorf("expected separate-files mode, got %s", pattern.Mode)
		}
		if pattern.Directory != "datasets" {
			t.Errorf("expected datasets directory, got %s", pattern.Directory)
		}
	})

	t.Run("separate files pattern", func(t *testing.T) {
		manifests := []ManifestInfo{
			{File: "/project/datasets/a.yaml", Kind: "DataSet", Position: 1},
			{File: "/project/datasets/b.yaml", Kind: "DataSet", Position: 1},
		}
		pattern := DetectFilePattern(manifests, "DataSet")
		if pattern.Mode != "separate-files" {
			t.Errorf("expected separate-files mode, got %s", pattern.Mode)
		}
	})

	t.Run("multi-document pattern", func(t *testing.T) {
		manifests := []ManifestInfo{
			{File: "/project/data.yaml", Kind: "DataSet", Position: 1},
			{File: "/project/data.yaml", Kind: "DataSet", Position: 2},
			{File: "/project/data.yaml", Kind: "DataSet", Position: 3},
		}
		pattern := DetectFilePattern(manifests, "DataSet")
		if pattern.Mode != "multi-document" {
			t.Errorf("expected multi-document mode, got %s", pattern.Mode)
		}
		if pattern.File != "/project/data.yaml" {
			t.Errorf("expected data.yaml file, got %s", pattern.File)
		}
	})
}

func TestRenderDataSetManifest(t *testing.T) {
	t.Run("basic SQL query", func(t *testing.T) {
		data := DataSetManifestData{
			Name:  "test_dataset",
			Query: "SELECT * FROM table",
		}
		got := RenderDataSetManifest(data)

		if !contains(got, "kind: DataSet") {
			t.Error("expected 'kind: DataSet' in output")
		}
		if !contains(got, "name: test_dataset") {
			t.Error("expected 'name: test_dataset' in output")
		}
		if !contains(got, "query: |") {
			t.Error("expected 'query: |' in output")
		}
	})

	t.Run("with description and deps", func(t *testing.T) {
		data := DataSetManifestData{
			Name:         "test_dataset",
			Description:  "Test description",
			Dependencies: []string{"source1", "source2"},
			Query:        "SELECT *",
		}
		got := RenderDataSetManifest(data)

		if !contains(got, "description:") {
			t.Error("expected description in output")
		}
		if !contains(got, "dependencies:") {
			t.Error("expected dependencies in output")
		}
	})

	t.Run("with file reference", func(t *testing.T) {
		data := DataSetManifestData{
			Name:      "test_dataset",
			QueryFile: "queries/test.sql",
		}
		got := RenderDataSetManifest(data)

		if !contains(got, "$file(queries/test.sql)") {
			t.Error("expected $file reference in output")
		}
	})

	t.Run("pass-through source", func(t *testing.T) {
		data := DataSetManifestData{
			Name:   "test_dataset",
			Source: "my_source",
		}
		got := RenderDataSetManifest(data)

		if !contains(got, "source: $my_source") {
			t.Error("expected source reference in output")
		}
	})
}

func TestRenderDataSourceManifest(t *testing.T) {
	t.Run("CSV file", func(t *testing.T) {
		data := DataSourceManifestData{
			Name: "test_csv",
			Type: DataSourceTypeCSV,
			Path: "data/test.csv",
		}
		got := RenderDataSourceManifest(data)

		if !contains(got, "kind: DataSource") {
			t.Error("expected 'kind: DataSource' in output")
		}
		if !contains(got, "type: csv") {
			t.Error("expected 'type: csv' in output")
		}
		if !contains(got, "path: data/test.csv") {
			t.Error("expected path in output")
		}
	})

	t.Run("PostgreSQL with structured connection", func(t *testing.T) {
		data := DataSourceManifestData{
			Name:       "test_db",
			Type:       DataSourceTypePostgres,
			DBHost:     "localhost",
			DBPort:     5432,
			DBDatabase: "analytics",
			DBUser:     "reporting",
			DBSecret:   "postgresCredentials",
			DBQuery:    "SELECT * FROM sales",
		}
		got := RenderDataSourceManifest(data)

		if !contains(got, "type: postgres_query") {
			t.Error("expected 'type: postgres_query' in output")
		}
		if !contains(got, "connection:") {
			t.Error("expected connection block in output")
		}
		if !contains(got, "host: localhost") {
			t.Error("expected host in output")
		}
		if !contains(got, "port: 5432") {
			t.Error("expected port in output")
		}
		if !contains(got, "database: analytics") {
			t.Error("expected database in output")
		}
		if !contains(got, "user: reporting") {
			t.Error("expected user in output")
		}
		if !contains(got, "secret: postgresCredentials") {
			t.Error("expected secret in output")
		}
		if !contains(got, "query: |") {
			t.Error("expected query block in output")
		}
	})

	t.Run("CSV with options", func(t *testing.T) {
		csvHeader := false
		data := DataSourceManifestData{
			Name:         "test_csv",
			Type:         DataSourceTypeCSV,
			Path:         "data/test.csv",
			CSVDelimiter: ";",
			CSVHeader:    &csvHeader,
			CSVSkipRows:  2,
		}
		got := RenderDataSourceManifest(data)

		if !contains(got, "delimiter:") {
			t.Error("expected delimiter in output")
		}
		if !contains(got, "header: false") {
			t.Error("expected header: false in output")
		}
		if !contains(got, "skipRows: 2") {
			t.Error("expected skipRows in output")
		}
	})
}

func TestWriteManifest(t *testing.T) {
	tmp := t.TempDir()

	t.Run("creates new file", func(t *testing.T) {
		path := filepath.Join(tmp, "new.yaml")
		content := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\n"

		err := WriteManifest(path, content)
		if err != nil {
			t.Fatalf("WriteManifest failed: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if string(got) != content {
			t.Errorf("content mismatch: got %q, want %q", got, content)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		path := filepath.Join(tmp, "sub", "dir", "new.yaml")
		content := "test content"

		err := WriteManifest(path, content)
		if err != nil {
			t.Fatalf("WriteManifest failed: %v", err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Errorf("file not created: %v", err)
		}
	})

	t.Run("fails if file exists", func(t *testing.T) {
		path := filepath.Join(tmp, "existing.yaml")
		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatalf("failed to create existing file: %v", err)
		}

		err := WriteManifest(path, "new content")
		if err == nil {
			t.Error("expected error for existing file")
		}
	})
}

func TestAppendToManifest(t *testing.T) {
	tmp := t.TempDir()

	t.Run("appends to existing file", func(t *testing.T) {
		path := filepath.Join(tmp, "multi.yaml")
		initial := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: first\n"
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatalf("failed to create initial file: %v", err)
		}

		addition := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: second\n"
		err := AppendToManifest(path, addition)
		if err != nil {
			t.Fatalf("AppendToManifest failed: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		if !contains(string(got), "---") {
			t.Error("expected document separator in output")
		}
		if !contains(string(got), "name: first") {
			t.Error("expected first document in output")
		}
		if !contains(string(got), "name: second") {
			t.Error("expected second document in output")
		}
	})
}

func TestScanManifests(t *testing.T) {
	tmp := t.TempDir()

	// Create a valid manifest file
	manifest := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  query: "SELECT 1"
`
	if err := os.WriteFile(filepath.Join(tmp, "test.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	ctx := context.Background()
	manifests, err := ScanManifests(ctx, tmp)
	if err != nil {
		t.Fatalf("ScanManifests failed: %v", err)
	}

	if len(manifests) != 1 {
		t.Errorf("expected 1 manifest, got %d", len(manifests))
	}

	if len(manifests) > 0 && manifests[0].Name != "test_dataset" {
		t.Errorf("expected name 'test_dataset', got %q", manifests[0].Name)
	}
}

func TestAddConfig(t *testing.T) {
	tmp := t.TempDir()

	t.Run("loads empty config from new directory", func(t *testing.T) {
		cfg, err := LoadAddConfig(tmp)
		if err != nil {
			t.Fatalf("LoadAddConfig failed: %v", err)
		}
		if cfg.Dataset != nil {
			t.Error("expected nil Dataset config")
		}
	})

	t.Run("saves and loads config", func(t *testing.T) {
		cfg := &AddConfig{
			Dataset: &KindConfig{
				Mode:      "separate-files",
				Directory: "datasets",
			},
		}

		if err := SaveAddConfig(tmp, cfg); err != nil {
			t.Fatalf("SaveAddConfig failed: %v", err)
		}

		loaded, err := LoadAddConfig(tmp)
		if err != nil {
			t.Fatalf("LoadAddConfig failed: %v", err)
		}

		if loaded.Dataset == nil {
			t.Fatal("expected non-nil Dataset config")
		}
		if loaded.Dataset.Mode != "separate-files" {
			t.Errorf("expected mode 'separate-files', got %q", loaded.Dataset.Mode)
		}
	})
}

func TestQuoteYAMLIfNeeded(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"no quoting needed":     {input: "simple", want: "simple"},
		"colon needs quoting":   {input: "key: value", want: `"key: value"`},
		"hash needs quoting":    {input: "# comment", want: `"# comment"`},
		"leading space":         {input: " leading", want: `" leading"`},
		"trailing space":        {input: "trailing ", want: `"trailing "`},
		"brackets":              {input: "[value]", want: `"[value]"`},
		"quotes in value":       {input: `say "hello"`, want: `"say \"hello\""`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := quoteYAMLIfNeeded(tc.input)
			if got != tc.want {
				t.Errorf("quoteYAMLIfNeeded(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
