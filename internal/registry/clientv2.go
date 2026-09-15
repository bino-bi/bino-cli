package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// FileMeta is one file of a resolved package tree.
type FileMeta struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"downloadUrl"`
}

// ResolveV2Result is the v2 resolve endpoint's response: a version's whole
// file tree, one entry per file with its own digest.
//
// A v1 (single-document) version resolves through this endpoint too, as a
// synthetic one-file tree carrying the v1 digest. Format records which of the
// two digest rules the files were published under; it is derived here, once,
// and must never be re-derived from a lock entry (a v1 package's bundled
// resources are added to the entry later and would break the inference).
type ResolveV2Result struct {
	Package      string     `json:"package"`
	Tag          string     `json:"tag"`
	Version      string     `json:"version"`
	Digest       string     `json:"digest"`
	Kinds        []string   `json:"kinds"`
	Dependencies []string   `json:"dependencies"`
	CompatEngine string     `json:"compatEngine"`
	CompatCli    string     `json:"compatCli"`
	Files        []FileMeta `json:"files"`

	// Format is derived from the response, not carried by it.
	Format string `json:"-"`
}

// ResolveV2 resolves a package ref against the v2 route, returning the
// version's file tree. A registry without the v2 routes answers 404/405; the
// caller is expected to fall back through Resolve, and SupportsV2 records the
// answer so the rest of a closure skips the probe.
func (c *Client) ResolveV2(ctx context.Context, scope, name, ref string) (ResolveV2Result, error) {
	u := fmt.Sprintf("%s/api/registry/v2/resolve/%s/%s", c.baseURL, url.PathEscape(scope), url.PathEscape(name))
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	var out ResolveV2Result
	body, _, err := c.get(ctx, u)
	if err != nil {
		if isRouteMissing(err) {
			c.v2.CompareAndSwap(v2Unknown, v2Absent)
		}
		return out, err
	}
	c.v2.Store(v2Supported)
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("registry: decode v2 resolve response: %w", err)
	}
	out.Format = formatOf(out)
	return out, nil
}

// formatOf classifies a resolved version. The server renders a v1 version as
// exactly one document file whose digest IS the version digest (api/v2.go
// legacyTree), while a real tree's version digest is the digest of its file
// manifest and so can never equal any single file's digest.
func formatOf(r ResolveV2Result) string {
	if len(r.Files) == 1 && r.Files[0].Type == FileDocument && r.Files[0].Digest == r.Digest {
		return FormatDocument
	}
	return FormatTree
}

// SupportsV2 reports whether this registry serves the v2 routes, probing once
// with a resolve if nothing has answered yet. The answer is a property of the
// server, so one probe serves a whole closure.
func (c *Client) SupportsV2(ctx context.Context, scope, name, ref string) bool {
	switch c.v2.Load() {
	case v2Supported:
		return true
	case v2Absent:
		return false
	}
	if _, err := c.ResolveV2(ctx, scope, name, ref); err != nil && isRouteMissing(err) {
		return false
	}
	// Any other error (not found, unauthorized, transport) says nothing about
	// v2 support; ResolveV2 has already recorded a success.
	return c.v2.Load() != v2Absent
}

// isRouteMissing reports whether an error means "this registry has no such
// route" rather than "the request failed". request synthesizes http_<status>
// for a body that is not the registry's error envelope, which is exactly what
// a router 404/405 produces.
func isRouteMissing(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status != http.StatusNotFound && apiErr.Status != http.StatusMethodNotAllowed {
		return false
	}
	// A 404 carrying a registry error code is a real answer from a real v2
	// route (the package or scope is missing), not a missing route.
	return strings.HasPrefix(apiErr.Code, "http_")
}

// DownloadFile fetches one file of a package's tree and returns the body with
// the digest advertised in the ETag ("sha256:<hex>", unquoted). The URL is
// built locally from a validated path rather than taken from the resolve
// response, so a hostile downloadUrl cannot redirect the fetch.
func (c *Client) DownloadFile(ctx context.Context, scope, name, version, filePath string) (body []byte, etagDigest string, err error) {
	if err := ValidateTreePath(filePath); err != nil {
		return nil, "", err
	}
	segs := strings.Split(filePath, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := fmt.Sprintf("%s/api/registry/v2/files/%s/%s/%s/%s", c.baseURL,
		url.PathEscape(scope), url.PathEscape(name), url.PathEscape(version), strings.Join(segs, "/"))
	body, header, err := c.getWithLimit(ctx, u, maxResourceBytes)
	if err != nil {
		return nil, "", err
	}
	etag := strings.TrimPrefix(header.Get("ETag"), "W/")
	return body, strings.Trim(etag, `"`), nil
}

// PackageExists reports whether a package already exists in the registry.
// known is false when the answer is inconclusive — a transport failure, a 5xx,
// or a permission error — so callers that must not guess (publish, deciding
// whether to send a visibility that only takes effect on creation) can refuse
// rather than proceed on a default.
func (c *Client) PackageExists(ctx context.Context, scope, name string) (exists, known bool) {
	u := fmt.Sprintf("%s/api/registry/v2/resolve/%s/%s", c.baseURL, url.PathEscape(scope), url.PathEscape(name))
	_, _, err := c.get(ctx, u)
	if err == nil {
		return true, true
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false, false
	}
	switch apiErr.Code {
	case "package_not_found", "scope_not_found":
		return false, true
	case "version_not_found", "tag_not_found", "version_yanked":
		// The package is there; it just has no resolvable version.
		return true, true
	}
	return false, false
}

// ResolveTree resolves a package to its file tree, transparently falling back
// to the v1 resolve route on a registry that has no v2 routes at all. It never
// falls back per package: a v1 version resolves through v2 as a one-file tree,
// so the only thing the fallback covers is an older registry.
func (c *Client) ResolveTree(ctx context.Context, scope, name, ref string) (ResolveV2Result, error) {
	if c.v2.Load() != v2Absent {
		res, err := c.ResolveV2(ctx, scope, name, ref)
		if err == nil {
			return res, nil
		}
		if !isRouteMissing(err) {
			return ResolveV2Result{}, err
		}
	}
	v1, err := c.Resolve(ctx, scope, name, ref)
	if err != nil {
		return ResolveV2Result{}, err
	}
	return legacyTreeOf(v1, name), nil
}

// legacyTreeOf renders a v1 resolve response the way a v2 registry would: a
// one-file tree whose single document carries the version digest, and is
// therefore digested by the v1 single-document rule.
func legacyTreeOf(v1 ResolveResult, name string) ResolveV2Result {
	return ResolveV2Result{
		Package:      v1.Package,
		Tag:          v1.Tag,
		Version:      v1.Version,
		Digest:       v1.Digest,
		Kinds:        []string{v1.Kind},
		Dependencies: v1.Dependencies,
		Files: []FileMeta{{
			Path:   name + ".yml",
			Type:   FileDocument,
			Digest: v1.Digest,
		}},
		Format: FormatDocument,
	}
}
