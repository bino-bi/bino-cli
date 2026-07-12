package registry

import (
	"fmt"
	"os"
	"strings"
)

// DefaultURL is the public bino registry.
const DefaultURL = "https://registry.bino.bi"

// EnvToken is the fallback environment variable for the registry auth token.
const EnvToken = "BINO_REGISTRY_TOKEN"

// EnvURL overrides the registry URL for commands that run outside a project.
const EnvURL = "BINO_REGISTRY_URL"

// Config is the resolved registry connection configuration.
type Config struct {
	URL   string
	Token string // empty = anonymous
}

// ResolveConfig turns the raw [registry] values from bino.toml into a usable
// Config. The URL defaults to DefaultURL. Token precedence: the bino.toml
// value (a "${VAR}" form is expanded from the environment; an unset VAR is an
// error, so a misconfigured secret fails loudly instead of silently going
// anonymous), then BINO_REGISTRY_TOKEN, then the credential stored by
// `bino registry login` for this URL. An unreadable credentials file degrades
// to anonymous.
func ResolveConfig(rawURL, rawToken string) (Config, error) {
	url := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if url == "" {
		url = DefaultURL
	}
	token := strings.TrimSpace(rawToken)
	if strings.HasPrefix(token, "${") && strings.HasSuffix(token, "}") {
		name := token[2 : len(token)-1]
		val, ok := os.LookupEnv(name)
		if !ok {
			return Config{}, fmt.Errorf("registry token references ${%s} but %s is not set", name, name)
		}
		token = strings.TrimSpace(val)
	}
	if token == "" {
		token = strings.TrimSpace(os.Getenv(EnvToken))
	}
	if token == "" {
		if creds, err := LoadCredentials(); err == nil {
			if cred, ok := creds.Get(url); ok {
				token = cred.PAT
			}
		}
	}
	return Config{URL: url, Token: token}, nil
}
