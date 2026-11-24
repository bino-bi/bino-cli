package render

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

const sourceLocPage = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: main
spec:
  children:
    - kind: Text
      ref: intro
    - kind: LayoutCard
      metadata:
        name: card
      spec:
        children:
          - kind: Text
            metadata:
              name: nested
            spec:
              value: nested text
`

const sourceLocTexts = `apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: other
spec:
  value: other
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: intro
spec:
  value: intro text
`

// slotAttrs returns the attributes of the slot div of the named component.
func slotAttrs(t *testing.T, html, attr, value string) map[string]string {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<div slot='[^']*'[^>]*>`).FindAllString(html, -1) {
		attrs := attrsOf(tag)
		if attrs[attr] == value {
			return attrs
		}
	}
	t.Fatalf("no slot with %s=%q in:\n%s", attr, value, html)
	return nil
}

func TestPreviewSlotsCarrySourceLine(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.yaml")
	texts := filepath.Join(dir, "texts.yaml")
	if err := os.WriteFile(page, []byte(sourceLocPage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(texts, []byte(sourceLocTexts), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	res, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := string(res.HTML)

	cases := []struct {
		attr, value, file, line string
	}{
		{"data-bino-ref", "intro", texts, "8"},   // ref: start of the second document
		{"data-bino-name", "card", page, "9"},    // inline child of the page
		{"data-bino-name", "nested", page, "14"}, // inline child of the inline card
	}
	for _, c := range cases {
		attrs := slotAttrs(t, html, c.attr, c.value)
		if got := attrs["data-bino-source-file"]; got != c.file {
			t.Errorf("%s: source file = %q, want %q", c.value, got, c.file)
		}
		if got := attrs["data-bino-source-line"]; got != c.line {
			t.Errorf("%s: source line = %q, want %q", c.value, got, c.line)
		}
	}

	build, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0")
	if err != nil {
		t.Fatalf("render build: %v", err)
	}
	if strings.Contains(string(build.HTML), "data-bino-source-") {
		t.Errorf("build output must not carry source locations")
	}
}

// Comment-only and "{}" documents do not count as loader positions, so the
// source line of a later document must skip them.
func TestPreviewSourceLineSkipsEmptyDocuments(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.yaml")
	texts := filepath.Join(dir, "texts.yaml")
	if err := os.WriteFile(page, []byte(sourceLocPage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(texts, []byte("# --- texts ---\n---\n{}\n---\n"+sourceLocTexts), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	attrs := slotAttrs(t, string(res.HTML), "data-bino-ref", "intro")
	if got := attrs["data-bino-source-line"]; got != "12" {
		t.Errorf("intro: source line = %q, want %q", got, "12")
	}
}
