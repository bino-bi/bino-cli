package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"bino.bi/bino/internal/report/config"
)

// SecretSpec represents the parsed specification for a ConnectionSecret manifest.
// Secrets are created as temporary (in-memory only) via CREATE SECRET.
//
// Supported secret types (from DuckDB extensions):
//   - s3: AWS S3 (requires httpfs extension)
//   - gcs: Google Cloud Storage (requires httpfs extension)
//   - http: HTTP/HTTPS authentication (requires httpfs extension)
//   - r2: Cloudflare R2 (requires httpfs extension)
//   - azure: Azure Blob Storage (requires azure extension)
//   - postgres: PostgreSQL credentials only (requires postgres extension)
//   - mysql: MySQL credentials only (requires mysql extension)
//
// Database secrets (postgres, mysql) contain only credentials.
// Connection details (host, port, database, user) are defined in the DataSource connection block.
type SecretSpec struct {
	// Type is the DuckDB secret type (s3, gcs, http, postgres, mysql, etc.)
	Type string `json:"type"`

	// Scope is an optional file path prefix that the secret applies to.
	// For example, "s3://my-bucket" to scope to a specific bucket.
	Scope string `json:"scope"`

	// Provider is the secret provider (defaults to "config").
	// Use "credential_chain" for automatic credential discovery (S3, GCS, Azure).
	Provider string `json:"provider"`

	// Backend-specific credential configurations (mutually exclusive based on Type)
	Postgres    *PostgresAuthSpec    `json:"postgres,omitempty"`
	MySQL       *MySQLAuthSpec       `json:"mysql,omitempty"`
	S3          *S3AuthSpec          `json:"s3,omitempty"`
	GCS         *GCSAuthSpec         `json:"gcs,omitempty"`
	HTTP        *HTTPAuthSpec        `json:"http,omitempty"`
	R2          *R2AuthSpec          `json:"r2,omitempty"`
	Azure       *AzureAuthSpec       `json:"azure,omitempty"`
	Huggingface *HuggingfaceAuthSpec `json:"huggingface,omitempty"`
}

// PostgresAuthSpec holds PostgreSQL credentials.
// Connection details (host, port, database, user) are defined in the DataSource.
type PostgresAuthSpec struct {
	Password        string `json:"password,omitempty"`
	PasswordFromEnv string `json:"passwordFromEnv,omitempty"`
}

// MySQLAuthSpec holds MySQL credentials.
// Connection details (host, port, database, user) are defined in the DataSource.
type MySQLAuthSpec struct {
	Password        string `json:"password,omitempty"`
	PasswordFromEnv string `json:"passwordFromEnv,omitempty"`
}

// S3AuthSpec holds AWS S3 credentials and configuration.
type S3AuthSpec struct {
	KeyID               string `json:"keyId,omitempty"`
	KeyIDFromEnv        string `json:"keyIdFromEnv,omitempty"`
	Secret              string `json:"secret,omitempty"`
	SecretFromEnv       string `json:"secretFromEnv,omitempty"`
	Region              string `json:"region,omitempty"`
	SessionToken        string `json:"sessionToken,omitempty"`
	SessionTokenFromEnv string `json:"sessionTokenFromEnv,omitempty"`
	Endpoint            string `json:"endpoint,omitempty"`
	URLStyle            string `json:"urlStyle,omitempty"`
}

// GCSAuthSpec holds Google Cloud Storage credentials.
type GCSAuthSpec struct {
	KeyID         string `json:"keyId,omitempty"`
	KeyIDFromEnv  string `json:"keyIdFromEnv,omitempty"`
	Secret        string `json:"secret,omitempty"`
	SecretFromEnv string `json:"secretFromEnv,omitempty"`
}

