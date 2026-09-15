package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"sort"
)

// PublishManifest is the JSON "manifest" part of a v2 publish request: the
// package identity, everything the version row needs, and the file list whose
// sorted canonical JSON is the version digest.
type PublishManifest struct {
	Name         string      `json:"name"`
	Bump         string      `json:"bump"`
	Visibility   string      `json:"visibility,omitempty"`
	Title        string      `json:"title,omitempty"`
	Description  string      `json:"description,omitempty"`
	Category     string      `json:"category,omitempty"`
	CompatEngine string      `json:"compatEngine,omitempty"`
	CompatCli    string      `json:"compatCli,omitempty"`
	Preview      string      `json:"preview,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	Dependencies []string    `json:"dependencies,omitempty"`
	DryRun       bool        `json:"dryRun,omitempty"`
	Files        []FileEntry `json:"files"`
}

// PublishFile is one file to upload: its tree path and a way to open its
// bytes. Open is called exactly once, while the multipart body is streamed.
type PublishFile struct {
	Path string
	Open func() (io.ReadCloser, error)
}

// PublishResult is a successful publish. Unchanged marks the idempotent
// replay of an already-published tree: nothing was written and Version names
// the version that already carries it.
type PublishResult struct {
	Package   string      `json:"package"`
	Version   string      `json:"version"`
	Digest    string      `json:"digest"`
	Tag       string      `json:"tag"`
	Kinds     []string    `json:"kinds"`
	Unchanged bool        `json:"unchanged"`
	Files     []FileEntry `json:"files"`
	Warnings  []Finding   `json:"warnings"`
}

// DryRunResult is what a dry run gets back. It is a different shape from
// PublishResult on the wire — no package, no unchanged — because nothing was
// minted.
type DryRunResult struct {
	DryRun   bool        `json:"dryRun"`
	Digest   string      `json:"digest"`
	Version  string      `json:"version"`
	Files    []FileEntry `json:"files"`
	Warnings []Finding   `json:"warnings"`
}

// Finding is one diagnostic from the registry's validation gate. The fields
// are those the CLI renders; unknown ones are ignored.
type Finding struct {
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Document string `json:"document"`
}

// GateDetail is one item of a schema_invalid response's details array. It
// names the bino and engine the registry's gate ran, so a rejection caused by
// version skew between the author's CLI and the registry's is visible rather
// than mysterious.
type GateDetail struct {
	Bino     string    `json:"bino"`
	Engine   string    `json:"engine"`
	Findings []Finding `json:"findings"`
}

// GateDetails decodes the gate diagnostics from a schema_invalid error,
// returning nil when the error carries none or reports them in a shape this
// CLI does not know.
func (e *APIError) GateDetails() []GateDetail {
	if len(e.Details) == 0 {
		return nil
	}
	var out []GateDetail
	if err := json.Unmarshal(e.Details, &out); err != nil {
		return nil
	}
	return out
}

// Publish uploads a package tree and mints a version. The manifest's DryRun
// field selects validation-only mode, which answers with a different body —
// use PublishDryRun for that.
func (c *Client) Publish(ctx context.Context, m PublishManifest, files []PublishFile) (PublishResult, error) {
	var out PublishResult
	body, err := c.publish(ctx, m, files)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("registry: decode publish response: %w", err)
	}
	return out, nil
}

// PublishDryRun runs the registry's validation gate over the tree without
// minting anything, reporting the version and digest the publish would have
// produced.
func (c *Client) PublishDryRun(ctx context.Context, m PublishManifest, files []PublishFile) (DryRunResult, error) {
	m.DryRun = true
	var out DryRunResult
	body, err := c.publish(ctx, m, files)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("registry: decode dry-run response: %w", err)
	}
	return out, nil
}

// publish streams the multipart body: one "manifest" form value carrying the
// JSON, then one file part per entry whose part NAME is the file's tree path.
// The body is produced as it is written, so a large tree is never buffered.
func (c *Client) publish(ctx context.Context, m PublishManifest, files []PublishFile) ([]byte, error) {
	auth, err := c.bearer(ctx)
	if err != nil {
		return nil, err
	}
	if auth == "" {
		return nil, fmt.Errorf("registry: publishing requires authentication — run 'bino registry login'")
	}
	manifestJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("registry: encode publish manifest: %w", err)
	}

	ordered := make([]PublishFile, len(files))
	copy(ordered, files)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(writePublishBody(mw, manifestJSON, ordered)) //nolint:errcheck // CloseWithError never fails
	}()
	defer pr.Close() //nolint:errcheck // best-effort; the goroutine owns the write half

	u := c.baseURL + "/api/registry/v2/publish"
	return c.requestStream(ctx, "POST", u, mw.FormDataContentType(), auth, pr, publishTimeout)
}

// writePublishBody writes the whole multipart body and closes the writer. Any
// error it returns is delivered to the reading half, so a file that vanishes
// mid-upload aborts the request instead of truncating it silently.
func writePublishBody(mw *multipart.Writer, manifestJSON []byte, files []PublishFile) error {
	if err := mw.WriteField("manifest", string(manifestJSON)); err != nil {
		return err
	}
	for _, f := range files {
		if err := writePublishFile(mw, f); err != nil {
			return err
		}
	}
	return mw.Close()
}

func writePublishFile(mw *multipart.Writer, f PublishFile) error {
	// The part name is the tree path; the filename only has to be non-empty
	// for net/http to route the part to the file map rather than the value map.
	w, err := mw.CreateFormFile(f.Path, path.Base(f.Path))
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Path, err)
	}
	defer rc.Close() //nolint:errcheck // read-only handle
	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("upload %s: %w", f.Path, err)
	}
	return nil
}
