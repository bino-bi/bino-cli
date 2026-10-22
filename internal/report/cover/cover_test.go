package cover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/report/config"
)

// assetDoc builds an Asset document with a local file next to the manifest.
func assetDoc(t *testing.T, dir, name, typ, mediaType, file string) config.Document {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       "Asset",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"type": typ, "mediaType": mediaType,
			"source": map[string]any{"localPath": "./" + file},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return config.Document{Kind: "Asset", Name: name, File: filepath.Join(dir, "assets.yaml"), Raw: raw}
}

func testDocs(t *testing.T) []config.Document {
	t.Helper()
	dir := t.TempDir()
	return []config.Document{
		assetDoc(t, dir, "logo", "image", "image/png", "logo.png"),
		assetDoc(t, dir, "brochure", "file", "application/pdf", "brochure.pdf"),
		assetDoc(t, dir, "notes", "file", "text/plain", "notes.txt"),
	}
}

func baseSpec(front ...config.CoverPage) config.DocumentArtefactSpec {
	return config.DocumentArtefactSpec{
		Format: "a4", Orientation: "portrait", Locale: "en",
		Title: "Manual", Author: "Ops Team", Subject: "Guide",
		Cover: &config.CoverSpec{Front: front},
	}
}

func TestResolveClassic(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	front, _, err := Resolve(testDocs(t), baseSpec(config.CoverPage{
		Template: "classic",
		Params:   map[string]any{"subtitle": "Install & run", "logo": "logo", "accent": "#123456"},
		CSS:      ".bn-cover { --bn-cover-font: serif; }",
	}))
	if err != nil {
		t.Fatal(err)
	}
	html := string(front[0].HTML)
	for _, want := range []string{
		">Manual</h1>", "Install &amp; run", "Ops Team", "November 14, 2023",
		`src="/assets/files/logo"`, "--bn-cover-accent: #123456", "--bn-cover-font: serif",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if len(front[0].LocalAssets) != 1 || front[0].LocalAssets[0].URLPath != "/assets/files/logo" {
		t.Errorf("local assets = %+v", front[0].LocalAssets)
	}
	if strings.Index(html, "--bn-cover-font: serif") < strings.Index(html, "--bn-cover-font: 'Helvetica") {
		t.Errorf("user css must come after the template styles")
	}
}

func TestResolveHTML(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	spec := baseSpec(config.CoverPage{
		HTML:   `<div class="note">{{ .Date }} · {{ .Author }} · {{ .Params.edition }} · {{ .Subject }}</div><img src="asset:logo">`,
		CSS:    `.note { background: url(asset:logo); }`,
		Params: map[string]any{"edition": "2nd"},
	})
	spec.Stylesheet = "./doc.css"
	front, _, err := Resolve(testDocs(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	html := string(front[0].HTML)
	for _, want := range []string{
		"November 14, 2023 · Ops Team · 2nd · Guide",
		`<img src="/assets/files/logo">`,
		"url(/assets/files/logo)",
		"@page { size: 8.2677in 11.6929in; margin: 0; }",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "doc.css") || strings.Contains(html, "bn-document-content") {
		t.Errorf("cover page must not load the document stylesheet or body CSS")
	}
	if len(front[0].LocalAssets) != 2 {
		t.Errorf("want 2 local asset entries (html and css), got %d", len(front[0].LocalAssets))
	}
}

func TestResolveImageAndPDF(t *testing.T) {
	spec := baseSpec(config.CoverPage{Image: "logo", Fit: "contain"})
	spec.Orientation = "landscape"
	spec.Cover.Back = []config.CoverPage{{PDF: "brochure", Page: 2}, {PDF: "brochure"}}
	front, back, err := Resolve(testDocs(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	html := string(front[0].HTML)
	if !strings.Contains(html, `src="/assets/files/logo"`) || !strings.Contains(html, "object-fit:contain") {
		t.Errorf("image page:\n%s", html)
	}
	if !strings.Contains(html, "size: 11.6929in 8.2677in") {
		t.Errorf("landscape page size missing:\n%s", html)
	}
	if !back[0].IsPDF() || back[0].PDFPage != 2 || back[1].PDFPage != 1 || back[1].Label != "cover.back[1]" {
		t.Errorf("pdf pages = %+v", back)
	}
}

func TestAssetRefsOnlyInURLs(t *testing.T) {
	front, _, err := Resolve(testDocs(t), baseSpec(config.CoverPage{
		HTML: `<a href='asset:logo'>x</a><video poster="asset:logo"></video><p>see asset:ghost</p>` +
			`<IMG SRC=asset:logo><object data="asset:logo"></object><img srcset="asset:logo 1x, asset:logo 2x">`,
		CSS: `.asset:hover { opacity: .8 } a.hero-asset:first-child {} .x { background: url( "asset:logo" ) } .y { background: URL(asset:logo) }`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	html := string(front[0].HTML)
	for _, want := range []string{
		`href='/assets/files/logo'`, `poster="/assets/files/logo"`, `url( "/assets/files/logo" )`,
		"see asset:ghost", ".asset:hover", "a.hero-asset:first-child",
		`SRC=/assets/files/logo>`, `data="/assets/files/logo"`, `srcset="/assets/files/logo 1x, /assets/files/logo 2x"`, `URL(/assets/files/logo)`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestClassicAccentKeepsCSSFunctions(t *testing.T) {
	for _, accent := range []string{"rgb(11, 83, 148)", "var(--brand)", "#0b5394"} {
		front, _, err := Resolve(nil, baseSpec(config.CoverPage{Template: "classic", Params: map[string]any{"accent": accent}}))
		if err != nil {
			t.Fatal(err)
		}
		if html := string(front[0].HTML); !strings.Contains(html, "--bn-cover-accent: "+accent+";") {
			t.Errorf("accent %q lost:\n%s", accent, html)
		}
	}
}

func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name  string
		entry config.CoverPage
		want  []string
	}{
		{"missing image asset", config.CoverPage{Image: "nope"}, []string{"cover.front[0]", `"nope"`, "not found"}},
		{"image asset used as pdf", config.CoverPage{PDF: "logo"}, []string{"cover.front[0]", "application/pdf", `"image"`}},
		{"pdf with wrong media type", config.CoverPage{PDF: "notes"}, []string{"text/plain"}},
		{"pdf used as image", config.CoverPage{Image: "brochure"}, []string{"must be of type image"}},
		{"unknown asset in html", config.CoverPage{HTML: `<img src="asset:ghost">`}, []string{`"ghost"`}},
		{"bad template syntax", config.CoverPage{HTML: `{{ .Title `}, []string{"html:"}},
		{"missing logo", config.CoverPage{Template: "classic", Params: map[string]any{"logo": "ghost"}}, []string{"params.logo", `"ghost"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Resolve(testDocs(t), baseSpec(tt.entry))
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestResolveNoCover(t *testing.T) {
	front, back, err := Resolve(nil, config.DocumentArtefactSpec{})
	if err != nil || front != nil || back != nil {
		t.Errorf("got %v %v %v", front, back, err)
	}
}

func TestFormatDate(t *testing.T) {
	d := time.Date(2026, time.March, 5, 12, 0, 0, 0, time.UTC)
	for locale, want := range map[string]string{
		"de": "5. März 2026", "de-AT": "5. März 2026", "en": "March 5, 2026", "en_GB": "March 5, 2026", "fr": "2026-03-05", "": "2026-03-05",
	} {
		if got := FormatDate(d, locale); got != want {
			t.Errorf("FormatDate(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestAssetNames(t *testing.T) {
	got := AssetNames(&config.CoverSpec{
		Front: []config.CoverPage{
			{Template: "classic", Params: map[string]any{"logo": "logo"}},
			{HTML: `<img src="asset:hero" srcset="asset:hero2 2x">`, CSS: `body { background: url(asset:@acme/bg) }`},
		},
		Back: []config.CoverPage{{PDF: "brochure"}, {Image: "logo"}},
	})
	want := []string{"logo", "hero2", "hero", "@acme/bg", "brochure"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("AssetNames = %v, want %v", got, want)
	}
}

func TestPreviewHTML(t *testing.T) {
	front, back, err := Resolve(testDocs(t), func() config.DocumentArtefactSpec {
		s := baseSpec(config.CoverPage{HTML: `<p class="x">Hi</p>`})
		s.Cover.Back = []config.CoverPage{{PDF: "brochure", Page: 3}}
		return s
	}())
	if err != nil {
		t.Fatal(err)
	}
	f := PreviewHTML(front, "a4", "portrait", "bn-cover-front")
	if !strings.Contains(f, "<iframe class='bn-cover-frame'") || !strings.Contains(f, "&lt;p class=&#34;x&#34;&gt;Hi") || !strings.Contains(f, "width:8.2677in;height:11.6929in") {
		t.Errorf("front preview:\n%s", f)
	}
	b := PreviewHTML(back, "a4", "portrait", "bn-cover-back")
	if !strings.Contains(b, "PDF page 3 of asset brochure") || strings.Contains(b, "<iframe") {
		t.Errorf("back preview:\n%s", b)
	}
	if PreviewHTML(nil, "a4", "portrait", "x") != "" {
		t.Errorf("empty pages must render nothing")
	}
}
