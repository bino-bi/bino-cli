package template

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"bino.bi/bino/internal/archive"
	"bino.bi/bino/internal/pathutil"
)

const (
	cacheSubdir       = "templates"
	downloadTimeout   = 5 * time.Minute
	connectTimeout    = 10 * time.Second
	maxDownloadBytes  = 64 << 20 // 64 MiB compressed-archive cap
	githubAPIBase     = "https://api.github.com"
	githubCodeloadURL = "https://codeload.github.com"
)

// Manager fetches and caches remote templates, keyed by immutable commit SHA. It
// mirrors engine.Manager so the fetch/cache machinery stays consistent.
type Manager struct {
	cacheDir   string
	httpClient *http.Client
	apiBase    string // overridable for tests
	codeload   string // overridable for tests
}

// NewManager creates a Manager rooted at ~/.bino/templates.
func NewManager() (*Manager, error) {
	cacheDir, err := pathutil.CacheDir(cacheSubdir)
	if err != nil {
		return nil, fmt.Errorf("resolve template cache directory: %w", err)
	}
	return NewManagerWithClient(cacheDir, defaultHTTPClient()), nil
}

// NewManagerWithClient builds a Manager with an explicit cache dir and client
// (used by tests).
func NewManagerWithClient(cacheDir string, client *http.Client) *Manager {
	return &Manager{
		cacheDir:   cacheDir,
		httpClient: client,
		apiBase:    githubAPIBase,
		codeload:   githubCodeloadURL,
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: downloadTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: connectTimeout,
			ForceAttemptHTTP2:     true,
		},
	}
}

// Resolved is a template ready to render.
type Resolved struct {
	Manifest   *ProjectTemplate
	Root       fs.FS
	Provenance string // "builtin:minimal" | "owner/repo@<sha>" | "local:path" | "url:..."
	SHA        string
	cleanup    func()
}

// Close releases any temporary extraction directory backing the template.
func (r *Resolved) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Resolve turns a parsed Source into a render-ready template. Built-in and local
// sources need no network; remote sources are fetched and cached by SHA, and
// hard-error in offline mode on a cache miss.
func (m *Manager) Resolve(ctx context.Context, src Source, offline bool) (*Resolved, error) {
	switch src.Kind {
	case SourceBuiltin:
		return resolveBuiltin(src.Name)
	case SourceLocal:
		return resolveLocal(src.Path)
	case SourceCurated:
		e, ok := curated[src.Name]
		if !ok {
			return nil, fmt.Errorf("curated template %q is not available in this release", src.Name)
		}
		return m.resolveGitHub(ctx, e.Owner, e.Repo, "", e.SHA, offline)
	case SourceShorthand:
		return m.resolveGitHub(ctx, src.Owner, src.Repo, src.Subdir, src.Ref, offline)
	case SourceURL:
		return m.resolveURL(ctx, src.URL, offline)
	default:
		return nil, fmt.Errorf("unsupported template source")
	}
}

func resolveBuiltin(name string) (*Resolved, error) {
	manifest, err := BuiltinManifest(name)
	if err != nil {
		return nil, err
	}
	root, err := BuiltinRoot(name)
	if err != nil {
		return nil, err
	}
	return &Resolved{Manifest: manifest, Root: root, Provenance: "builtin:" + name}, nil
}

func resolveLocal(path string) (*Resolved, error) {
	manifest, root, err := LoadTree(os.DirFS(path))
	if err != nil {
		return nil, err
	}
	return &Resolved{Manifest: manifest, Root: root, Provenance: "local:" + path}, nil
}

