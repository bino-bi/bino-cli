package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPDFRouteReachesConfiguredHandler(t *testing.T) {
	t.Parallel()

	var gotName string
	srv, err := New(Config{PDFHandler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotName = r.URL.Query().Get("name")
	})})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/__preview/pdf?kind=ReportArtefact&name=%40acme%2Fsales", nil)
	srv.httpServer.Handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotName != "@acme/sales" {
		t.Errorf("handler got name %q, want %q", gotName, "@acme/sales")
	}
}
