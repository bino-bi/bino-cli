package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
)

func TestParseYAMLSyntaxError(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantOK   bool
		wantFile string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "decode with line",
			in:       "decode /tmp/x/draft.yaml: yaml: line 4: mapping values are not allowed in this context",
			wantOK:   true,
			wantFile: "/tmp/x/draft.yaml",
			wantLine: 4,
			wantMsg:  "YAML syntax error: mapping values are not allowed in this context",
		},
		{
			name:     "decode without line",
			in:       "decode /tmp/x/draft.yaml: yaml: did not find expected node content",
			wantOK:   true,
			wantFile: "/tmp/x/draft.yaml",
			wantLine: 0,
			wantMsg:  "YAML syntax error: did not find expected node content",
		},
		{
			name:     "windows drive colon stays in the path",
			in:       `decode C:\proj\report.yaml: yaml: line 2: found character that cannot start any token`,
			wantOK:   true,
			wantFile: `C:\proj\report.yaml`,
			wantLine: 2,
			wantMsg:  "YAML syntax error: found character that cannot start any token",
		},
		{
			name:     "type error keeps full message, anchors first line",
			in:       "decode /p/f.yaml: yaml: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
			wantOK:   true,
			wantFile: "/p/f.yaml",
			wantLine: 3,
			wantMsg:  "YAML syntax error: unmarshal errors:\n  line 3: cannot unmarshal !!str `x` into int",
		},
		{
			name:   "not a decode error",
			in:     "validate /p/f.yaml document 2: boom",
			wantOK: false,
		},
		{
			name:   "decode without yaml marker",
			in:     "decode /p/f.yaml: permission denied",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := parseYAMLSyntaxError(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if d.File != tc.wantFile {
				t.Errorf("File = %q, want %q", d.File, tc.wantFile)
			}
			if d.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", d.Line, tc.wantLine)
			}
			if d.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", d.Message, tc.wantMsg)
			}
			if d.Code != "yaml-syntax" {
				t.Errorf("Code = %q, want yaml-syntax", d.Code)
			}
		})
	}
}

// TestValidateDraft_SyntaxErrorSanitized: a draft with a YAML syntax error must
// come back positioned (line set) and must not leak the temp draft path into
// the editor-visible message.
func TestValidateDraft_SyntaxErrorSanitized(t *testing.T) {
	st, err := NewState(t.TempDir(), nil, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	diags, err := st.ValidateDraft(context.Background(), []byte("kind: Table\nspec: [\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for the syntax-broken draft")
	}
	d := diags[0]
	if d.File != "<draft>" {
		t.Errorf("File = %q, want <draft>", d.File)
	}
	if d.Code != "yaml-syntax" {
		t.Errorf("Code = %q, want yaml-syntax", d.Code)
	}
	if d.Line <= 0 {
		t.Errorf("Line = %d, want a positioned syntax error", d.Line)
	}
	if strings.Contains(d.Message, "bino-draft-") || strings.Contains(d.Message, os.TempDir()) {
		t.Errorf("message leaks the temp draft path: %q", d.Message)
	}
}

// TestValidateDraft_ForeignAndBinoGate: non-bino YAML buffers (the editor
// attaches to every yaml file) must validate to nothing; bino manifests —
// including half-typed ones without apiVersion — keep their diagnostics.
func TestValidateDraft_ForeignAndBinoGate(t *testing.T) {
	st, err := NewState(t.TempDir(), nil, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	foreign := []struct{ name, body string }{
		{"docker-compose", "services:\n  web:\n    image: nginx\n"},
		{"kubernetes", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n"},
		{"github actions", "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n"},
		{"empty buffer", ""},
		{"broken foreign", "services:\n  web: [\n"},
	}
	for _, tc := range foreign {
		t.Run("foreign/"+tc.name, func(t *testing.T) {
			diags, err := st.ValidateDraft(ctx, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 0 {
				t.Fatalf("foreign YAML must produce no bino diagnostics, got %+v", diags)
			}
		})
	}

	t.Run("bino kind without apiVersion is validated", func(t *testing.T) {
		diags, err := st.ValidateDraft(ctx, []byte("kind: DataSet\nmetadata:\n  name: x\nspec:\n  query: select 1\n"))
		if err != nil {
			t.Fatal(err)
		}
		var sawMissingAPIVersion bool
		for _, d := range diags {
			if strings.Contains(d.Message, "apiVersion") {
				sawMissingAPIVersion = true
			}
		}
		if !sawMissingAPIVersion {
			t.Fatalf("a half-typed bino manifest must keep its missing-apiVersion diagnostic, got %+v", diags)
		}
	})

	t.Run("null value gets friendly message and hint", func(t *testing.T) {
		diags, err := st.ValidateDraft(ctx, []byte("apiVersion: bino.bi/v1alpha1\nkind:\nmetadata:\n  name: x\nspec: {}\n"))
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, d := range diags {
			if !strings.Contains(d.Message, "no value provided (expected string)") {
				continue
			}
			found = true
			if d.Hint == "" {
				t.Errorf("null-value diagnostic must carry a hint, got %+v", d)
			}
		}
		if !found {
			t.Fatalf("expected the friendly null-value message, got %+v", diags)
		}
	})
}

const refParamsFixture = `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Report
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: rev_table
  params:
    - name: title
      type: string
    - name: mandatory
      type: string
      required: true
spec:
  dataset: sales
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page_one
spec:
  children:
    - kind: Table
      ref: rev_table
      params:
        title: Revenue
    - kind: Text
      ref: ghost_text
`

// TestValidateDocs_RefParamsAndSeverity: the daemon's lint document conversion
// must carry metadata.params (the ref-params rule is inert without the
// declarations — and worse, mis-fires "unknown param" for legitimately passed
// ones), and lint findings must keep their per-rule severity instead of a
// hardcoded "warning".
func TestValidateDocs_RefParamsAndSeverity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(refParamsFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewState(dir, nil, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	st.mu.Lock()
	diags := st.validateDocs(context.Background())
	st.mu.Unlock()

	var missingRequired, missingRef bool
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown param") {
			t.Errorf("declared param flagged as unknown — declarations were dropped: %q", d.Message)
		}
		if d.Code == "ref-params" && strings.Contains(d.Message, `missing required param "mandatory"`) {
			missingRequired = true
		}
		if d.Code == "missing-required-reference" {
			missingRef = true
			if d.Severity != "error" {
				t.Errorf("missing-required-reference severity = %q, want error", d.Severity)
			}
		}
	}
	if !missingRequired {
		t.Errorf("expected a ref-params finding for the missing required param, got %+v", diags)
	}
	if !missingRef {
		t.Errorf("expected a missing-required-reference finding for the dangling ref, got %+v", diags)
	}
}
