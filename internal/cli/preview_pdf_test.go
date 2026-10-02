package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/pipeline"
)

// minimalPDF returns a valid one-page PDF without content.
func minimalPDF() []byte {
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> >>\nendobj\n",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objs))
	for _, obj := range objs {
		offsets = append(offsets, buf.Len())
		buf.WriteString(obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

type fakeRender func(ctx context.Context, kind, name, outDir string) (string, error)

func newTestPDFHandler(render fakeRender) *previewPDFHandler {
	return &previewPDFHandler{
		base:   context.Background(),
		mu:     &sync.Mutex{},
		logger: logx.Nop(),
		render: render,
	}
}

// writesPDF is a render that produces a valid PDF.
func writesPDF(_ context.Context, _, _, outDir string) (string, error) {
	path := filepath.Join(outDir, previewPDFFilename)
	return path, os.WriteFile(path, minimalPDF(), 0o600)
}

func postPDF(ctx context.Context, t *testing.T, h http.Handler, query string, header http.Header) (int, previewPDFResponse) {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/__preview/pdf"+query, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var resp previewPDFResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	return rec.Code, resp
}

const reportQuery = "?kind=ReportArtefact&name=sales"

func TestPreviewPDFHandlerRejectsBadRequests(t *testing.T) {
	t.Parallel()
	h := newTestPDFHandler(func(context.Context, string, string, string) (string, error) {
		t.Error("render must not run for a rejected request")
		return "", nil
	})

	tests := []struct {
		name   string
		query  string
		header http.Header
		want   int
	}{
		{"unknown kind", "?kind=LayoutPage&name=sales", nil, http.StatusBadRequest},
		{"missing name", "?kind=ReportArtefact", nil, http.StatusBadRequest},
		{"cross-site", reportQuery, http.Header{"Sec-Fetch-Site": {"cross-site"}}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := postPDF(context.Background(), t, h, tt.query, tt.header)
			if status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
			if resp.Error == "" || resp.PDF != nil {
				t.Errorf("want an error and no PDF, got %+v", resp)
			}
		})
	}
}

// The tests that reach the watermark step are not parallel: pdfcpu loads its
// configuration on first use without locking. The handler's mutex covers this
// outside of tests.
func TestPreviewPDFHandlerServesWatermarkedPDF(t *testing.T) {
	var outDir string
	h := newTestPDFHandler(func(ctx context.Context, kind, name, dir string) (string, error) {
		outDir = dir
		if kind != previewPDFKindDocument || name != "@acme/handbook" {
			t.Errorf("render got kind=%q name=%q", kind, name)
		}
		logx.FromContext(ctx).Channel("datasource").Errorf("costs (dataset): execute: boom")
		return writesPDF(ctx, kind, name, dir)
	})

	status, resp := postPDF(context.Background(), t, h, "?kind=DocumentArtefact&name=%40acme%2Fhandbook",
		http.Header{"Sec-Fetch-Site": {"same-origin"}})

	if status != http.StatusOK || resp.Error != "" {
		t.Fatalf("status = %d, error = %q", status, resp.Error)
	}
	if !bytes.HasPrefix(resp.PDF, []byte("%PDF-")) {
		t.Fatalf("body is not a PDF: %q", resp.PDF)
	}
	if bytes.Equal(resp.PDF, minimalPDF()) {
		t.Error("PDF was served without the watermark step")
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "costs (dataset): execute: boom" {
		t.Errorf("warnings = %q", resp.Warnings)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Errorf("temp dir %s was not removed", outDir)
	}
}

func TestPreviewPDFHandlerErrors(t *testing.T) {
	tests := []struct {
		name       string
		render     fakeRender
		wantStatus int
		wantError  string
		wantWarn   bool
	}{
		{
			name: "not found",
			render: func(context.Context, string, string, string) (string, error) {
				return "", httpserver.NewHTTPError(http.StatusNotFound, `no ReportArtefact named "sales"`)
			},
			wantStatus: http.StatusNotFound,
			wantError:  `no ReportArtefact named "sales"`,
		},
		{
			name: "render failure keeps warnings",
			render: func(ctx context.Context, _, _, _ string) (string, error) {
				logx.FromContext(ctx).Warnf("unresolved environment variable TOKEN in a.yaml")
				return "", errors.New("artefact sales: render: boom")
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "artefact sales: render: boom",
			wantWarn:   true,
		},
		{
			name: "chrome missing",
			render: func(context.Context, string, string, string) (string, error) {
				return "", fmt.Errorf("artefact sales: load x: %w", &exec.Error{Name: "google-chrome", Err: exec.ErrNotFound})
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "Chrome was not found",
		},
		{
			name: "timeout",
			render: func(context.Context, string, string, string) (string, error) {
				return "", fmt.Errorf("artefact sales: generate pdf: %w", context.DeadlineExceeded)
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "Rendering timed out",
		},
		{
			// The watermark step fails, so the file must not be served.
			name: "output is not a pdf",
			render: func(_ context.Context, _, _, outDir string) (string, error) {
				path := filepath.Join(outDir, previewPDFFilename)
				return path, os.WriteFile(path, []byte("not a pdf"), 0o600)
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "stamp watermark",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := postPDF(context.Background(), t, newTestPDFHandler(tt.render), reportQuery, nil)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if !strings.Contains(resp.Error, tt.wantError) {
				t.Errorf("error = %q, want it to contain %q", resp.Error, tt.wantError)
			}
			if resp.PDF != nil {
				t.Error("an error response must not carry a PDF")
			}
			if tt.wantWarn != (len(resp.Warnings) > 0) {
				t.Errorf("warnings = %q, wantWarn = %v", resp.Warnings, tt.wantWarn)
			}
		})
	}
}

// A closed modal and a preview shutdown must both stop the build.
func TestPreviewPDFHandlerCancelsRender(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"request", "base"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			reqCtx, cancelReq := context.WithCancel(context.Background())
			defer cancelReq()
			baseCtx, cancelBase := context.WithCancel(context.Background())
			defer cancelBase()

			h := newTestPDFHandler(func(ctx context.Context, _, _, _ string) (string, error) {
				if source == "request" {
					cancelReq()
				} else {
					cancelBase()
				}
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(5 * time.Second):
					t.Error("render context was not canceled")
					return "", nil
				}
			})
			h.base = baseCtx

			status, resp := postPDF(reqCtx, t, h, reportQuery, nil)
			if status != http.StatusServiceUnavailable || resp.PDF != nil {
				t.Errorf("status = %d, pdf = %v, want 503 without a PDF", status, resp.PDF != nil)
			}
		})
	}
}

// The build shares the refresh mutex, so it waits for a running refresh.
func TestPreviewPDFHandlerWaitsForMutex(t *testing.T) {
	started := make(chan struct{})
	h := newTestPDFHandler(func(ctx context.Context, kind, name, outDir string) (string, error) {
		close(started)
		return writesPDF(ctx, kind, name, outDir)
	})

	h.mu.Lock()
	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__preview/pdf"+reportQuery, nil))
		done <- rec.Code
	}()

	select {
	case <-started:
		t.Fatal("render started while the mutex was held")
	case <-time.After(100 * time.Millisecond):
	}
	h.mu.Unlock()

	if status := <-done; status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

// renderArtefact fails before any build for a broken project or an unknown artefact.
func TestPreviewPDFRenderArtefactLookup(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("..", "report", "pipeline", "testdata", "sample-bundle")

	t.Run("broken manifest", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("apiVersion: bino.bi/v1alpha1\nkind: [\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		h := newTestPDFHandler(nil)
		h.builder = pipeline.Builder{Workdir: dir}

		_, err := h.renderArtefact(context.Background(), previewPDFKindReport, "sales", t.TempDir())
		var httpErr *httpserver.HTTPError
		if err == nil || errors.As(err, &httpErr) {
			t.Fatalf("want a load error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		kind    string
		art     string
		exclude []string
	}{
		{"unknown name", previewPDFKindReport, "missing", nil},
		{"wrong kind", previewPDFKindDocument, "sample-report", nil},
		{"filtered out", previewPDFKindReport, "sample-report", []string{"sample-report"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestPDFHandler(nil)
			h.builder = pipeline.Builder{Workdir: bundle}
			h.filter = pipeline.FilterOptions{Exclude: tt.exclude}

			_, err := h.renderArtefact(context.Background(), tt.kind, tt.art, t.TempDir())
			var httpErr *httpserver.HTTPError
			if !errors.As(err, &httpErr) || httpErr.Code != http.StatusNotFound {
				t.Fatalf("want a 404 HTTPError, got %v", err)
			}
		})
	}
}

// recordingLogger records what reaches the terminal logger.
type recordingLogger struct {
	logx.Logger
	lines *[]string
}

func (l recordingLogger) Infof(format string, args ...any) {
	*l.lines = append(*l.lines, "info: "+fmt.Sprintf(format, args...))
}

func (l recordingLogger) Warnf(format string, args ...any) {
	*l.lines = append(*l.lines, "warn: "+fmt.Sprintf(format, args...))
}

func (l recordingLogger) Errorf(format string, args ...any) {
	*l.lines = append(*l.lines, "error: "+fmt.Sprintf(format, args...))
}

func (l recordingLogger) Channel(string) logx.Logger { return l }

func TestWarnCapture(t *testing.T) {
	t.Parallel()
	var terminal []string
	c := warnCapture{Logger: recordingLogger{Logger: logx.Nop(), lines: &terminal}, sink: &warnSink{}}

	c.Warnf("a %d", 1)
	c.Errorf("b")
	c.Channel("chrome").Warnf("c")
	c.Channel("render").Warnf("a %d", 1) // repeat
	c.Channel("pdf").Infof("building")

	got := c.sink.snapshot()
	want := []string{"a 1", "b", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("captured = %q, want %q", got, want)
	}
	// Warnings and errors are for the UI; only the other levels pass through.
	if len(terminal) != 1 || terminal[0] != "info: building" {
		t.Errorf("terminal got %q, want only the info line", terminal)
	}
}
