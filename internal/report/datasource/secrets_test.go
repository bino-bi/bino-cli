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
			name: "http with proxy configuration",
			raw: `{
				"spec": {
					"type": "http",
					"http": {
						"httpProxy": "http://proxy:8080",
						"httpProxyUsername": "user",
						"httpProxyPasswordFromEnv": "PROXY_PASS"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.HTTP == nil {
					t.Fatal("expected http auth spec")
				}
				if spec.HTTP.HTTPProxy != "http://proxy:8080" {
					t.Errorf("unexpected httpProxy: %s", spec.HTTP.HTTPProxy)
				}
				if spec.HTTP.HTTPProxyUsername != "user" {
					t.Errorf("unexpected httpProxyUsername: %s", spec.HTTP.HTTPProxyUsername)
				}
				if spec.HTTP.HTTPProxyPasswordFromEnv != "PROXY_PASS" {
					t.Errorf("unexpected httpProxyPasswordFromEnv: %s", spec.HTTP.HTTPProxyPasswordFromEnv)
				}
			},
		},
		{
			name: "webdav with credentials",
			raw: `{
				"spec": {
					"type": "webdav",
					"scope": "webdav://server.example.com/",
					"webdav": {
						"username": "user123",
						"password": "pass456"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.Type != "webdav" {
					t.Errorf("expected type webdav, got %s", spec.Type)
				}
				if spec.Scope != "webdav://server.example.com/" {
					t.Errorf("expected scope webdav://server.example.com/, got %s", spec.Scope)
				}
				if spec.WebDAV == nil {
					t.Fatal("expected webdav auth spec")
				}
				if spec.WebDAV.Username != "user123" {
					t.Errorf("unexpected username: %s", spec.WebDAV.Username)
				}
				if spec.WebDAV.Password != "pass456" {
					t.Errorf("unexpected password: %s", spec.WebDAV.Password)
				}
			},
		},
		{
			name: "webdav with credentials from env",
			raw: `{
				"spec": {
					"type": "webdav",
					"scope": "storagebox://u123456",
					"webdav": {
						"usernameFromEnv": "WEBDAV_USER",
						"passwordFromEnv": "WEBDAV_PASS"
					}
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, spec SecretSpec) {
				if spec.WebDAV == nil {
					t.Fatal("expected webdav auth spec")
				}
				if spec.WebDAV.UsernameFromEnv != "WEBDAV_USER" {
					t.Errorf("unexpected usernameFromEnv: %s", spec.WebDAV.UsernameFromEnv)
				}
				if spec.WebDAV.PasswordFromEnv != "WEBDAV_PASS" {
					t.Errorf("unexpected passwordFromEnv: %s", spec.WebDAV.PasswordFromEnv)
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
			name:     "http with proxy configuration",
			specName: "test-http-proxy",
			spec: SecretSpec{
				Type: "http",
				HTTP: &HTTPAuthSpec{
					HTTPProxy:         "http://proxy.example.com:8080",
					HTTPProxyUsername: "proxyuser",
					HTTPProxyPassword: "proxypass",
				},
			},
			wantErr: false,
			contains: []string{
				"TYPE http",
				"HTTP_PROXY 'http://proxy.example.com:8080'",
				"HTTP_PROXY_USERNAME 'proxyuser'",
				"HTTP_PROXY_PASSWORD 'proxypass'",
			},
		},
		{
			name:     "http with proxy and bearer token",
			specName: "test-http-proxy-bearer",
			spec: SecretSpec{
				Type:  "http",
				Scope: "https://api.example.com",
				HTTP: &HTTPAuthSpec{
					BearerToken: "token123",
					HTTPProxy:   "http://proxy:3128",
				},
			},
			wantErr: false,
			contains: []string{
				"TYPE http",
				"SCOPE 'https://api.example.com'",
				"BEARER_TOKEN 'token123'",
				"HTTP_PROXY 'http://proxy:3128'",
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
			name:     "webdav with full config",
			specName: "test-webdav",
			spec: SecretSpec{
				Type:  "webdav",
				Scope: "webdav://server.example.com/",
				WebDAV: &WebDAVAuthSpec{
					Username: "user123",
					Password: "pass456",
				},
			},
			wantErr: false,
			contains: []string{
				`CREATE SECRET "test-webdav"`,
				"TYPE webdav",
				"SCOPE 'webdav://server.example.com/'",
				"USERNAME 'user123'",
				"PASSWORD 'pass456'",
			},
		},
		{
			name:     "webdav for hetzner storagebox",
			specName: "my-storagebox",
			spec: SecretSpec{
				Type:  "webdav",
				Scope: "storagebox://u123456",
				WebDAV: &WebDAVAuthSpec{
					Username: "u123456",
					Password: "secret",
				},
			},
			wantErr: false,
			contains: []string{
				`CREATE SECRET "my-storagebox"`,
				"TYPE webdav",
				"SCOPE 'storagebox://u123456'",
				"USERNAME 'u123456'",
				"PASSWORD 'secret'",
			},
		},
		{
			name:     "webdav missing auth spec",
			specName: "test-webdav-missing",
			spec: SecretSpec{
				Type: "webdav",
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

func TestExtensionForSecretType(t *testing.T) {
	tests := []struct {
		secretType string
		want       string
	}{
		{"s3", "httpfs"},
		{"gcs", "httpfs"},
		{"http", "httpfs"},
		{"r2", "httpfs"},
		{"huggingface", "httpfs"},
		{"azure", "azure"},
		{"postgres", "postgres"},
		{"mysql", "mysql"},
		{"webdav", "webdavfs"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.secretType, func(t *testing.T) {
			got := extensionForSecretType(tt.secretType)
			if got != tt.want {
				t.Errorf("extensionForSecretType(%q) = %q, want %q", tt.secretType, got, tt.want)
			}
		})
	}
}

func TestIsCommunityExtension(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"webdavfs", true},
		{"prql", true},
		{"httpfs", false},
		{"postgres", false},
		{"mysql", false},
		{"azure", false},
		{"excel", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCommunityExtension(tt.name)
			if got != tt.want {
				t.Errorf("IsCommunityExtension(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
