package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAssetRefRule runs the rule and returns the paths of the findings it produced.
func runAssetRefRule(t *testing.T, docs []Document) []string {
	t.Helper()
	findings := assetReferenceUndefined.Check(context.Background(), docs)
	paths := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.RuleID != "asset-reference-undefined" {
			t.Errorf("RuleID = %q, want asset-reference-undefined", f.RuleID)
		}
		paths = append(paths, f.Path)
	}
	return paths
}

// imageAssetDoc builds an Asset document backed by a remote URL, so it declares
// a resolvable name without touching the filesystem.
func imageAssetDoc(name, assetType string) Document {
	spec := map[string]any{
		"type":      assetType,
		"mediaType": "image/png",
		"source":    map[string]any{"remoteURL": "https://example.com/" + name + ".png"},
	}
	return Document{
		File:     "/project/assets.yaml",
		Position: 1,
		Kind:     "Asset",
		Name:     name,
		Raw:      rawDoc("Asset", name, spec),
	}
}

func componentDoc(kind, name string, spec map[string]any) Document {
	return Document{
		File:     "/project/page.yaml",
		Position: 1,
		Kind:     kind,
		Name:     name,
		Raw:      rawDoc(kind, name, spec),
	}
}

func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d findings %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding[%d] path = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAssetReferenceUndefined_ImageFields(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		field     string
		value     string
		assetKind string // asset type declared under the name "logo"; empty means no Asset at all
		wantPaths []string
	}{
		{name: "bare name matching an Asset", kind: "LayoutPage", field: "messageImage", value: "logo", assetKind: "image"},
		{name: "asset: prefix matching an Asset", kind: "LayoutPage", field: "messageImage", value: "asset:logo", assetKind: "image"},
		{
			name: "asset: prefix with no Asset", kind: "LayoutPage", field: "messageImage", value: "asset:missing",
			assetKind: "image", wantPaths: []string{"spec.messageImage"},
		},
		{
			name: "bare name with no Asset", kind: "LayoutPage", field: "messageImage", value: "missing",
			assetKind: "image", wantPaths: []string{"spec.messageImage"},
		},
		{
			name: "no Asset documents at all", kind: "LayoutPage", field: "messageImage", value: "logo",
			wantPaths: []string{"spec.messageImage"},
		},
		{
			name: "font Asset cannot back an image", kind: "LayoutPage", field: "messageImage", value: "logo",
			assetKind: "font", wantPaths: []string{"spec.messageImage"},
		},
		{name: "empty value renders no image", kind: "LayoutPage", field: "messageImage", value: ""},
		{name: "https URL", kind: "LayoutPage", field: "messageImage", value: "https://example.com/logo.png"},
		{name: "http URL", kind: "LayoutPage", field: "messageImage", value: "http://example.com/logo.png"},
		{name: "data URI", kind: "LayoutPage", field: "messageImage", value: "data:image/png;base64,AAAA"},
		{name: "root-relative URL", kind: "LayoutPage", field: "messageImage", value: "/assets/files/logo"},
		{name: "unresolved env var is owned by missing-env-var", kind: "LayoutPage", field: "messageImage", value: "${LOGO}"},

		{name: "card titleImage resolves", kind: "LayoutCard", field: "titleImage", value: "logo", assetKind: "image"},
		{
			name: "card titleImage dangling", kind: "LayoutCard", field: "titleImage", value: "nope",
			assetKind: "image", wantPaths: []string{"spec.titleImage"},
		},
		{name: "image source resolves", kind: "Image", field: "source", value: "logo", assetKind: "image"},
		{
			name: "image source dangling", kind: "Image", field: "source", value: "nope",
			assetKind: "image", wantPaths: []string{"spec.source"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{componentDoc(tt.kind, "c", map[string]any{tt.field: tt.value})}
			if tt.assetKind != "" {
				docs = append(docs, imageAssetDoc("logo", tt.assetKind))
			}
			assertPaths(t, runAssetRefRule(t, docs), tt.wantPaths)
		})
	}
}

