package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/version"
)

// envSigstoreIDToken is an OIDC token for keyless signing outside GitHub
// Actions, as cosign reads it.
const envSigstoreIDToken = "SIGSTORE_ID_TOKEN" //nolint:gosec // G101: the variable's name, not a token

// signOptions are the --sign flags.
type signOptions struct {
	sign      bool
	keyless   bool
	key       string
	certChain string
}

// publishSigning is a checked --sign request: either a loaded key, or
// keyless with a known token source. The keyless signer is built at signing
// time because its token is short-lived.
type publishSigning struct {
	key     *registry.ReleaseSigner
	keyless bool
	actions bool // the token comes from GitHub Actions, not SIGSTORE_ID_TOKEN
}

func (s *publishSigning) rootKind() string {
	if s.keyless {
		return registry.SignerKeyless
	}
	return s.key.RootKind
}

// newPublishSigning checks the --sign flags and loads the key, before
// anything is uploaded. It returns nil when the publish is not signed.
func newPublishSigning(o signOptions) (*publishSigning, error) {
	if !o.sign {
		if o.keyless || o.key != "" || o.certChain != "" {
			return nil, ConfigErrorf("--keyless, --key and --cert-chain only apply with --sign")
		}
		return nil, nil
	}
	switch {
	case o.keyless && o.key != "":
		return nil, ConfigErrorf("--keyless and --key are exclusive: pick one way to sign")
	case o.certChain != "" && o.key == "":
		return nil, ConfigErrorf("--cert-chain needs --key, the private key of its first certificate")
	case o.keyless:
		if os.Getenv(envSigstoreIDToken) != "" {
			return &publishSigning{keyless: true}, nil
		}
		if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" && os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN") != "" {
			return &publishSigning{keyless: true, actions: true}, nil
		}
		return nil, wrapErrorWithHint(ErrorKindConfig,
			errors.New("--keyless needs an OIDC identity token, and none was found"),
			"on GitHub Actions give the job 'permissions: id-token: write'; elsewhere set "+envSigstoreIDToken+" to a token for the audience \"sigstore\"")
	case o.key != "":
		s, err := registry.NewKeySigner(o.key, o.certChain)
		if err != nil {
			return nil, ConfigError(err)
		}
		return &publishSigning{key: s}, nil
	}
	return nil, wrapErrorWithHint(ErrorKindConfig, errors.New("--sign needs a way to sign"),
		"use --keyless (Sigstore, from CI), --key <private-key.pem> (a long-lived key) or --key <private-key.pem> --cert-chain <chain.pem> (a company CA)")
}

// checkSignable checks a dry run of the publish about to be signed: the
// registry must have digested the tree exactly as bino did, and a keyless
// signature, which goes to the public Sigstore log, needs a public package.
// declared is the visibility the manifest sends, set only on a first publish.
func checkSignable(s *publishSigning, res registry.DryRunResult, localDigest, declared string) error {
	if res.Digest != localDigest {
		return ExternalErrorf("the registry digests this package as %s, but bino computed %s; refusing to sign a digest bino cannot check", res.Digest, localDigest)
	}
	if !s.keyless {
		return nil
	}
	vis := res.Visibility
	if vis == "" {
		vis = declared
	}
	if vis != "public" {
		return wrapErrorWithHint(ErrorKindConfig,
			errors.New("--keyless writes the package name and version to the public Sigstore transparency log, and this package is not public"),
			"sign a private package with --key, or make it public first")
	}
	return nil
}

// signRelease signs the statement for pkg@ver with digest and returns the
// bundle and the signer that made it.
func signRelease(ctx context.Context, s *publishSigning, pkg, ver, digest string) (json.RawMessage, *registry.ReleaseSigner, error) {
	scope, name, err := registry.ParseName(pkg)
	if err != nil {
		return nil, nil, ConfigError(err)
	}
	pred, err := registryattest.NewPredicate(publishChannel(s.actions), "https://bino.bi/cli@"+version.Version)
	if err != nil {
		return nil, nil, RuntimeError(err)
	}
	st, err := registryattest.NewStatement(scope, name, ver, digest, pred)
	if err != nil {
		return nil, nil, RuntimeError(err)
	}
	signer := s.key
	if s.keyless {
		token, err := keylessToken(ctx, s.actions)
		if err != nil {
			return nil, nil, err
		}
		if signer, err = registry.NewKeylessSigner(token); err != nil {
			return nil, nil, RuntimeError(err)
		}
	}
	bundle, err := signer.Sign(ctx, st)
	if err != nil {
		return nil, nil, ExternalError(err)
	}
	return bundle, signer, nil
}

// publishChannel is the channel record of a publish. Every publish today
// authenticates with a token from `bino registry login` (or the environment);
// Trusted Publishing, which exchanges a CI OIDC token instead, will claim
// registryattest.ChannelOIDC here. On GitHub Actions the record names the run
// as the signing certificate does, since the registry refuses a claim the
// certificate does not back; without such a certificate it claims no run.
func publishChannel(actions bool) registryattest.PublishChannel {
	ch := registryattest.PublishChannel{Channel: registryattest.ChannelPAT}
	if !actions {
		return ch
	}
	env := os.Getenv
	repo := env("GITHUB_REPOSITORY")
	ch.Repository = repo
	// "acme/reports/.github/workflows/publish.yml@refs/tags/v1" names the
	// workflow path; the certificate carries the same as a URI.
	wf, _, _ := strings.Cut(env("GITHUB_WORKFLOW_REF"), "@")
	if path, ok := strings.CutPrefix(wf, repo+"/"); ok && repo != "" {
		ch.Workflow = path
	}
	ch.Ref = env("GITHUB_REF")
	ch.Commit = env("GITHUB_SHA")
	server, id, attempt := env("GITHUB_SERVER_URL"), env("GITHUB_RUN_ID"), env("GITHUB_RUN_ATTEMPT")
	if server != "" && repo != "" && id != "" && attempt != "" {
		ch.RunURL = server + "/" + repo + "/actions/runs/" + id + "/attempts/" + attempt
	}
	return ch
}

// keylessToken returns the OIDC token for Fulcio: SIGSTORE_ID_TOKEN, or one
// requested from GitHub Actions for the audience "sigstore".
func keylessToken(ctx context.Context, actions bool) (string, error) {
	if !actions {
		return os.Getenv(envSigstoreIDToken), nil
	}
	u, err := url.Parse(os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"))
	if err != nil {
		return "", ConfigErrorf("ACTIONS_ID_TOKEN_REQUEST_URL is not a URL")
	}
	q := u.Query()
	q.Set("audience", "sigstore")
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody) //nolint:gosec // G704: the runner sets this URL
	if err != nil {
		return "", ConfigErrorf("ACTIONS_ID_TOKEN_REQUEST_URL is not a URL")
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: the runner sets this URL
	if err != nil {
		return "", ExternalErrorf("request a GitHub Actions OIDC token: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", ExternalErrorf("request a GitHub Actions OIDC token: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", ExternalErrorf("request a GitHub Actions OIDC token: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Value == "" {
		return "", ExternalErrorf("request a GitHub Actions OIDC token: the response carries no token")
	}
	return out.Value, nil
}
