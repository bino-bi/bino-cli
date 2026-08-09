package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateArtefactLayoutPages is the one wizard function that rewrites an
// existing user file (multi-doc reparse + re-encode). A decode error mid-file
// used to be swallowed (`break`), so a YAML syntax error in document 2
// silently truncated every following document on the rewrite.

func writeArtefactFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artefacts.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestUpdateArtefactLayoutPagesSyntaxErrorLeavesFileUntouched(t *testing.T) {
	// Document 1 parses (and is a ReportArtefact, so the rewrite would
	// proceed); document 2 has a YAML syntax error; document 3 would be
	// silently dropped by a rewrite that stops at the first decode error.
	fixture := strings.Join([]string{
		"apiVersion: bino.bi/v1alpha1",
		"kind: ReportArtefact",
		"metadata:",
		"  name: monthly_report",
		"spec:",
		"  filename: out.pdf",
		"---",
		"kind: LayoutPage",
		"metadata:",
		"  name: [broken",
		"---",
		"kind: LayoutPage",
		"metadata:",
		"  name: survivor_page",
		"",
	}, "\n")
	path := writeArtefactFixture(t, fixture)

	err := updateArtefactLayoutPages(path, LayoutPageRefData{Page: "new_page"})
	if err == nil {
		t.Fatal("expected an error for a file with a YAML syntax error")
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read file back: %v", readErr)
	}
	if string(after) != fixture {
		t.Errorf("file was modified although it could not be fully parsed:\n--- before ---\n%s\n--- after ---\n%s", fixture, after)
	}
}

func TestUpdateArtefactLayoutPagesRewrite(t *testing.T) {
	t.Run("appends the page and keeps unrelated documents and keys", func(t *testing.T) {
		fixture := strings.Join([]string{
			"apiVersion: bino.bi/v1alpha1",
			"kind: ReportArtefact",
			"metadata:",
			"  name: monthly_report",
			"  description: keep me",
			"spec:",
			"  filename: out.pdf",
			"  layoutPages:",
			"    - existing_page",
			"---",
			"apiVersion: bino.bi/v1alpha1",
			"kind: LayoutPage",
			"metadata:",
			"  name: sibling_page",
			"  description: unrelated document",
			"spec:",
			"  children: []",
			"",
		}, "\n")
		path := writeArtefactFixture(t, fixture)

		if err := updateArtefactLayoutPages(path, LayoutPageRefData{Page: "new_page"}); err != nil {
			t.Fatalf("update: %v", err)
		}

		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file back: %v", err)
		}
		got := string(after)
		// The rewrite re-encodes, so exact formatting is not guaranteed —
		// but every key and both documents must survive, plus the new ref.
		for _, want := range []string{
			"kind: ReportArtefact",
			"name: monthly_report",
			"description: keep me",
			"filename: out.pdf",
			"existing_page",
			"new_page",
			"kind: LayoutPage",
			"name: sibling_page",
			"description: unrelated document",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("rewritten file lost %q:\n%s", want, got)
			}
		}
	})

	t.Run("params use the object form", func(t *testing.T) {
		fixture := strings.Join([]string{
			"kind: ReportArtefact",
			"metadata:",
			"  name: param_report",
			"spec:",
			"  filename: out.pdf",
			"",
		}, "\n")
		path := writeArtefactFixture(t, fixture)

		ref := LayoutPageRefData{Page: "regional_page", Params: map[string]string{"region": "emea"}}
		if err := updateArtefactLayoutPages(path, ref); err != nil {
			t.Fatalf("update: %v", err)
		}

		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file back: %v", err)
		}
		got := string(after)
		for _, want := range []string{"page: regional_page", "region: emea"} {
			if !strings.Contains(got, want) {
				t.Errorf("rewritten file missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("no ReportArtefact leaves the file untouched", func(t *testing.T) {
		fixture := strings.Join([]string{
			"kind: LayoutPage",
			"metadata:",
			"  name: lonely_page",
			"spec:",
			"  children: []",
			"",
		}, "\n")
		path := writeArtefactFixture(t, fixture)

		if err := updateArtefactLayoutPages(path, LayoutPageRefData{Page: "new_page"}); err == nil {
			t.Fatal("expected an error when the file has no ReportArtefact")
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file back: %v", err)
		}
		if string(after) != fixture {
			t.Errorf("file was modified although no ReportArtefact was found:\n%s", after)
		}
	})
}

func TestDetectPageParams(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pages.yaml")
	content := strings.Join([]string{
		"kind: LayoutPage",
		"metadata:",
		"  name: plain_page",
		"spec:",
		"  children: []",
		"---",
		"kind: LayoutPage",
		"metadata:",
		"  name: regional_page",
		"  params:",
		"    - name: REGION",
		"      type: select",
		"      required: true",
		"      options:",
		"        items:",
		"          - value: emea",
		"          - value: apac",
		"    - name: YEAR",
		"      type: number",
		"spec:",
		"  children: []",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	manifests := []ManifestInfo{
		{Kind: "LayoutPage", Name: "plain_page", File: file, Position: 1},
		{Kind: "LayoutPage", Name: "regional_page", File: file, Position: 2},
	}

	t.Run("reads params from the right document", func(t *testing.T) {
		params := detectPageParams("regional_page", manifests)
		if len(params) != 2 {
			t.Fatalf("params = %+v, want 2 entries", params)
		}
		if params[0].Name != "REGION" || params[0].Type != "select" || !params[0].Required {
			t.Errorf("params[0] = %+v, want REGION/select/required", params[0])
		}
		if params[0].Options == nil || len(params[0].Options.Items) != 2 {
			t.Errorf("params[0].Options = %+v, want 2 select items", params[0].Options)
		}
		if params[1].Name != "YEAR" || params[1].Type != "number" {
			t.Errorf("params[1] = %+v, want YEAR/number", params[1])
		}
	})

	t.Run("page without params yields nil", func(t *testing.T) {
		if params := detectPageParams("plain_page", manifests); params != nil {
			t.Errorf("params = %+v, want nil", params)
		}
	})

	t.Run("unknown page yields nil", func(t *testing.T) {
		if params := detectPageParams("missing_page", manifests); params != nil {
			t.Errorf("params = %+v, want nil", params)
		}
	})
}
