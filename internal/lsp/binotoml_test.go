package lsp

import (
	"context"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestTOMLDiagnostics(t *testing.T) {
	cases := []struct {
		name, text, code string
		sev              protocol.DiagnosticSeverity
		want             [3]uint32
		msg              string
	}{
		{"unknown key", "report-id = \"x\"\n[lint]\ndisabel = [\"a\"]\n", "toml-unknown-key", protocol.DiagnosticSeverityWarning, [3]uint32{2, 0, 7}, "unknown key 'lint.disabel'"},
		{"env var must be a string", "[build.env]\nOK = \"x\"\nFOO = 1\n", "toml-type", protocol.DiagnosticSeverityError, [3]uint32{2, 0, 3}, "'build.env.FOO' must be a string"},
		{"wrong type", "report-id = \"x\"\n[lint]\ndisable = \"a\" # one rule\n", "toml-type", protocol.DiagnosticSeverityError, [3]uint32{2, 10, 13}, "'lint.disable' must be an array, not a TOML string"},
		{"unknown trust key", "[trust]\nrequire = \"warn\"\n[[trust.signer]]\nscope = \"@a/*\"\nissuer = \"x\"\n", "toml-unknown-key", protocol.DiagnosticSeverityError, [3]uint32{4, 0, 6}, "unknown key 'trust.signer.issuer': commands that apply the trust policy fail on it"},
		{"syntax", "report-id = \n", "toml-syntax", protocol.DiagnosticSeverityError, [3]uint32{0, 12, 12}, "TOML syntax error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tomlDiagnostics(&Document{Text: c.text})
			if len(got) != 1 {
				t.Fatalf("got %d diagnostics: %+v", len(got), got)
			}
			d := got[0]
			if diagCode(d.Code) != c.code || d.Severity != c.sev {
				t.Errorf("code/severity = %s/%v, want %s/%v", diagCode(d.Code), d.Severity, c.code, c.sev)
			}
			if !strings.Contains(diagMessage(d.Message), c.msg) {
				t.Errorf("message %q does not contain %q", diagMessage(d.Message), c.msg)
			}
			if s := span(d.Range); s != c.want {
				t.Errorf("range = %v, want %v", s, c.want)
			}
		})
	}
	if got := tomlDiagnostics(&Document{Text: "report-id = \"x\"\ntemplate = \"t\"\n[plugins.sci]\nversion = \"1\"\n[preview.env]\nA = \"1\"\n[preview.env.values]\nB = \"2\"\n"}); got == nil || len(got) != 0 {
		t.Errorf("valid bino.toml must give an empty, non-nil list, got %+v", got)
	}
}

// TestTOMLDidOpenPublishes: a bino.toml buffer is checked without the YAML
// draft validator, and an unknown key reaches the editor.
func TestTOMLDidOpenPublishes(t *testing.T) {
	s := newTestServer()
	s.ctx = context.Background()
	client := &capturingClient{}
	s.client = client
	u := uri.File("/proj/bino.toml")
	err := s.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: u, LanguageID: "plaintext", Version: 1, Text: "report-id = \"x\"\nreportid = \"y\"\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if client.count() != 1 {
		t.Fatalf("expected 1 publish, got %d", client.count())
	}
	p := client.last()
	if p.URI != u || len(p.Diagnostics) != 1 || diagCode(p.Diagnostics[0].Code) != "toml-unknown-key" {
		t.Fatalf("unexpected publish: %+v", p)
	}
}

func tomlLabels(t *testing.T, text string, line, char uint32) []string {
	t.Helper()
	s := newTestServer()
	u := uri.File("/proj/bino.toml")
	s.docs.Set(u, text, 1)
	res, err := s.Completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: line, Character: char},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return completionLabels(t, res)
}

func TestTOMLCompletion(t *testing.T) {
	root := tomlLabels(t, "rep", 0, 3)
	for _, want := range []string{"report-id", "engine-version", "template"} {
		if !contains(root, want) {
			t.Errorf("root keys %v miss %q", root, want)
		}
	}
	if contains(root, "registry") {
		t.Error("tables must not be offered as keys")
	}
	tables := tomlLabels(t, "[bu", 0, 3)
	for _, want := range []string{"build", "build.env", "build.env.values", "lint", "registry"} {
		if !contains(tables, want) {
			t.Errorf("tables %v miss %q", tables, want)
		}
	}
	if got := tomlLabels(t, "[registry]\n", 1, 0); len(got) != 2 || !contains(got, "url") || !contains(got, "token") {
		t.Errorf("[registry] keys = %v, want url, token", got)
	}
	if got := tomlLabels(t, "[plugins.sci]\nv", 1, 1); !contains(got, "version") || !contains(got, "hook_timeout") {
		t.Errorf("[plugins.<name>] keys = %v", got)
	}
	if got := tomlLabels(t, "[tr", 0, 3); !contains(got, "trust") || !contains(got, "trust.signer") {
		t.Errorf("tables %v miss trust, trust.signer", got)
	}
	if got := tomlLabels(t, "[[trust.signer]]\n", 1, 0); !contains(got, "scope") || !contains(got, "oidc-issuer") || !contains(got, "key-file") {
		t.Errorf("[[trust.signer]] keys = %v", got)
	}
	if got := tomlLabels(t, "[trust]\n", 1, 0); len(got) != 1 || got[0] != "require" {
		t.Errorf("[trust] keys = %v, want [require]", got)
	}
	if got := tomlLabels(t, "[lint]\ndisable = ", 1, 10); len(got) != 0 {
		t.Errorf("no completion after '=', got %v", got)
	}
}
