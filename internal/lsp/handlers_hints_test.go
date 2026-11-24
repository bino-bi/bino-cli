package lsp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

func hintsFor(t *testing.T, s *Server, u uri.URI) map[string]string {
	t.Helper()
	hints, err := s.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{End: protocol.Position{Line: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := s.docs.Get(u)
	out := make(map[string]string)
	for _, h := range hints {
		line, _ := doc.lineText(int(h.Position.Line) + 1)
		before := []rune(line)[:h.Position.Character]
		i := strings.LastIndex(string(before), "${")
		out[string(before)[i:]] = string(h.Label.(protocol.String))
	}
	return out
}

func TestInlayHint_EnvVarsAndMasking(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("report-id = \"x\"\n[build.env]\nFROM_TOML = \"toml-value\"\n[build.env.values]\nREGION = \"from-toml\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REGION", "EU")
	t.Setenv("DB_PASSWORD", "hunter2")
	t.Setenv("API_KEY", "abc")

	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(&fakeBackend{}, log, true, dir)
	u := uri.File(filepath.Join(dir, "r.yaml"))
	s.docs.Set(u, `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: src
spec:
  type: postgres_query
  query: SELECT '${REGION}', '${FROM_TOML}', '${DB_PASSWORD}', '${API_KEY}', '${UNSET_VAR}', '\${REGION}'
`, 1)

	got := hintsFor(t, s, u)
	want := map[string]string{
		"${REGION}":      "= EU", // the environment wins over bino.toml
		"${FROM_TOML}":   "= toml-value",
		"${DB_PASSWORD}": "= ****",
		"${API_KEY}":     "= ****",
	}
	if len(got) != len(want) {
		t.Fatalf("hints = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("hint for %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestInlayHint_ParamDefaults(t *testing.T) {
	t.Setenv("REGION", "from-env")
	s := newTestServer()
	u := openDoc(t, s, `apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: commentary
  params:
    - name: REGION
      default: US
    - name: YEAR
spec:
  value: "${REGION} ${YEAR}"
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: other
spec:
  value: "${REGION}"
`)
	hints, _ := s.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{End: protocol.Position{Line: 1000}},
	})
	labels := make(map[uint32][]string)
	for _, h := range hints {
		labels[h.Position.Line] = append(labels[h.Position.Line], string(h.Label.(protocol.String)))
	}
	// The param default beats the environment; YEAR has no default.
	if got := labels[9]; len(got) != 1 || got[0] != "= US" {
		t.Errorf("param doc hints = %v, want [= US]", got)
	}
	// The second document declares no params, so the environment applies.
	if got := labels[16]; len(got) != 1 || got[0] != "= from-env" {
		t.Errorf("second doc hints = %v, want [= from-env]", got)
	}
}

func TestDocumentLink_FileReferences(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"queries/sales.sql", "assets/logo.png"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newTestServer()
	u := uri.File(filepath.Join(dir, "r.yaml"))
	s.docs.Set(u, `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query:
    $file: queries/sales.sql
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: missing
spec:
  query:
    $file: "./queries/missing.sql"
---
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  source:
    localPath: "./assets/logo.png"
`, 1)
	links, err := s.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("expected links for the two existing files, got %+v", links)
	}
	if got, want := *links[0].Target, uri.File(filepath.Join(dir, "queries/sales.sql")); got != want {
		t.Errorf("first link target = %s, want %s", got, want)
	}
	if r := links[0].Range; r.Start.Line != 6 || r.Start.Character != 11 || r.End.Character != 28 {
		t.Errorf("first link range = %+v, want line 6 chars 11-28", r)
	}
	// Quotes are not part of the link.
	if r := links[1].Range; r.Start.Line != 23 || r.Start.Character != 16 || r.End.Character != 33 {
		t.Errorf("asset link range = %+v, want line 23 chars 16-33", r)
	}
}
