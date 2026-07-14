package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	connectTimeout = 10 * time.Second
	// maxBodyBytes caps response bodies; packages are single YAML documents
	// (server-side limit 5 MiB).
	maxBodyBytes = 8 << 20
	// maxResourceBytes caps resource downloads; resources can be up to 50 MiB
	// server-side, plus a small overhead allowance.
	maxResourceBytes = 50<<20 + 1<<20
	// maxRetryAfter caps how long a single 429 retry may wait.
	maxRetryAfter = 30 * time.Second
)

// PATPrefix marks a personal access token (as opposed to a raw auth JWT).
// PATs are exchanged for a short-lived JWT once per client; JWTs pass through
// unchanged.
const PATPrefix = "bino_pat_"

// Client is a minimal consumer of the registry's read and auth APIs.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client

	exchOnce sync.Once // PAT→JWT exchange, at most once per client
	exchJWT  string
	exchErr  error
}

// NewClient builds a client for the given resolved configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		baseURL: cfg.URL,
		token:   cfg.Token,
		hc: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
				TLSHandshakeTimeout:   connectTimeout,
				ResponseHeaderTimeout: connectTimeout,
				ForceAttemptHTTP2:     true,
			},
		},
	}
}

// APIError is a structured error response from the registry.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("registry: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("registry: %s", e.Code)
}

// ResolveResult is the resolve endpoint's response.
type ResolveResult struct {
	Package      string   `json:"package"`
	Tag          string   `json:"tag"` // empty when ref was an exact version
	Version      string   `json:"version"`
	Kind         string   `json:"kind"`
	Digest       string   `json:"digest"`
	Dependencies []string `json:"dependencies"`
	DownloadURL  string   `json:"downloadUrl"`
}

// Resolve resolves a package ref (tag or exact version; empty = default tag).
func (c *Client) Resolve(ctx context.Context, scope, name, ref string) (ResolveResult, error) {
	u := fmt.Sprintf("%s/api/registry/resolve/%s/%s", c.baseURL, url.PathEscape(scope), url.PathEscape(name))
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	var out ResolveResult
	body, _, err := c.get(ctx, u)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("registry: decode resolve response: %w", err)
	}
	return out, nil
}

// Download fetches a version's canonical document. It returns the body and
// the digest advertised in the ETag header ("sha256:<hex>", unquoted).
func (c *Client) Download(ctx context.Context, scope, name, version string) (body []byte, etagDigest string, err error) {
	u := fmt.Sprintf("%s/api/registry/download/%s/%s/%s", c.baseURL, url.PathEscape(scope), url.PathEscape(name), url.PathEscape(version))
	body, header, err := c.get(ctx, u)
	if err != nil {
		return nil, "", err
	}
	etag := strings.TrimPrefix(header.Get("ETag"), "W/")
	return body, strings.Trim(etag, `"`), nil
}

// ResourceMeta describes one binary resource bundled with a package version.
type ResourceMeta struct {
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
}

// ListResources lists the binary resources bundled with a package version.
func (c *Client) ListResources(ctx context.Context, scope, name, version string) ([]ResourceMeta, error) {
	u := fmt.Sprintf("%s/api/registry/resources/%s/%s/%s", c.baseURL, url.PathEscape(scope), url.PathEscape(name), url.PathEscape(version))
	var out []ResourceMeta
	body, _, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("registry: decode resources response: %w", err)
	}
	return out, nil
}

// DownloadResource fetches one bundled resource's bytes. It returns the body
// and the content hash advertised in the ETag header ("sha256:<hex>",
// unquoted).
func (c *Client) DownloadResource(ctx context.Context, scope, name, version, resourceName string) (body []byte, contentHash string, err error) {
	u := fmt.Sprintf("%s/api/registry/resources/%s/%s/%s/%s", c.baseURL,
		url.PathEscape(scope), url.PathEscape(name), url.PathEscape(version), url.PathEscape(resourceName))
	body, header, err := c.getWithLimit(ctx, u, maxResourceBytes)
	if err != nil {
		return nil, "", err
	}
	etag := strings.TrimPrefix(header.Get("ETag"), "W/")
	return body, strings.Trim(etag, `"`), nil
}

// SearchParams are the query parameters for the search endpoint.
type SearchParams struct {
	Query   string
	Kinds   []string
	Scopes  []string // no "@" prefix
	Tags    []string
	Page    int
	PerPage int
}

// SearchItem is one search hit (subset of the server's fields the CLI shows).
type SearchItem struct {
	Package       string `json:"package"`
	Kind          string `json:"kind"`
	Description   string `json:"description"`
	LatestVersion string `json:"latestVersion"`
	PullsTotal    int    `json:"pullsTotal"`
}