func TestAssetReferenceUndefined_FilePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", "logo.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "adir.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		value     string
		wantPaths []string
	}{
		{name: "relative path to an existing file", value: "images/logo.png"},
		{name: "relative path to a missing file", value: "images/gone.png", wantPaths: []string{"spec.messageImage"}},
		{name: "bare filename that is missing", value: "gone.png", wantPaths: []string{"spec.messageImage"}},
		{name: "path that is a directory", value: "adir.png", wantPaths: []string{"spec.messageImage"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{
				File:     filepath.Join(dir, "page.yaml"),
				Position: 1,
				Kind:     "LayoutPage",
				Name:     "p",
				Raw:      rawDoc("LayoutPage", "p", map[string]any{"messageImage": tt.value}),
			}}
			assertPaths(t, runAssetRefRule(t, docs), tt.wantPaths)
		})
	}
}

// An Asset name always wins over the file-path interpretation, so an Asset may
// legitimately be named "logo.png".
func TestAssetReferenceUndefined_AssetNameWithExtensionBeatsPathCheck(t *testing.T) {
	docs := []Document{
		componentDoc("LayoutPage", "p", map[string]any{"messageImage": "logo.png"}),
		imageAssetDoc("logo.png", "image"),
	}
	assertPaths(t, runAssetRefRule(t, docs), nil)
}

func TestAssetReferenceUndefined_InlineChildren(t *testing.T) {
	spec := map[string]any{
		"pageLayout": "1x2",
		"children": []any{
			map[string]any{"kind": "LayoutCard", "spec": map[string]any{"titleImage": "logo"}},
			map[string]any{"kind": "LayoutCard", "spec": map[string]any{"titleImage": "nope"}},
		},
	}
	docs := []Document{
		componentDoc("LayoutPage", "p", spec),
		imageAssetDoc("logo", "image"),
	}
	assertPaths(t, runAssetRefRule(t, docs), []string{"spec.children.1.spec.titleImage"})
}

// A child that only carries a ref inherits the referenced document's image, so
// the finding belongs to that document — it must not be reported twice.
func TestAssetReferenceUndefined_RefChildReportedOnce(t *testing.T) {
	page := componentDoc("LayoutPage", "p", map[string]any{
		"children": []any{map[string]any{"kind": "LayoutCard", "ref": "card"}},
	})
	card := Document{
		File:     "/project/card.yaml",
		Position: 1,
		Kind:     "LayoutCard",
		Name:     "card",
		Raw:      rawDoc("LayoutCard", "card", map[string]any{"titleImage": "nope"}),
	}
	assertPaths(t, runAssetRefRule(t, []Document{page, card}), []string{"spec.titleImage"})
}

func TestAssetReferenceUndefined_MarkdownRefs(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		field     string
		value     string
		wantPaths []string
	}{
		{
			name: "message text references a known asset", kind: "LayoutPage", field: "messageText",
			value: "See ![alt](asset:logo)",
		},
		{
			name: "message text references a missing asset", kind: "LayoutPage", field: "messageText",
			value: "See ![alt](asset:missing)", wantPaths: []string{"spec.messageText"},
		},
		{
			name: "image title is ignored", kind: "LayoutPage", field: "messageText",
			value: `![alt](asset:missing "a caption")`, wantPaths: []string{"spec.messageText"},
		},
		{
			name: "title business unit", kind: "LayoutCard", field: "titleBusinessUnit",
			value: "![x](asset:missing)", wantPaths: []string{"spec.titleBusinessUnit"},
		},
		{
			name: "text component value", kind: "Text", field: "value",
			value: "![x](asset:missing)", wantPaths: []string{"spec.value"},
		},
		{
			name: "asset ref inside a fenced code block is not a reference", kind: "Text", field: "value",
			value: "```\n![x](asset:missing)\n```\n",
		},
		{
			name: "asset ref inside a code span is not a reference", kind: "Text", field: "value",
			value: "`![x](asset:missing)`",
		},
		{
			name: "a link is not an image", kind: "Text", field: "value",
			value: "[x](asset:missing)",
		},
		{
			name: "the same missing asset twice reports once", kind: "Text", field: "value",
			value: "![a](asset:missing) ![b](asset:missing)", wantPaths: []string{"spec.value"},
		},
		{
			name: "plain markdown with no asset refs", kind: "Text", field: "value",
			value: "**bold** ![x](https://example.com/a.png)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{
				componentDoc(tt.kind, "c", map[string]any{tt.field: tt.value}),
				imageAssetDoc("logo", "image"),
			}
			assertPaths(t, runAssetRefRule(t, docs), tt.wantPaths)
		})
	}
}

