package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
)

// registryFlagHelp is the shared --registry help text of the auth commands.
const registryFlagHelp = "Registry URL (overrides bino.toml, BINO_REGISTRY_URL, and ~/.bino/config.toml; default " + registry.DefaultURL + ")"

// authRegistryURL resolves the registry for the auth commands, which must
// work outside a bino project: --registry flag, then the enclosing project's
// [registry].url, then BINO_REGISTRY_URL, then the global config
// (~/.bino/config.toml), then the public default.
func authRegistryURL(flagURL string) (string, error) {
	if u := strings.TrimSpace(flagURL); u != "" {
		return strings.TrimRight(u, "/"), nil
	}
	if workdir, err := os.Getwd(); err == nil {
		if root, err := pathutil.FindProjectRoot(workdir); err == nil {
			if cfg, err := pathutil.LoadProjectConfig(root); err == nil && cfg.Registry.URL != "" {
				return strings.TrimRight(strings.TrimSpace(cfg.Registry.URL), "/"), nil
			}
		}
	}
	return registry.FallbackURL()
}

func newRegistryLoginCommand() *cobra.Command {
	var (
		flagRegistry  string
		flagWithToken bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a registry with a personal access token",
		Long: `Stores a personal access token in ~/.bino/credentials.json (0600) so
subsequent registry commands authenticate automatically (unless bino.toml or
BINO_REGISTRY_TOKEN provides one).

Create a token in the registry web UI under Settings → Tokens, then paste it
when prompted. Non-interactive: pipe the token on stdin with --with-token.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := NewOutput(OutputConfig{})
			url, err := authRegistryURL(flagRegistry)
			if err != nil {
				return ConfigError(err)
			}
			client := registry.NewClient(registry.Config{URL: url})

			pat, err := readPAT(cmd.InOrStdin(), out, url, flagWithToken)
			if err != nil {
				return loginCanceled(out, err)
			}
			res, err := client.ExchangePAT(ctx, pat)
			if err != nil {
				return ExternalError(fmt.Errorf("token rejected by %s: %w", url, err))
			}
			if err := storeCredential(url, registry.Credential{PAT: pat, PATID: res.ID}); err != nil {
				return RuntimeError(err)
			}

			credPath, _ := registry.CredentialsPath()
			out.Success(fmt.Sprintf("Logged in to %s", url))
			out.Info(fmt.Sprintf("Access token stored in %s", credPath))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", registryFlagHelp)
	cmd.Flags().BoolVar(&flagWithToken, "with-token", false, "Read the personal access token from stdin instead of prompting")
	return cmd
}

func loginCanceled(out *Output, err error) error {
	if errors.Is(err, errAddCanceled) {
		out.Info("Login canceled")
		return nil
	}
	return err
}

// readPAT obtains the personal access token to store: from stdin when
// --with-token (or non-interactive), otherwise via a masked prompt that points
// the user at the web UI. It validates the token prefix before returning.
func readPAT(stdin io.Reader, out *Output, url string, withToken bool) (string, error) {
	var pat string
	if withToken || !isInteractive() {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", RuntimeError(err)
		}
		pat = strings.TrimSpace(string(data))
	} else {
		out.Info(fmt.Sprintf("Create a token at %s/settings/tokens, then paste it here.", url))
		var err error
		if pat, err = huhPassword("Personal access token"); err != nil {
			return "", err
		}
		pat = strings.TrimSpace(pat)
	}
	if !strings.HasPrefix(pat, registry.PATPrefix) {
		return "", ConfigErrorf("that does not look like a personal access token (expected %q prefix)", registry.PATPrefix)
	}
	return pat, nil
}

func storeCredential(url string, cred registry.Credential) error {
	creds, err := registry.LoadCredentials()
	if err != nil {
		return err
	}
	creds.Set(url, cred)
	return registry.SaveCredentials(creds)
}

func newRegistryLogoutCommand() *cobra.Command {
	var flagRegistry string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored registry credential and revoke it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := NewOutput(OutputConfig{})
			url, err := authRegistryURL(flagRegistry)
			if err != nil {
				return ConfigError(err)
			}
			creds, err := registry.LoadCredentials()
			if err != nil {
				return RuntimeError(err)
			}
			cred, ok := creds.Get(url)
			if !ok {
				out.Info("Not logged in to " + url)
				return nil
			}

			// Best-effort server-side revocation; the local credential is
			// removed regardless.
			client := registry.NewClient(registry.Config{URL: url, Token: cred.PAT})
			id := cred.PATID
			if id == "" {
				if res, err := client.ExchangePAT(ctx, cred.PAT); err == nil {
					id = res.ID
				}
			}
			revoked := false
			if id != "" {
				if err := client.RevokePAT(ctx, id); err == nil {
					revoked = true
				}
			}
			if !revoked {
				out.Warning("Could not revoke the token server-side — it may still be active; revoke it in the registry web UI")
			}

			creds.Delete(url)
			if err := registry.SaveCredentials(creds); err != nil {
				return RuntimeError(err)
			}
			out.Success("Logged out of " + url)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", registryFlagHelp)
	return cmd
}
