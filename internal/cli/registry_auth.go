package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
)

// authRegistryURL resolves the registry for the auth commands, which must
// work outside a bino project: --registry flag, then the enclosing project's
// [registry].url, then BINO_REGISTRY_URL, then the public default.
func authRegistryURL(flagURL string) string {
	if u := strings.TrimSpace(flagURL); u != "" {
		return strings.TrimRight(u, "/")
	}
	if workdir, err := os.Getwd(); err == nil {
		if root, err := pathutil.FindProjectRoot(workdir); err == nil {
			if cfg, err := pathutil.LoadProjectConfig(root); err == nil && cfg.Registry.URL != "" {
				return strings.TrimRight(strings.TrimSpace(cfg.Registry.URL), "/")
			}
		}
	}
	if u := strings.TrimSpace(os.Getenv(registry.EnvURL)); u != "" {
		return strings.TrimRight(u, "/")
	}
	return registry.DefaultURL
}

// authClient builds a client through the normal token resolution chain
// (project token, env, stored credential) and fails when it would be
// anonymous — the token management commands always need auth.
func authClient(flagURL string) (*registry.Client, error) {
	url := authRegistryURL(flagURL)
	rawToken := ""
	if workdir, err := os.Getwd(); err == nil {
		if root, err := pathutil.FindProjectRoot(workdir); err == nil {
			if cfg, err := pathutil.LoadProjectConfig(root); err == nil {
				rawToken = cfg.Registry.Token
			}
		}
	}
	cfg, err := registry.ResolveConfig(url, rawToken)
	if err != nil {
		return nil, ConfigError(err)
	}
	if cfg.Token == "" {
		return nil, ConfigErrorf("not logged in to %s — run 'bino registry login' or set %s", url, registry.EnvToken)
	}
	return registry.NewClient(cfg), nil
}

var expiryDaysRe = regexp.MustCompile(`^(\d+)d$`)

// parseExpiry turns a human lifetime ("90d", "12h", "never", "") into the
// API's expiresIn seconds; 0 = never expires.
func parseExpiry(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" {
		return 0, nil
	}
	if m := expiryDaysRe.FindStringSubmatch(s); m != nil {
		days, err := strconv.Atoi(m[1])
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid expiry %q", s)
		}
		return int64(days) * 24 * 3600, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid expiry %q: use a positive duration like 90d or 12h, or \"never\"", s)
	}
	return int64(d.Seconds()), nil
}