func (m *Manager) resolveGitHub(ctx context.Context, owner, repo, subdir, ref string, offline bool) (*Resolved, error) {
	repoDir := filepath.Join(m.cacheDir, owner, repo)
	refKey := ref
	if refKey == "" {
		refKey = defaultRefToken
	}
	prov := func(sha string) string { return fmt.Sprintf("%s/%s@%s", owner, repo, sha) }

	// Cache-first: a previously-resolved ref (or an explicit SHA) serves offline.
	idx := loadIndex(repoDir)
	if sha, ok := idx.Refs[refKey]; ok && dirExists(filepath.Join(repoDir, sha)) {
		return m.fromCache(repoDir, sha, subdir, prov(sha))
	}
	if isHexSHA(ref) && dirExists(filepath.Join(repoDir, strings.ToLower(ref))) {
		sha := strings.ToLower(ref)
		return m.fromCache(repoDir, sha, subdir, prov(sha))
	}

	if offline {
		return nil, fmt.Errorf("template %s/%s@%s is not cached (offline mode)", owner, repo, refOrDefault(ref))
	}

	sha, err := m.resolveSHA(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	shaDir := filepath.Join(repoDir, sha)

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	lock := flock.New(filepath.Join(repoDir, ".lock"))
	locked, err := lock.TryLockContext(ctx, 200*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("lock template cache: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("could not acquire template cache lock for %s/%s", owner, repo)
	}
	defer func() { _ = lock.Unlock() }()

	if !dirExists(shaDir) {
		if err := m.downloadAndExtract(ctx, owner, repo, sha, shaDir); err != nil {
			return nil, err
		}
	}
	idx = loadIndex(repoDir)
	idx.set(refKey, sha)
	if isHexSHA(ref) {
		idx.set(strings.ToLower(ref), sha)
	}
	if err := saveIndex(repoDir, idx); err != nil {
		return nil, err
	}
	return m.fromCache(repoDir, sha, subdir, prov(sha))
}

func (m *Manager) fromCache(repoDir, sha, subdir, provenance string) (*Resolved, error) {
	rootDir := filepath.Join(repoDir, sha)
	if subdir != "" {
		rootDir = filepath.Join(rootDir, filepath.FromSlash(subdir))
	}
	manifest, root, err := LoadTree(os.DirFS(rootDir))
	if err != nil {
		return nil, err
	}
	return &Resolved{Manifest: manifest, Root: root, Provenance: provenance, SHA: sha}, nil
}

// resolveSHA returns the commit SHA for a ref, minimizing requests: an explicit
// SHA needs none; otherwise one GitHub API call resolves the ref (HEAD for the
// default branch).
func (m *Manager) resolveSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	if isHexSHA(ref) {
		return strings.ToLower(ref), nil
	}
	target := ref
	if target == "" {
		target = "HEAD"
	}
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", m.apiBase, owner, repo, target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.sha")
	m.authorize(req)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve %s/%s@%s: %w", owner, repo, target, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("template %s/%s ref %q not found", owner, repo, target)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return "", fmt.Errorf("github rate limit or access denied for %s/%s (set BINO_GITHUB_TOKEN to raise the limit)", owner, repo)
	default:
		return "", fmt.Errorf("resolve %s/%s@%s: unexpected status %d", owner, repo, target, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	sha := strings.ToLower(strings.TrimSpace(string(body)))
	if !isHexSHA(sha) {
		return "", fmt.Errorf("unexpected commit response for %s/%s@%s", owner, repo, target)
	}
	return sha, nil
}

func (m *Manager) downloadAndExtract(ctx context.Context, owner, repo, sha, shaDir string) error {
	url := fmt.Sprintf("%s/%s/%s/zip/%s", m.codeload, owner, repo, sha)
	zipPath, cleanup, err := m.download(ctx, url)
	if err != nil {
		return err
	}
	defer cleanup()

	tmpDir, err := os.MkdirTemp(filepath.Dir(shaDir), ".extract-*")
	if err != nil {
		return fmt.Errorf("create extraction dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := archive.Extract(zipPath, tmpDir, archive.Options{
		Strip:     archive.StripAuto,
		Untrusted: true,
		Limits:    archive.DefaultLimits(),
	}); err != nil {
		return fmt.Errorf("extract %s/%s@%s: %w", owner, repo, sha, err)
	}

	// Atomic publish: a concurrent fetch that won the race is fine.
	if err := os.Rename(tmpDir, shaDir); err != nil {
		if dirExists(shaDir) {
			return nil
		}
		return fmt.Errorf("publish template cache entry: %w", err)
	}
	return nil
}

func (m *Manager) resolveURL(ctx context.Context, url string, offline bool) (*Resolved, error) {
	if offline {
		return nil, fmt.Errorf("cannot fetch %s in offline mode", url)
	}
	zipPath, cleanupZip, err := m.download(ctx, url)
	if err != nil {
		return nil, err
	}
	defer cleanupZip()

	tmpDir, err := os.MkdirTemp("", "bino-url-template-*")
	if err != nil {
		return nil, err
	}
	if err := archive.Extract(zipPath, tmpDir, archive.Options{
		Strip:     archive.StripAuto,
		Untrusted: true,
		Limits:    archive.DefaultLimits(),
	}); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("extract %s: %w", url, err)
	}
	manifest, root, err := LoadTree(os.DirFS(tmpDir))
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}
	return &Resolved{
		Manifest:   manifest,
		Root:       root,
		Provenance: "url:" + url,
		cleanup:    func() { os.RemoveAll(tmpDir) },
	}, nil
}

// download fetches url into a temp zip file, capped at maxDownloadBytes.
func (m *Manager) download(ctx context.Context, url string) (zipPath string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", nil, err
	}
	m.authorize(req)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "bino-template-*.zip")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.Remove(tmpFile.Name()) }
	limited := io.LimitReader(resp.Body, maxDownloadBytes+1)
	n, err := io.Copy(tmpFile, limited)
	closeErr := tmpFile.Close()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("download %s: %w", url, err)
	}
	if closeErr != nil {
		cleanup()
		return "", nil, closeErr
	}
	if n > maxDownloadBytes {
		cleanup()
		return "", nil, fmt.Errorf("download %s exceeds %d bytes", url, maxDownloadBytes)
	}
	return tmpFile.Name(), cleanup, nil
}

func (m *Manager) authorize(req *http.Request) {
	if tok := strings.TrimSpace(os.Getenv("BINO_GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "token "+tok)
	}
}

func refOrDefault(ref string) string {
	if ref == "" {
		return "<default branch>"
	}
	return ref
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