func runAssetSourceRule(t *testing.T, docs []Document) []lintMessage {
	t.Helper()
	findings := assetSourceMissing.Check(context.Background(), docs)
	msgs := make([]lintMessage, 0, len(findings))
	for _, f := range findings {
		if f.RuleID != "asset-source-missing" {
			t.Errorf("RuleID = %q, want asset-source-missing", f.RuleID)
		}
		msgs = append(msgs, lintMessage{path: f.Path, message: f.Message})
	}
	return msgs
}

type lintMessage struct {
	path    string
	message string
}

func TestAssetSourceMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		source      map[string]any
		wantFinding bool
		wantMessage string
	}{
		{name: "local file exists", source: map[string]any{"localPath": "image.png"}},
		{
			name: "local file is missing", source: map[string]any{"localPath": "gone.png"},
			wantFinding: true, wantMessage: "does not exist",
		},
		{
			name: "local path is a directory", source: map[string]any{"localPath": "adir"},
			wantFinding: true, wantMessage: "is a directory",
		},
		{name: "remote url needs no file", source: map[string]any{"remoteURL": "https://example.com/a.png"}},
		{name: "inline base64 needs no file", source: map[string]any{"inlineBase64": "AAAA"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := map[string]any{"type": "image", "mediaType": "image/png", "source": tt.source}
			docs := []Document{{
				File:     filepath.Join(dir, "assets.yaml"),
				Position: 1,
				Kind:     "Asset",
				Name:     "logo",
				Raw:      rawDoc("Asset", "logo", spec),
			}}

			got := runAssetSourceRule(t, docs)
			if !tt.wantFinding {
				if len(got) != 0 {
					t.Fatalf("got %d findings %v, want none", len(got), got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d findings %v, want 1", len(got), got)
			}
			if got[0].path != "spec.source.localPath" {
				t.Errorf("path = %q, want spec.source.localPath", got[0].path)
			}
			if !strings.Contains(got[0].message, tt.wantMessage) {
				t.Errorf("message = %q, want it to contain %q", got[0].message, tt.wantMessage)
			}
		})
	}
}

// A subdirectory manifest resolves its localPath relative to itself, matching
// render.resolveLocalAssetPath.
func TestAssetSourceMissing_ResolvesRelativeToManifest(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "resources", "logos")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "image.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := map[string]any{
		"type":      "image",
		"mediaType": "image/png",
		"source":    map[string]any{"localPath": "image.png"},
	}
	docs := []Document{{
		File:     filepath.Join(sub, "logo.yaml"),
		Position: 1,
		Kind:     "Asset",
		Name:     "logo",
		Raw:      rawDoc("Asset", "logo", spec),
	}}

	if got := runAssetSourceRule(t, docs); len(got) != 0 {
		t.Fatalf("got %d findings %v, want none", len(got), got)
	}
}

// The rule must not choke on a document whose spec is not an object.
func TestAssetRules_MalformedSpec(t *testing.T) {
	raw := json.RawMessage(`{"apiVersion":"bino.bi/v1","kind":"Asset","metadata":{"name":"logo"},"spec":"nope"}`)
	docs := []Document{{File: "/project/a.yaml", Position: 1, Kind: "Asset", Name: "logo", Raw: raw}}

	if got := runAssetSourceRule(t, docs); len(got) != 0 {
		t.Fatalf("got %d findings %v, want none", len(got), got)
	}
	if got := runAssetRefRule(t, docs); len(got) != 0 {
		t.Fatalf("got %d findings %v, want none", len(got), got)
	}
}
