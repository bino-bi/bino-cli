package schema

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDocumentRoundTrip tests that all document kinds can be marshaled and unmarshaled.
func TestDocumentRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		doc  *Document
	}{
		{
			name: "DataSet with inline query",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindDataSet,
				Metadata: Metadata{
					Name:        "sales_summary",
					Description: "Aggregated sales data",
					Constraints: ConstraintListFromStrings([]string{"env == production", "region != test"}),
				},
				Spec: &DataSetSpec{
					Query:        &QueryField{Inline: "SELECT * FROM sales"},
					Dependencies: []DataSourceRef{{Ref: "sales_db"}},
				},
			},
		},
		{
			name: "DataSet with file query",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindDataSet,
				Metadata: Metadata{
					Name: "users_query",
				},
				Spec: &DataSetSpec{
					Query: &QueryField{File: "queries/users.sql"},
				},
			},
		},
		{
			name: "DataSource CSV",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindDataSource,
				Metadata: Metadata{
					Name:        "sales_csv",
					Description: "Sales data from CSV",
				},
				Spec: &DataSourceSpec{
					Type: DataSourceTypeCSV,
					Path: "data/sales.csv",
				},
			},
		},
		{
			name: "DataSource Postgres",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindDataSource,
				Metadata: Metadata{
					Name: "postgres_source",
				},
				Spec: &DataSourceSpec{
					Type: DataSourceTypePostgresQuery,
					Connection: &ConnectionSpec{
						Host:     "localhost",
						Port:     5432,
						Database: "mydb",
						Schema:   "public",
						User:     "admin",
						Secret:   "$db_secret",
					},
					Query: "SELECT * FROM orders",
				},
			},
		},
		{
			name: "ConnectionSecret Postgres",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindConnectionSecret,
				Metadata: Metadata{
					Name: "db_secret",
				},
				Spec: &ConnectionSecretSpec{
					Type:            ConnectionSecretTypePostgres,
					PasswordFromEnv: "DB_PASSWORD",
				},
			},
		},
		{
			name: "ConnectionSecret S3",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindConnectionSecret,
				Metadata: Metadata{
					Name: "s3_secret",
				},
				Spec: &ConnectionSecretSpec{
					Type:          ConnectionSecretTypeS3,
					KeyID:         "AKIAIOSFODNN7EXAMPLE",
					SecretFromEnv: "AWS_SECRET_KEY",
				},
			},
		},
		{
			name: "LayoutPage",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindLayoutPage,
				Metadata: Metadata{
					Name:        "main_page",
					Description: "Main report page",
				},
				Spec: &LayoutPageSpec{
					Children: []string{"$header", "$content", "$footer"},
				},
			},
		},
		{
			name: "LayoutCard",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindLayoutCard,
				Metadata: Metadata{
					Name: "summary_card",
				},
				Spec: &LayoutCardSpec{
					Title:    "Summary",
					Children: []string{"$chart1", "$table1"},
				},
			},
		},
		{
			name: "Text with value",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindText,
				Metadata: Metadata{
					Name: "welcome_text",
				},
				Spec: &TextSpec{
					Value: "Welcome to the report",
				},
			},
		},
		{
			name: "Text with dataset",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindText,
				Metadata: Metadata{
					Name: "dynamic_text",
				},
				Spec: &TextSpec{
					Dataset: "$summary_data",
				},
			},
		},
		{
			name: "Table",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindTable,
				Metadata: Metadata{
					Name: "sales_table",
				},
				Spec: &TableSpec{
					Dataset:    "$sales_data",
					TableTitle: "Sales Overview",
				},
			},
		},
		{
			name: "ChartStructure",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindChartStructure,
				Metadata: Metadata{
					Name: "pie_chart",
				},
				Spec: &ChartStructureSpec{
					Dataset:    "$category_breakdown",
					ChartTitle: "Category Distribution",
					Type:       "pie",
				},
			},
		},
		{
			name: "ChartTime",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindChartTime,
				Metadata: Metadata{
					Name: "trend_chart",
				},
				Spec: &ChartTimeSpec{
					Dataset:    "$monthly_sales",
					ChartTitle: "Sales Trend",
				},
			},
		},
		{
			name: "Asset image",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindAsset,
				Metadata: Metadata{
					Name: "company_logo",
				},
				Spec: &AssetSpec{
					Type:      AssetTypeImage,
					MediaType: "image/png",
					Source: &AssetSource{
						LocalPath: "assets/logo.png",
					},
				},
			},
		},
		{
			name: "Asset font",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindAsset,
				Metadata: Metadata{
					Name: "custom_font",
				},
				Spec: &AssetSpec{
					Type:      AssetTypeFont,
					MediaType: "font/ttf",
					Source: &AssetSource{
						RemoteURL: "https://fonts.example.com/roboto.ttf",
					},
				},
			},
		},
		{
			name: "ComponentStyle",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindComponentStyle,
				Metadata: Metadata{
					Name: "table_style",
				},
				Spec: &ComponentStyleSpec{
					Content: map[string]any{
						"border": "1px solid black",
						"color":  "blue",
					},
				},
			},
		},
		{
			name: "Internationalization",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindInternationalization,
				Metadata: Metadata{
					Name: "en_translations",
				},
				Spec: &InternationalizationSpec{
					Code: "en",
					Content: map[string]string{
						"greeting": "Hello",
						"farewell": "Goodbye",
					},
				},
			},
		},
		{
			name: "ReportArtefact",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindReportArtefact,
				Metadata: Metadata{
					Name:        "monthly_report",
					Description: "Monthly sales report PDF",
				},
				Spec: &ReportArtefactSpec{
					Filename:    "report_{{date}}.pdf",
					LayoutPages: []string{"$main_page"},
					Format:      "pdf",
				},
			},
		},
		{
			name: "LiveReportArtefact",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindLiveReportArtefact,
				Metadata: Metadata{
					Name: "live_dashboard",
				},
				Spec: &LiveReportArtefactSpec{
					Title: "Dashboard",
					Routes: map[string]LiveRouteSpec{
						"/": {
							LayoutPages: []string{"$dashboard_page"},
						},
					},
				},
			},
		},
		{
			name: "SigningProfile",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindSigningProfile,
				Metadata: Metadata{
					Name: "pdf_signer",
				},
				Spec: &SigningProfileSpec{
					Certificate: &FileRef{LocalPath: "certs/signing.pem"},
					PrivateKey:  &FileRef{LocalPath: "certs/signing.key"},
					SignerName:  "Report Signer",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to YAML
			data, err := yaml.Marshal(tt.doc)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			// Unmarshal back
			var parsed Document
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Unmarshal error: %v\nYAML:\n%s", err, string(data))
			}

			// Verify core fields survive round-trip
			if parsed.APIVersion != tt.doc.APIVersion {
				t.Errorf("APIVersion mismatch: got %q, want %q", parsed.APIVersion, tt.doc.APIVersion)
			}
			if parsed.Kind != tt.doc.Kind {
				t.Errorf("Kind mismatch: got %q, want %q", parsed.Kind, tt.doc.Kind)
			}
			if parsed.Metadata.Name != tt.doc.Metadata.Name {
				t.Errorf("Name mismatch: got %q, want %q", parsed.Metadata.Name, tt.doc.Metadata.Name)
			}
			if parsed.Metadata.Description != tt.doc.Metadata.Description {
				t.Errorf("Description mismatch: got %q, want %q", parsed.Metadata.Description, tt.doc.Metadata.Description)
			}
			if len(parsed.Metadata.Constraints) != len(tt.doc.Metadata.Constraints) {
				t.Errorf("Constraints length mismatch: got %d, want %d", len(parsed.Metadata.Constraints), len(tt.doc.Metadata.Constraints))
			}
		})
	}
}