// SearchResult is the search endpoint's response envelope.
type SearchResult struct {
	Page       int          `json:"page"`
	PerPage    int          `json:"perPage"`
	TotalItems int          `json:"totalItems"`
	TotalPages int          `json:"totalPages"`
	Items      []SearchItem `json:"items"`
}

// Search queries the registry's full-text search.
func (c *Client) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	q := url.Values{}
	if p.Query != "" {
		q.Set("q", p.Query)
	}
	for _, k := range p.Kinds {
		q.Add("kind", k)
	}
	for _, s := range p.Scopes {
		q.Add("scope", strings.TrimPrefix(s, "@"))
	}
	for _, t := range p.Tags {
		q.Add("tag", t)
	}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.PerPage > 0 {
		q.Set("perPage", strconv.Itoa(p.PerPage))
	}
	u := c.baseURL + "/api/registry/search"
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	var out SearchResult
	body, _, err := c.get(ctx, u)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("registry: decode search response: %w", err)
	}
	return out, nil
}

// bearer returns the effective Authorization value: raw JWTs (and the empty
// anonymous token) pass through; PAT-prefixed tokens are exchanged once per
// client for a short-lived JWT.
func (c *Client) bearer(ctx context.Context) (string, error) {
	if !strings.HasPrefix(c.token, PATPrefix) {
		return c.token, nil
	}
	c.exchOnce.Do(func() {
		res, err := c.ExchangePAT(ctx, c.token)
		if err != nil {
			c.exchErr = fmt.Errorf("exchange access token: %w", err)
			return
		}
		c.exchJWT = res.Token
	})
	return c.exchJWT, c.exchErr
}

// get performs an authenticated GET, retrying once on 429 per Retry-After,
// capped at the default maxBodyBytes.
func (c *Client) get(ctx context.Context, u string) ([]byte, http.Header, error) {
	return c.getWithLimit(ctx, u, maxBodyBytes)
}

// getWithLimit is get with an explicit body-size cap, for call sites (like
// resource downloads) that need a larger limit than the default.
func (c *Client) getWithLimit(ctx context.Context, u string, maxBytes int64) ([]byte, http.Header, error) {
	auth, err := c.bearer(ctx)
	if err != nil {
		return nil, nil, err
	}
	return c.request(ctx, http.MethodGet, u, nil, auth, maxBytes)
}

// doJSON performs a request with a JSON body (nil = none), decoding a 2xx
// response into out (nil = discard). auth is the raw Authorization value
// ("" = anonymous) — callers choose between bearer(), an explicit session
// JWT, or anonymous.
func (c *Client) doJSON(ctx context.Context, method, u, auth string, reqBody, out any) error {
	var payload []byte
	if reqBody != nil {
		var err error
		if payload, err = json.Marshal(reqBody); err != nil {
			return err
		}
	}
	body, _, err := c.request(ctx, method, u, payload, auth, maxBodyBytes)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("registry: decode response: %w", err)
		}
	}
	return nil
}

// request performs an HTTP request with auth, one 429 retry per Retry-After,
// and non-2xx mapping to APIError. maxBytes caps the response body.
func (c *Client) request(ctx context.Context, method, u string, reqBody []byte, auth string, maxBytes int64) ([]byte, http.Header, error) {
	body, header, status, err := c.doOnce(ctx, method, u, reqBody, auth, maxBytes)
	if err == nil && status == http.StatusTooManyRequests {
		wait := retryAfter(header)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(wait):
		}
		body, header, status, err = c.doOnce(ctx, method, u, reqBody, auth, maxBytes)
	}
	if err != nil {
		return nil, nil, err
	}
	if status < 200 || status > 299 {
		apiErr := &APIError{Status: status}
		if jsonErr := json.Unmarshal(body, apiErr); jsonErr != nil || apiErr.Code == "" {
			apiErr.Code = fmt.Sprintf("http_%d", status)
		}
		return nil, nil, apiErr
	}
	return body, header, nil
}

func (c *Client) doOnce(ctx context.Context, method, u string, reqBody []byte, auth string, maxBytes int64) (body []byte, header http.Header, status int, err error) {
	var reader io.Reader
	if reqBody != nil {
		reader = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, nil, 0, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("registry: %w", err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("registry: read response: %w", err)
	}
	return body, resp.Header, resp.StatusCode, nil
}

func retryAfter(header http.Header) time.Duration {
	wait := time.Second
	if v := header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			wait = time.Duration(secs) * time.Second
		} else if at, err := http.ParseTime(v); err == nil {
			wait = time.Until(at)
		}
	}
	if wait < 0 {
		wait = 0
	}
	if wait > maxRetryAfter {
		wait = maxRetryAfter
	}
	return wait
}