// HTTPAuthSpec holds HTTP/HTTPS authentication credentials and proxy configuration.
type HTTPAuthSpec struct {
	Username           string `json:"username,omitempty"`
	UsernameFromEnv    string `json:"usernameFromEnv,omitempty"`
	Password           string `json:"password,omitempty"`
	PasswordFromEnv    string `json:"passwordFromEnv,omitempty"`
	BearerToken        string `json:"bearerToken,omitempty"`
	BearerTokenFromEnv string `json:"bearerTokenFromEnv,omitempty"`
	// HTTP proxy configuration (only supported for TYPE http secrets)
	HTTPProxy                string `json:"httpProxy,omitempty"`
	HTTPProxyFromEnv         string `json:"httpProxyFromEnv,omitempty"`
	HTTPProxyUsername        string `json:"httpProxyUsername,omitempty"`
	HTTPProxyUsernameFromEnv string `json:"httpProxyUsernameFromEnv,omitempty"`
	HTTPProxyPassword        string `json:"httpProxyPassword,omitempty"`
	HTTPProxyPasswordFromEnv string `json:"httpProxyPasswordFromEnv,omitempty"`
}

// R2AuthSpec holds Cloudflare R2 credentials and configuration.
type R2AuthSpec struct {
	KeyID         string `json:"keyId,omitempty"`
	KeyIDFromEnv  string `json:"keyIdFromEnv,omitempty"`
	Secret        string `json:"secret,omitempty"`
	SecretFromEnv string `json:"secretFromEnv,omitempty"`
	AccountID     string `json:"accountId,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
}

// AzureAuthSpec holds Azure Blob Storage credentials.
type AzureAuthSpec struct {
	ConnectionString        string `json:"connectionString,omitempty"`
	ConnectionStringFromEnv string `json:"connectionStringFromEnv,omitempty"`
	AccountName             string `json:"accountName,omitempty"`
	AccountKey              string `json:"accountKey,omitempty"`
	AccountKeyFromEnv       string `json:"accountKeyFromEnv,omitempty"`
}

// HuggingfaceAuthSpec holds Hugging Face credentials.
type HuggingfaceAuthSpec struct {
	Token        string `json:"token,omitempty"`
	TokenFromEnv string `json:"tokenFromEnv,omitempty"`
}

// LoadSecrets creates in-memory DuckDB secrets from ConnectionSecret manifests.
// Secrets are created as temporary (never persistent) and exist only for
// the lifetime of the DuckDB session.
//
// Returns the list of required extensions and any errors encountered.
func LoadSecrets(ctx context.Context, db *sql.DB, docs []config.Document) ([]string, []Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	var (
		extensions = make(map[string]struct{})
		diags      []Diagnostic
	)

	for _, doc := range docs {
		if doc.Kind != "ConnectionSecret" {
			continue
		}

		spec, err := parseSecretSpec(doc.Raw)
		if err != nil {
			diags = append(diags, diagnostic(doc.Name, "spec", err))
			continue
		}

		// Track required extension
		if ext := extensionForSecretType(spec.Type); ext != "" {
			extensions[ext] = struct{}{}
		}

		// Build and execute CREATE SECRET statement
		stmt, err := buildCreateSecret(doc.Name, spec)
		if err != nil {
			diags = append(diags, diagnostic(doc.Name, "build", err))
			continue
		}

		if _, err := db.ExecContext(ctx, stmt); err != nil {
			diags = append(diags, diagnostic(doc.Name, "execute", err))
			continue
		}
	}

	result := make([]string, 0, len(extensions))
	for ext := range extensions {
		result = append(result, ext)
	}

	return result, diags, nil
}

func parseSecretSpec(raw json.RawMessage) (SecretSpec, error) {
	var payload struct {
		Spec SecretSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return SecretSpec{}, fmt.Errorf("decode spec: %w", err)
	}
	payload.Spec.Type = strings.ToLower(strings.TrimSpace(payload.Spec.Type))
	if payload.Spec.Type == "" {
		return SecretSpec{}, fmt.Errorf("spec.type is required")
	}
	return payload.Spec, nil
}

// buildCreateSecret generates a CREATE SECRET statement.
// Secrets are always temporary (in-memory only).
func buildCreateSecret(name string, spec SecretSpec) (string, error) {
	var parts []string

	// TYPE is required
	parts = append(parts, fmt.Sprintf("TYPE %s", spec.Type))

	// Optional provider
	if spec.Provider != "" {
		parts = append(parts, fmt.Sprintf("PROVIDER %s", spec.Provider))
	}

	// Optional scope
	if spec.Scope != "" {
		parts = append(parts, fmt.Sprintf("SCOPE '%s'", escapeSQLString(spec.Scope)))
	}

	// Add backend-specific parameters based on secret type
	var err error
	switch strings.ToLower(spec.Type) {
	case "postgres":
		err = addPostgresParams(&parts, spec.Postgres)
	case "mysql":
		err = addMySQLParams(&parts, spec.MySQL)
	case "s3":
		err = addS3Params(&parts, spec.S3)
	case "gcs":
		err = addGCSParams(&parts, spec.GCS)
	case "http":
		err = addHTTPParams(&parts, spec.HTTP)
	case "r2":
		err = addR2Params(&parts, spec.R2)
	case "azure":
		err = addAzureParams(&parts, spec.Azure)
	case "huggingface":
		err = addHuggingfaceParams(&parts, spec.Huggingface)
	default:
		return "", fmt.Errorf("unsupported secret type: %s", spec.Type)
	}

	if err != nil {
		return "", err
	}

	// Quote the secret name to handle names with special characters like hyphens
	return fmt.Sprintf("CREATE SECRET \"%s\" (\n    %s\n)", name, strings.Join(parts, ",\n    ")), nil
}

func addPostgresParams(parts *[]string, auth *PostgresAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("postgres secret requires postgres credentials")
	}

	password, err := resolveCredential(auth.Password, auth.PasswordFromEnv)
	if err != nil {
		return fmt.Errorf("password: %w", err)
	}
	*parts = append(*parts, fmt.Sprintf("PASSWORD '%s'", escapeSQLString(password)))

	return nil
}

func addMySQLParams(parts *[]string, auth *MySQLAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("mysql secret requires mysql credentials")
	}

	password, err := resolveCredential(auth.Password, auth.PasswordFromEnv)
	if err != nil {
		return fmt.Errorf("password: %w", err)
	}
	*parts = append(*parts, fmt.Sprintf("PASSWORD '%s'", escapeSQLString(password)))

	return nil
}

func addS3Params(parts *[]string, auth *S3AuthSpec) error {
	if auth == nil {
		return fmt.Errorf("s3 secret requires s3 credentials")
	}

	if keyID, err := resolveCredential(auth.KeyID, auth.KeyIDFromEnv); err == nil && keyID != "" {
		*parts = append(*parts, fmt.Sprintf("KEY_ID '%s'", escapeSQLString(keyID)))
	}

	if secret, err := resolveCredential(auth.Secret, auth.SecretFromEnv); err == nil && secret != "" {
		*parts = append(*parts, fmt.Sprintf("SECRET '%s'", escapeSQLString(secret)))
	}

	if auth.Region != "" {
		*parts = append(*parts, fmt.Sprintf("REGION '%s'", escapeSQLString(auth.Region)))
	}

	if token, err := resolveCredential(auth.SessionToken, auth.SessionTokenFromEnv); err == nil && token != "" {
		*parts = append(*parts, fmt.Sprintf("SESSION_TOKEN '%s'", escapeSQLString(token)))
	}

	if auth.Endpoint != "" {
		*parts = append(*parts, fmt.Sprintf("ENDPOINT '%s'", escapeSQLString(auth.Endpoint)))
	}

	if auth.URLStyle != "" {
		*parts = append(*parts, fmt.Sprintf("URL_STYLE '%s'", escapeSQLString(auth.URLStyle)))
	}

	return nil
}

func addGCSParams(parts *[]string, auth *GCSAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("gcs secret requires gcs credentials")
	}

	if keyID, err := resolveCredential(auth.KeyID, auth.KeyIDFromEnv); err == nil && keyID != "" {
		*parts = append(*parts, fmt.Sprintf("KEY_ID '%s'", escapeSQLString(keyID)))
	}

	if secret, err := resolveCredential(auth.Secret, auth.SecretFromEnv); err == nil && secret != "" {
		*parts = append(*parts, fmt.Sprintf("SECRET '%s'", escapeSQLString(secret)))
	}

	return nil
}

func addHTTPParams(parts *[]string, auth *HTTPAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("http secret requires http credentials")
	}

	if username, err := resolveCredential(auth.Username, auth.UsernameFromEnv); err == nil && username != "" {
		*parts = append(*parts, fmt.Sprintf("USERNAME '%s'", escapeSQLString(username)))
	}

	if password, err := resolveCredential(auth.Password, auth.PasswordFromEnv); err == nil && password != "" {
		*parts = append(*parts, fmt.Sprintf("PASSWORD '%s'", escapeSQLString(password)))
	}

	if token, err := resolveCredential(auth.BearerToken, auth.BearerTokenFromEnv); err == nil && token != "" {
		*parts = append(*parts, fmt.Sprintf("BEARER_TOKEN '%s'", escapeSQLString(token)))
	}

	// HTTP proxy configuration
	if proxy, err := resolveCredential(auth.HTTPProxy, auth.HTTPProxyFromEnv); err == nil && proxy != "" {
		*parts = append(*parts, fmt.Sprintf("HTTP_PROXY '%s'", escapeSQLString(proxy)))
	}

	if proxyUser, err := resolveCredential(auth.HTTPProxyUsername, auth.HTTPProxyUsernameFromEnv); err == nil && proxyUser != "" {
		*parts = append(*parts, fmt.Sprintf("HTTP_PROXY_USERNAME '%s'", escapeSQLString(proxyUser)))
	}

	if proxyPass, err := resolveCredential(auth.HTTPProxyPassword, auth.HTTPProxyPasswordFromEnv); err == nil && proxyPass != "" {
		*parts = append(*parts, fmt.Sprintf("HTTP_PROXY_PASSWORD '%s'", escapeSQLString(proxyPass)))
	}

	return nil
}

func addR2Params(parts *[]string, auth *R2AuthSpec) error {
	if auth == nil {
		return fmt.Errorf("r2 secret requires r2 credentials")
	}

	if keyID, err := resolveCredential(auth.KeyID, auth.KeyIDFromEnv); err == nil && keyID != "" {
		*parts = append(*parts, fmt.Sprintf("KEY_ID '%s'", escapeSQLString(keyID)))
	}

	if secret, err := resolveCredential(auth.Secret, auth.SecretFromEnv); err == nil && secret != "" {
		*parts = append(*parts, fmt.Sprintf("SECRET '%s'", escapeSQLString(secret)))
	}

	if auth.AccountID != "" {
		*parts = append(*parts, fmt.Sprintf("ACCOUNT_ID '%s'", escapeSQLString(auth.AccountID)))
	}

	if auth.Endpoint != "" {
		*parts = append(*parts, fmt.Sprintf("ENDPOINT '%s'", escapeSQLString(auth.Endpoint)))
	}

	return nil
}

func addAzureParams(parts *[]string, auth *AzureAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("azure secret requires azure credentials")
	}

	if connStr, err := resolveCredential(auth.ConnectionString, auth.ConnectionStringFromEnv); err == nil && connStr != "" {
		*parts = append(*parts, fmt.Sprintf("CONNECTION_STRING '%s'", escapeSQLString(connStr)))
	}

	if auth.AccountName != "" {
		*parts = append(*parts, fmt.Sprintf("ACCOUNT_NAME '%s'", escapeSQLString(auth.AccountName)))
	}

	if key, err := resolveCredential(auth.AccountKey, auth.AccountKeyFromEnv); err == nil && key != "" {
		*parts = append(*parts, fmt.Sprintf("ACCOUNT_KEY '%s'", escapeSQLString(key)))
	}

	return nil
}

func addHuggingfaceParams(parts *[]string, auth *HuggingfaceAuthSpec) error {
	if auth == nil {
		return fmt.Errorf("huggingface secret requires huggingface credentials")
	}

	token, err := resolveCredential(auth.Token, auth.TokenFromEnv)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	*parts = append(*parts, fmt.Sprintf("TOKEN '%s'", escapeSQLString(token)))

	return nil
}

// resolveCredential returns the inline value or resolves from environment variable.
// Returns an error only if both are empty or if env var is missing.
func resolveCredential(inline, envName string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if envName != "" {
		return resolveEnvVar(envName)
	}
	return "", fmt.Errorf("credential not provided (neither inline nor from environment)")
}

// extensionForSecretType returns the DuckDB extension required for a secret type.
func extensionForSecretType(secretType string) string {
	switch strings.ToLower(secretType) {
	case "s3", "gcs", "http", "r2", "huggingface":
		return "httpfs"
	case "azure":
		return "azure"
	case "postgres":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return ""
	}
}

// resolveEnvVar looks up an environment variable.
func resolveEnvVar(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	val, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(val) == "" {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return val, nil
}