// TestDocumentRoundTripYAML verifies YAML output matches expected format.
func TestDocumentRoundTripYAML(t *testing.T) {
	tests := []struct {
		name     string
		doc      *Document
		contains []string
	}{
		{
			name: "DataSet has correct structure",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindDataSet,
				Metadata:   Metadata{Name: "test"},
				Spec:       &DataSetSpec{Query: &QueryField{Inline: "SELECT 1"}},
			},
			contains: []string{
				"apiVersion: bino.bi/v1alpha1",
				"kind: DataSet",
				"name: test",
				"query: SELECT 1",
			},
		},
		{
			name: "LayoutPage has children array",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindLayoutPage,
				Metadata:   Metadata{Name: "page1"},
				Spec:       &LayoutPageSpec{Children: []string{"$a", "$b"}},
			},
			contains: []string{
				"kind: LayoutPage",
				"children:",
				"- $a",
				"- $b",
			},
		},
		{
			name: "Table references dataset",
			doc: &Document{
				APIVersion: APIVersion,
				Kind:       KindTable,
				Metadata:   Metadata{Name: "my_table"},
				Spec:       &TableSpec{Dataset: "$my_data", TableTitle: "Data Table"},
			},
			contains: []string{
				"kind: Table",
				"dataset: $my_data",
				"tableTitle: Data Table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.doc)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			yamlStr := string(data)
			for _, expected := range tt.contains {
				if !containsSubstring(yamlStr, expected) {
					t.Errorf("expected YAML to contain %q, got:\n%s", expected, yamlStr)
				}
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
