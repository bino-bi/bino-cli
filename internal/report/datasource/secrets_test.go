package datasource

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseSecretSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		check   func(t *testing.T, spec SecretSpec)
	}{
		{
			name: "postgres with inline password",
			raw: `{
				"spec": {
					"type": "postgres",
					"postgres": {
						"password": "secret123"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.Type != "postgres" {
					t.Errorf("expected type postgres, got %s", spec.Type)
				}
				if spec.Postgres == nil {
					t.Fatal("expected postgres auth spec")
				}
				if spec.Postgres.Password != "secret123" {
					t.Errorf("expected password secret123, got %s", spec.Postgres.Password)
				}
			},
		},
		{
			name: "postgres with password from env",
			raw: `{
				"spec": {
					"type": "postgres",
					"postgres": {
						"passwordFromEnv": "DB_PASSWORD"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.Postgres == nil {
					t.Fatal("expected postgres auth spec")
				}
				if spec.Postgres.PasswordFromEnv != "DB_PASSWORD" {
					t.Errorf("expected passwordFromEnv DB_PASSWORD, got %s", spec.Postgres.PasswordFromEnv)
				}
			},
		},
		{
			name: "mysql with inline password",
			raw: `{
				"spec": {
					"type": "mysql",
					"mysql": {
						"password": "mysql123"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.Type != "mysql" {
					t.Errorf("expected type mysql, got %s", spec.Type)
				}
				if spec.MySQL == nil {
					t.Fatal("expected mysql auth spec")
				}
				if spec.MySQL.Password != "mysql123" {
					t.Errorf("expected password mysql123, got %s", spec.MySQL.Password)
				}
			},
		},
		{
			name: "s3 with credentials",
			raw: `{
				"spec": {
					"type": "s3",
					"s3": {
						"keyId": "AKIAIOSFODNN7EXAMPLE",
						"secret": "wJalrXUtnFEMI/K7MDENG",
						"region": "us-east-1"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.Type != "s3" {
					t.Errorf("expected type s3, got %s", spec.Type)
				}
				if spec.S3 == nil {
					t.Fatal("expected s3 auth spec")
				}
				if spec.S3.KeyID != "AKIAIOSFODNN7EXAMPLE" {
					t.Errorf("unexpected keyId: %s", spec.S3.KeyID)
				}
				if spec.S3.Region != "us-east-1" {
					t.Errorf("unexpected region: %s", spec.S3.Region)
				}
			},
		},
		{
			name: "http with bearer token",
			raw: `{
				"spec": {
					"type": "http",
					"http": {
						"bearerToken": "eyJhbGc..."
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.HTTP == nil {
					t.Fatal("expected http auth spec")
				}
				if spec.HTTP.BearerToken != "eyJhbGc..." {
					t.Errorf("unexpected bearerToken: %s", spec.HTTP.BearerToken)
				}
			},
		},
		{
			name: "missing type",
			raw: `{
				"spec": {}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseSecretSpec(json.RawMessage(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSecretSpec() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, spec)
			}
		})
	}
}

func TestBuildCreateSecret(t *testing.T) {
	tests := []struct {
		name     string
		specName string
		spec     SecretSpec
		wantErr  bool
		contains []string
	}{
		{
			name:     "postgres with inline password",
			specName: "test-pg",
			spec: SecretSpec{
				Type: "postgres",
				Postgres: &PostgresAuthSpec{
					Password: "secret123",
				},
			},
			wantErr: false,
			contains: []string{
				`CREATE SECRET "test-pg"`,
				"TYPE postgres",
				"PASSWORD 'secret123'",
			},
		},
		{
			name:     "mysql with inline password",
			specName: "test-mysql",
			spec: SecretSpec{
				Type: "mysql",
				MySQL: &MySQLAuthSpec{
					Password: "mysql123",
				},
			},
			wantErr: false,
			contains: []string{
				`CREATE SECRET "test-mysql"`,
				"TYPE mysql",
				"PASSWORD 'mysql123'",
			},
		},
		{
			name:     "s3 with full config",
			specName: "test-s3",
			spec: SecretSpec{
				Type: "s3",
				S3: &S3AuthSpec{
					KeyID:  "AKIAIOSFODNN7EXAMPLE",
					Secret: "wJalrXUtnFEMI/K7MDENG",
					Region: "us-east-1",
				},
			},
			wantErr: false,
			contains: []string{
				"TYPE s3",
				"KEY_ID 'AKIAIOSFODNN7EXAMPLE'",
				"SECRET 'wJalrXUtnFEMI/K7MDENG'",
				"REGION 'us-east-1'",
			},
		},
		{
			name:     "http with bearer token",
			specName: "test-http",
			spec: SecretSpec{
				Type: "http",
				HTTP: &HTTPAuthSpec{
					BearerToken: "eyJhbGc...",
				},
			},
			wantErr: false,
			contains: []string{
				"TYPE http",
				"BEARER_TOKEN 'eyJhbGc...'",
			},
		},
		{
			name:     "postgres missing auth spec",
			specName: "test-missing",
			spec: SecretSpec{
				Type: "postgres",
			},
			wantErr: true,
		},
		{
			name:     "unsupported type",
			specName: "test-unknown",
			spec: SecretSpec{
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := buildCreateSecret(tt.specName, tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildCreateSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, substr := range tt.contains {
					if !containsSubstring(stmt, substr) {
						t.Errorf("buildCreateSecret() statement missing %q\nGot: %s", substr, stmt)
					}
				}
			}
		})
	}
}

func TestResolveCredential(t *testing.T) {
	// Set a test env var
	os.Setenv("TEST_CREDENTIAL", "env_value")
	defer os.Unsetenv("TEST_CREDENTIAL")

	tests := []struct {
		name    string
		inline  string
		envName string
		want    string
		wantErr bool
	}{
		{
			name:    "inline value",
			inline:  "inline_value",
			envName: "",
			want:    "inline_value",
			wantErr: false,
		},
		{
			name:    "from env",
			inline:  "",
			envName: "TEST_CREDENTIAL",
			want:    "env_value",
			wantErr: false,
		},
		{
			name:    "inline takes precedence",
			inline:  "inline_value",
			envName: "TEST_CREDENTIAL",
			want:    "inline_value",
			wantErr: false,
		},
		{
			name:    "missing env var",
			inline:  "",
			envName: "NONEXISTENT_VAR",
			want:    "",
			wantErr: true,
		},
		{
			name:    "both empty",
			inline:  "",
			envName: "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCredential(tt.inline, tt.envName)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveCredential() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("resolveCredential() = %v, want %v", got, tt.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
