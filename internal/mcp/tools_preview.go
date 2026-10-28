package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// previewReadyTimeout bounds how long preview_server_start waits for the first render.
const previewReadyTimeout = 90 * time.Second

// previewSettle is how long a reused preview gets to notice a fresh file write.
const previewSettle = 250 * time.Millisecond

type previewStartInput struct {
	Name string `json:"name,omitempty" jsonschema:"metadata.name of one component, LayoutPage or artefact; the result then carries embedding_url, a page with only that document and no preview UI"`
	Kind string `json:"kind,omitempty" jsonschema:"kind of name when several documents share it, e.g. Table"`
	Port int    `json:"port,omitempty" jsonschema:"port for a new preview (default 45678); ignored when a preview already runs"`
}

type previewStartOutput struct {
	Status       string `json:"status"`
	URL          string `json:"url"`
	EmbeddingURL string `json:"embedding_url,omitempty"`
	Ready        bool   `json:"ready"`
}

func (h *handlers) registerPreviewTools(srv *mcpsdk.Server) {
	p := h.deps.Preview
	if p == nil {
		return
	}

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "preview_server_start",
		Description: "Start the live preview server for this project (`bino preview --no-open`, loopback on the machine that runs this server), or reuse the one this server already started, and wait until it has rendered. A preview started elsewhere (e.g. in a terminal) on the same port is not reused: pass another port. Returns url (the full preview UI) and, with name, embedding_url: one component, LayoutPage or artefact rendered alone, without the preview UI. Both reload when files change. Open the URL in a browser you can control (for example a built-in browser pane) to look at the result; this server cannot show it. ready=false means the first render is still running after the wait; the URL works once it finishes. A tool error carries the preview's reason: an unknown name lists the available names, a failed refresh names the cause (fix it, then call again).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in previewStartInput) (*mcpsdk.CallToolResult, previewStartOutput, error) {
		info, err := p.Start(in.Port, "--no-open")
		if err != nil {
			return errorResult(fmt.Errorf("start preview: %w", err)), previewStartOutput{}, nil
		}
		out := previewStartOutput{Status: info.Status, URL: info.URL}
		if in.Name != "" {
			out.EmbeddingURL = embeddingURL(info.URL, in.Name, in.Kind)
		}
		if info.Status == "running" {
			// A reused preview may not have seen a file the agent just wrote:
			// give the watcher time to queue it, so refresh-state reports it.
			select {
			case <-ctx.Done():
			case <-time.After(previewSettle):
			}
		}

		ready, err := waitForPreview(ctx, info.URL, out.EmbeddingURL, previewReadyTimeout, func() string { return p.Info().Status })
		out.Status = p.Info().Status
		if err != nil {
			msg := err.Error()
			if tail := strings.TrimSpace(p.Output()); tail != "" && out.Status == "stopped" {
				msg += "\npreview output:\n" + tail
			}
			return errorResult(errors.New(msg)), out, nil
		}
		out.Ready = ready
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "preview_server_stop",
		Description: "Stop the live preview server. An editor that uses the same preview loses it too.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, previewStartOutput, error) {
		p.Stop()
		return nil, previewStartOutput{Status: "stopped"}, nil
	})
}

func embeddingURL(base, name, kind string) string {
	u := base + "/__embedding/" + url.PathEscape(name)
	if kind != "" {
		u += "?kind=" + url.QueryEscape(kind)
	}
	return u
}

// waitForPreview waits until the preview at base has finished a refresh
// (/__preview/refresh-state) and, when embed is set, until that URL renders.
// It only probes once the process reports "running", so a foreign server on
// the port is never taken for ours. It returns ready=false when the timeout
// passes first, and an error when the process exits, the latest refresh
// failed, or embed answers with an error status.
func waitForPreview(ctx context.Context, base, embed string, timeout time.Duration, status func() string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second}
	refreshed := false
	for {
		switch status() {
		case "stopped":
			return false, errors.New("the preview process exited")
		case "running":
			if !refreshed {
				var err error
				if refreshed, err = previewRefreshed(ctx, client, base); err != nil {
					return false, err
				}
			}
			if refreshed {
				if embed == "" {
					return true, nil
				}
				code, body, err := previewGet(ctx, client, embed)
				switch {
				case err != nil:
				case code == http.StatusOK:
					return true, nil
				case code != http.StatusServiceUnavailable:
					return false, fmt.Errorf("%s: %d %s: %s", embed, code, http.StatusText(code), body)
				}
			}
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return false, nil
			}
			return false, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// previewRefreshed reports whether a refresh has finished. A failed latest
// refresh is an error carrying the preview's reason.
func previewRefreshed(ctx context.Context, client *http.Client, base string) (bool, error) {
	code, body, err := previewGet(ctx, client, base+"/__preview/refresh-state")
	if err != nil || code != http.StatusOK {
		return false, nil //nolint:nilerr // not reachable yet or still refreshing: keep waiting
	}
	var st struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &st) != nil {
		return false, nil
	}
	if !st.OK {
		return false, fmt.Errorf("the preview's latest refresh failed: %s", st.Error)
	}
	return true, nil
}

func previewGet(ctx context.Context, client *http.Client, target string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()                                //nolint:errcheck // read-only response
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) //nolint:errcheck // a short body only decorates an error message
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}
