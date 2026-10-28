package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
)

func TestEmbeddingURLEscapesName(t *testing.T) {
	got := embeddingURL("http://127.0.0.1:45678", "@acme/kit/revenue", "Table")
	want := "http://127.0.0.1:45678/__embedding/@acme%2Fkit%2Frevenue?kind=Table"
	if got != want {
		t.Errorf("embeddingURL = %q, want %q", got, want)
	}
}

// fakePreview serves refresh-state from state() ("" = still refreshing,
// "ok", or an error text) and the embedding route from embed.
func fakePreview(t *testing.T, state func() string, embed http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/__preview/refresh-state", func(w http.ResponseWriter, _ *http.Request) {
		switch st := state(); st {
		case "":
			http.Error(w, "first refresh still running", http.StatusServiceUnavailable)
		case "ok":
			fmt.Fprint(w, `{"ok":true}`)
		default:
			fmt.Fprintf(w, `{"ok":false,"error":%q}`, st)
		}
	})
	if embed != nil {
		mux.HandleFunc("/__embedding/{name}", embed)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func running() string { return "running" }

func TestWaitForPreviewWaitsForFirstRefresh(t *testing.T) {
	var polls atomic.Int32
	srv := fakePreview(t, func() string {
		if polls.Add(1) < 3 {
			return ""
		}
		return "ok"
	}, nil)

	ready, err := waitForPreview(context.Background(), srv.URL, "", 10*time.Second, running)
	if err != nil || !ready {
		t.Fatalf("ready = %v, err = %v; want ready", ready, err)
	}
	if polls.Load() != 3 {
		t.Errorf("refresh-state polls = %d, want 3", polls.Load())
	}
}

func TestWaitForPreviewReportsFailedRefresh(t *testing.T) {
	srv := fakePreview(t, func() string { return "bad yaml" }, nil)

	_, err := waitForPreview(context.Background(), srv.URL, "", 10*time.Second, running)
	if err == nil || !strings.Contains(err.Error(), "bad yaml") {
		t.Fatalf("err = %v, want the refresh error", err)
	}
}

func TestWaitForPreviewWaitsOutBootingEmbedding(t *testing.T) {
	var calls atomic.Int32
	srv := fakePreview(t, func() string { return "ok" }, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "preview still booting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	ready, err := waitForPreview(context.Background(), srv.URL, srv.URL+"/__embedding/x", 10*time.Second, running)
	if err != nil || !ready {
		t.Fatalf("ready = %v, err = %v; want ready", ready, err)
	}
	if calls.Load() != 3 {
		t.Errorf("embedding calls = %d, want 3", calls.Load())
	}
}

// An unknown name answers 404; the agent must get the server's reason.
func TestWaitForPreviewReportsErrorStatus(t *testing.T) {
	srv := fakePreview(t, func() string { return "ok" }, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `no document named "nope"`, http.StatusNotFound)
	})

	_, err := waitForPreview(context.Background(), srv.URL, srv.URL+"/__embedding/nope", 10*time.Second, running)
	if err == nil || !strings.Contains(err.Error(), `no document named "nope"`) {
		t.Fatalf("err = %v, want the 404 body", err)
	}
}

// Until our process reports running, whatever answers on the port belongs to
// someone else and must not be probed.
func TestWaitForPreviewIgnoresForeignServerWhileStarting(t *testing.T) {
	var calls atomic.Int32
	srv := fakePreview(t, func() string { calls.Add(1); return "ok" }, nil)

	ready, err := waitForPreview(context.Background(), srv.URL, "", time.Second, func() string { return "starting" })
	if err != nil || ready {
		t.Fatalf("ready = %v, err = %v; want not ready, no error", ready, err)
	}
	if calls.Load() != 0 {
		t.Errorf("foreign server probed %d time(s)", calls.Load())
	}
}

func TestWaitForPreviewFailsWhenProcessExits(t *testing.T) {
	_, err := waitForPreview(context.Background(), "http://127.0.0.1:1", "", time.Second, func() string { return "stopped" })
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("err = %v, want an exit error", err)
	}
}

func TestPreviewToolsRequirePreviewDep(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, root)

	for _, preview := range []*daemon.PreviewProcess{nil, daemon.NewPreviewProcess(root, nil)} {
		server := NewServer(Deps{State: state, Preview: preview})
		clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatalf("server connect: %v", err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
		cs, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatalf("client connect: %v", err)
		}
		t.Cleanup(func() { _ = cs.Close() })

		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
		}
		want := preview != nil
		for _, name := range []string{"preview_server_start", "preview_server_stop"} {
			if slices.Contains(names, name) != want {
				t.Errorf("Preview set=%v: %s listed=%v", want, name, !want)
			}
		}
	}
}