// displayExpiry renders a wire expiry timestamp ("" = never) for humans.
func displayExpiry(s string) string {
	if s == "" {
		return "never"
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func newRegistryLoginCommand() *cobra.Command {
	var (
		flagRegistry      string
		flagEmail         string
		flagPasswordStdin bool
		flagWithToken     bool
		flagExpires       string
		flagName          string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a registry and store an access token",
		Long: `Authenticates with email and password (plus an authenticator code when the
account has two-factor enabled), creates a personal access token, and stores
it in ~/.bino/credentials.json (0600). Subsequent registry commands use the
stored token automatically unless bino.toml or BINO_REGISTRY_TOKEN provides one.

Non-interactive use: --email with --password-stdin, or --with-token to store
an existing personal access token read from stdin.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := NewOutput(OutputConfig{})
			url := authRegistryURL(flagRegistry)
			client := registry.NewClient(registry.Config{URL: url})

			expiresIn, err := parseExpiry(flagExpires)
			if err != nil {
				return ConfigError(err)
			}

			if flagWithToken {
				return loginWithToken(ctx, cmd.InOrStdin(), out, client, url)
			}

			email, password, err := loginCredentials(cmd.InOrStdin(), flagEmail, flagPasswordStdin)
			if err != nil {
				return loginCanceled(out, err)
			}

			auth, err := client.AuthWithPassword(ctx, email, password)
			var mfaErr *registry.MFARequiredError
			if errors.As(err, &mfaErr) {
				if !isInteractive() {
					return ConfigErrorf("the account requires an authenticator code; log in interactively or use --with-token")
				}
				auth, err = promptTOTP(ctx, out, client, mfaErr.MfaID)
			}
			if err != nil {
				if errors.Is(err, errAddCanceled) {
					return loginCanceled(out, err)
				}
				return ExternalError(fmt.Errorf("login to %s failed: %w", url, err))
			}

			name := strings.TrimSpace(flagName)
			if name == "" {
				name = "bino-cli"
				if host, err := os.Hostname(); err == nil && host != "" {
					name = "bino-cli @ " + host
				}
			}
			created, err := client.CreatePAT(ctx, auth.Token, name, expiresIn)
			if err != nil {
				return ExternalError(fmt.Errorf("create access token: %w", err))
			}
			if err := storeCredential(url, registry.Credential{PAT: created.Token, PATID: created.ID, User: email}); err != nil {
				return RuntimeError(err)
			}

			credPath, _ := registry.CredentialsPath()
			out.Success(fmt.Sprintf("Logged in to %s as %s", url, email))
			out.Info(fmt.Sprintf("Access token %q stored in %s (expires: %s)", name, credPath, displayExpiry(created.Expires)))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", "Registry URL (defaults to the project's or "+registry.DefaultURL+")")
	cmd.Flags().StringVar(&flagEmail, "email", "", "Account email (prompts when omitted)")
	cmd.Flags().BoolVar(&flagPasswordStdin, "password-stdin", false, "Read the password from stdin")
	cmd.Flags().BoolVar(&flagWithToken, "with-token", false, "Store an existing personal access token read from stdin")
	cmd.Flags().StringVar(&flagExpires, "expires", "", "Lifetime of the created token (e.g. 90d, 12h; default never)")
	cmd.Flags().StringVar(&flagName, "name", "", "Name for the created token (default \"bino-cli @ <hostname>\")")
	return cmd
}

func loginCanceled(out *Output, err error) error {
	if errors.Is(err, errAddCanceled) {
		out.Info("Login canceled")
		return nil
	}
	return err
}

// loginWithToken stores an existing personal access token read from stdin,
// validating it against the registry first.
func loginWithToken(ctx context.Context, stdin io.Reader, out *Output, client *registry.Client, url string) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return RuntimeError(err)
	}
	pat := strings.TrimSpace(string(data))
	if !strings.HasPrefix(pat, registry.PATPrefix) {
		return ConfigErrorf("stdin does not look like a personal access token (expected %q prefix)", registry.PATPrefix)
	}
	res, err := client.ExchangePAT(ctx, pat)
	if err != nil {
		return ExternalError(fmt.Errorf("token rejected by %s: %w", url, err))
	}
	if err := storeCredential(url, registry.Credential{PAT: pat, PATID: res.ID}); err != nil {
		return RuntimeError(err)
	}
	out.Success(fmt.Sprintf("Logged in to %s", url))
	return nil
}

// loginCredentials gathers email and password from flags/stdin, prompting
// interactively for whatever is missing.
func loginCredentials(stdin io.Reader, flagEmail string, passwordStdin bool) (email, password string, err error) {
	email = strings.TrimSpace(flagEmail)
	if passwordStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", "", RuntimeError(err)
		}
		password = strings.TrimSpace(string(data))
	}
	if email != "" && password != "" {
		return email, password, nil
	}
	if !isInteractive() {
		return "", "", ConfigErrorf("non-interactive login: pass --email and --password-stdin, or --with-token")
	}
	if email == "" {
		if email, err = huhInput("Email", "you@example.com", "", nil); err != nil {
			return "", "", err
		}
	}
	if password == "" {
		if password, err = huhPassword("Password"); err != nil {
			return "", "", err
		}
	}
	return email, password, nil
}

// promptTOTP asks for an authenticator code, allowing up to three attempts.
func promptTOTP(ctx context.Context, out *Output, client *registry.Client, mfaID string) (registry.AuthResult, error) {
	for attempt := 1; ; attempt++ {
		code, err := huhInput("Authenticator code", "123456", "", nil)
		if err != nil {
			return registry.AuthResult{}, err
		}
		auth, err := client.VerifyTOTP(ctx, mfaID, strings.TrimSpace(code))
		var apiErr *registry.APIError
		if err != nil && errors.As(err, &apiErr) && apiErr.Code == "invalid_totp" && attempt < 3 {
			out.Warning("Invalid code, try again")
			continue
		}
		return auth, err
	}
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
			url := authRegistryURL(flagRegistry)
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
				out.Warning("Could not revoke the token server-side — it may still be active; revoke it via 'bino registry token revoke' or the registry UI")
			}

			creds.Delete(url)
			if err := registry.SaveCredentials(creds); err != nil {
				return RuntimeError(err)
			}
			out.Success("Logged out of " + url)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", "Registry URL (defaults to the project's or "+registry.DefaultURL+")")
	return cmd
}

func newRegistryTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage personal access tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRegistryTokenCreateCommand())
	cmd.AddCommand(newRegistryTokenListCommand())
	cmd.AddCommand(newRegistryTokenRevokeCommand())
	return cmd
}

func newRegistryTokenCreateCommand() *cobra.Command {
	var (
		flagRegistry string
		flagExpires  string
		flagName     string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a personal access token",
		Long:  "Creates a token and prints its plaintext exactly once — it cannot be retrieved later.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := NewOutput(OutputConfig{})
			expiresIn, err := parseExpiry(flagExpires)
			if err != nil {
				return ConfigError(err)
			}
			client, err := authClient(flagRegistry)
			if err != nil {
				return err
			}
			created, err := client.CreatePAT(ctx, "", flagName, expiresIn)
			if err != nil {
				return ExternalError(err)
			}
			out.Success(fmt.Sprintf("Token created: %s", created.Name))
			fmt.Printf("\n  %s\n\n", created.Token)
			out.Warning("Save it now — it will not be shown again")
			out.Info(fmt.Sprintf("ID: %s   Expires: %s", created.ID, displayExpiry(created.Expires)))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", "Registry URL (defaults to the project's or "+registry.DefaultURL+")")
	cmd.Flags().StringVar(&flagExpires, "expires", "", "Token lifetime (e.g. 90d, 12h; default never)")
	cmd.Flags().StringVar(&flagName, "name", "", "Token name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newRegistryTokenListCommand() *cobra.Command {
	var flagRegistry string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your personal access tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authClient(flagRegistry)
			if err != nil {
				return err
			}
			items, err := client.ListPATs(cmd.Context())
			if err != nil {
				return ExternalError(err)
			}
			if len(items) == 0 {
				fmt.Println("No tokens.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tPREFIX\tCREATED\tLAST USED\tEXPIRES")
			for _, item := range items {
				lastUsed := "-"
				if item.LastUsed != "" {
					lastUsed = displayExpiry(item.LastUsed)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					item.ID, item.Name, item.Prefix, displayExpiry(item.Created), lastUsed, displayExpiry(item.Expires))
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", "Registry URL (defaults to the project's or "+registry.DefaultURL+")")
	return cmd
}

func newRegistryTokenRevokeCommand() *cobra.Command {
	var flagRegistry string
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a personal access token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := NewOutput(OutputConfig{})
			client, err := authClient(flagRegistry)
			if err != nil {
				return err
			}
			if err := client.RevokePAT(cmd.Context(), args[0]); err != nil {
				var apiErr *registry.APIError
				if errors.As(err, &apiErr) && apiErr.Code == "token_not_found" {
					return ConfigErrorf("token %s not found", args[0])
				}
				return ExternalError(err)
			}
			out.Success("Token revoked: " + args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRegistry, "registry", "", "Registry URL (defaults to the project's or "+registry.DefaultURL+")")
	return cmd
}
