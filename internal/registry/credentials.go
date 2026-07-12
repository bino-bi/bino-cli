package registry

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// credentialsFileName is the per-user credential store under ~/.bino. It is
// written 0600 — it holds personal access tokens. `bino cache clean --global`
// deliberately spares it.
const credentialsFileName = "credentials.json"

// Credential is one stored registry login.
type Credential struct {
	PAT   string `json:"pat"`
	PATID string `json:"patId,omitempty"`
	User  string `json:"user,omitempty"` // email, informational
}

// Credentials is the on-disk credential store, keyed by normalized registry URL.
type Credentials struct {
	Registries map[string]Credential `json:"registries"`
}

// CredentialsPath returns ~/.bino/credentials.json.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bino", credentialsFileName), nil
}

// LoadCredentials reads the store; a missing file yields an empty store.
func LoadCredentials() (*Credentials, error) {
	creds := &Credentials{Registries: map[string]Credential{}}
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return creds, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if creds.Registries == nil {
		creds.Registries = map[string]Credential{}
	}
	return creds, nil
}

// SaveCredentials writes the store atomically with 0600 permissions.
func SaveCredentials(creds *Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomicMode(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Get returns the credential stored for a registry URL.
func (c *Credentials) Get(rawURL string) (Credential, bool) {
	cred, ok := c.Registries[NormalizeRegistryURL(rawURL)]
	return cred, ok
}

// Set stores a credential for a registry URL.
func (c *Credentials) Set(rawURL string, cred Credential) {
	c.Registries[NormalizeRegistryURL(rawURL)] = cred
}

// Delete removes a registry's credential, reporting whether one existed.
func (c *Credentials) Delete(rawURL string) bool {
	key := NormalizeRegistryURL(rawURL)
	_, ok := c.Registries[key]
	delete(c.Registries, key)
	return ok
}

// NormalizeRegistryURL canonicalizes a registry URL for use as a store key:
// lowercased scheme and host, default ports dropped, trailing slash stripped.
func NormalizeRegistryURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) || (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.Fragment = ""
	u.RawQuery = ""
	return u.String()
}
