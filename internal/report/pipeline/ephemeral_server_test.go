package pipeline

import (
	"context"
	"io"
	"net/http"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/render"
)

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestEphemeralServerServesPluginAssets(t *testing.T) {
	b := &Builder{PluginOptions: &render.PluginOptions{
		AssetHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/plugins/p/a.js" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte("plugin js"))
		}),
	}}

	srv, err := newEphemeralServer(context.Background(), t.TempDir(), logx.Nop(), []byte("<html>report</html>"), nil, nil, b.pluginAssetHandler())
	if err != nil {
		t.Fatalf("newEphemeralServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if code, body := getBody(t, srv.URL()+"/plugins/p/a.js"); code != http.StatusOK || body != "plugin js" {
		t.Errorf("plugin asset = %d %q, want 200 %q", code, body, "plugin js")
	}
	if code, body := getBody(t, srv.URL()+"/"); code != http.StatusOK || body != "<html>report</html>" {
		t.Errorf("page = %d %q, want the report HTML", code, body)
	}
}

func TestBuilderPluginAssetHandlerNil(t *testing.T) {
	if h := (&Builder{}).pluginAssetHandler(); h != nil {
		t.Errorf("pluginAssetHandler() = %v, want nil without PluginOptions", h)
	}
}
