// Package cover turns the cover entries of a DocumentArtefact spec into
// standalone, full-bleed HTML pages (or a PDF page reference) that the build
// prints as separate PDFs and the preview shows in iframes.
package cover

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	htmltemplate "html/template"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

//go:embed classic.html
var classicSource string

var classicTemplate = htmltemplate.Must(htmltemplate.New("classic").Parse(classicSource))

// assetNameExpr is the Asset metadata.name syntax.
const assetNameExpr = `(?:@[a-z0-9][a-z0-9_-]*/(?:[a-z0-9][a-z0-9_-]*/)?)?[A-Za-z0-9_](?:[-A-Za-z0-9_]*[A-Za-z0-9_])?`

// asset:<name> is resolved only where a URL is expected, so text such as the
// CSS selector ".asset:hover" is left alone. assetRefPattern covers src, href,
// poster and data attribute values and CSS url(); group 1 is the text before
// "asset:", group 2 the name. assetSrcsetPattern finds srcset values, in which
// every asset:<name> is a URL.
var (
	assetRefPattern    = regexp.MustCompile(`((?i:\b(?:src|href|poster|data)\s*=\s*["']?|url\(\s*["']?))asset:(` + assetNameExpr + `)`)
	assetSrcsetPattern = regexp.MustCompile(`(?i:\bsrcset\s*=\s*)("[^"]*"|'[^']*')`)
	assetInSrcset      = regexp.MustCompile(`asset:(` + assetNameExpr + `)`)
)

// rewriteAssetRefs replaces every asset:<name> in a URL position of s with
// resolve(name).
func rewriteAssetRefs(s string, resolve func(name string) string) string {
	s = assetSrcsetPattern.ReplaceAllStringFunc(s, func(m string) string {
		return assetInSrcset.ReplaceAllStringFunc(m, func(ref string) string {
			return resolve(ref[len("asset:"):])
		})
	})
	return assetRefPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := assetRefPattern.FindStringSubmatch(m)
		return sub[1] + resolve(sub[2])
	})
}

// Page is one resolved cover page.
type Page struct {
	// Label names the entry in messages, for example "cover.front[0]".
	Label string
	// HTML is the standalone page document. Empty for a PDF page.
	HTML []byte
	// LocalAssets are the files HTML references through the asset server.
	LocalAssets []render.LocalAsset
	// PDFAsset is set for a PDF page; PDFPage is its 1-based page number.
	PDFAsset string
	PDFPage  int
	PDF      render.AssetInfo
}

// IsPDF reports whether the page is taken from a PDF asset.
func (p Page) IsPDF() bool { return p.PDFAsset != "" }

// Data is the template data of template and html cover pages.
type Data struct {
	Title   string
	Author  string
	Subject string
	Date    string
	Params  map[string]any
}

// Resolve checks and renders the cover pages of spec. Missing or wrongly typed
// assets are errors. It returns nil slices when spec has no cover.
func Resolve(docs []config.Document, spec config.DocumentArtefactSpec) (front, back []Page, err error) {
	if !spec.HasCover() {
		return nil, nil, nil
	}
	now := time.Now()
	if ts, ok := pdf.SourceDateEpoch(); ok {
		now = ts
	}
	date := FormatDate(now, spec.Locale)
	front, err = resolveList(docs, spec, spec.Cover.Front, "cover.front", date)
	if err != nil {
		return nil, nil, err
	}
	back, err = resolveList(docs, spec, spec.Cover.Back, "cover.back", date)
	if err != nil {
		return nil, nil, err
	}
	return front, back, nil
}

func resolveList(docs []config.Document, spec config.DocumentArtefactSpec, entries []config.CoverPage, prefix, date string) ([]Page, error) {
	pages := make([]Page, 0, len(entries))
	for i, entry := range entries {
		label := fmt.Sprintf("%s[%d]", prefix, i)
		page, err := resolvePage(docs, spec, entry, date)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		page.Label = label
		pages = append(pages, page)
	}
	return pages, nil
}

func resolvePage(docs []config.Document, spec config.DocumentArtefactSpec, entry config.CoverPage, date string) (Page, error) {
	data := Data{Title: spec.Title, Author: spec.Author, Subject: spec.Subject, Date: date, Params: entry.Params}
	switch {
	case entry.PDF != "":
		info, err := lookupAsset(docs, entry.PDF)
		if err != nil {
			return Page{}, err
		}
		if info.Type != "file" || info.MediaType != "application/pdf" {
			return Page{}, fmt.Errorf("asset %q must be of type file with mediaType application/pdf, got type %q mediaType %q", entry.PDF, info.Type, info.MediaType)
		}
		pageNr := entry.Page
		if pageNr == 0 {
			pageNr = 1
		}
		return Page{PDFAsset: entry.PDF, PDFPage: pageNr, PDF: info}, nil

	case entry.Image != "":
		info, err := lookupAsset(docs, entry.Image)
		if err != nil {
			return Page{}, err
		}
		if info.Type != "image" {
			return Page{}, fmt.Errorf("asset %q must be of type image, got %q", entry.Image, info.Type)
		}
		fit := entry.Fit
		if fit == "" {
			fit = "cover"
		}
		body := `<img src="` + html.EscapeString(info.Value) + `" alt="" style="display:block;width:100%;height:100%;object-fit:` + html.EscapeString(fit) + `">`
		return Page{HTML: document(spec, "", body), LocalAssets: localOf(info)}, nil

	case entry.Template != "":
		if entry.Template != "classic" {
			return Page{}, fmt.Errorf("unknown template %q; valid templates: classic", entry.Template)
		}
		classic := struct {
			Data
			LogoURL htmltemplate.URL
			Accent  htmltemplate.CSS
		}{Data: data}
		// Trusted manifest value; as a plain string html/template would turn
		// rgb(), hsl() or var() into ZgotmplZ.
		if accent, ok := entry.Params["accent"].(string); ok {
			classic.Accent = htmltemplate.CSS(accent) //nolint:gosec // G203: from the trusted manifest
		}
		var locals []render.LocalAsset
		if logo, ok := entry.Params["logo"].(string); ok && logo != "" {
			info, err := lookupAsset(docs, logo)
			if err != nil {
				return Page{}, fmt.Errorf("params.logo: %w", err)
			}
			classic.LogoURL = htmltemplate.URL(info.Value) //nolint:gosec // G203: resolved from a declared Asset
			locals = localOf(info)
		}
		var buf bytes.Buffer
		if err := classicTemplate.Execute(&buf, classic); err != nil {
			return Page{}, fmt.Errorf("template classic: %w", err)
		}
		return finishHTML(docs, spec, buf.String(), entry.CSS, locals)

	case entry.HTML != "":
		tmpl, err := htmltemplate.New("cover").Parse(entry.HTML)
		if err != nil {
			return Page{}, fmt.Errorf("html: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return Page{}, fmt.Errorf("html: %w", err)
		}
		return finishHTML(docs, spec, buf.String(), entry.CSS, nil)
	}
	return Page{}, fmt.Errorf("set one of image, pdf, template or html")
}

// finishHTML resolves asset: references in body and css and wraps them into a page document.
func finishHTML(docs []config.Document, spec config.DocumentArtefactSpec, body, css string, locals []render.LocalAsset) (Page, error) {
	var resolveErr error
	replace := func(s string) string {
		return rewriteAssetRefs(s, func(name string) string {
			info, err := lookupAsset(docs, name)
			if err != nil {
				if resolveErr == nil {
					resolveErr = err
				}
				return "asset:" + name
			}
			locals = append(locals, localOf(info)...)
			return info.Value
		})
	}
	body = replace(body)
	css = replace(css)
	if resolveErr != nil {
		return Page{}, resolveErr
	}
	return Page{HTML: document(spec, css, body), LocalAssets: locals}, nil
}

func lookupAsset(docs []config.Document, name string) (render.AssetInfo, error) {
	info, err := render.ResolveNamedAssetInfo(docs, name)
	if err != nil {
		return render.AssetInfo{}, fmt.Errorf("asset %q: %w", name, err)
	}
	return info, nil
}

func localOf(info render.AssetInfo) []render.LocalAsset {
	if info.Local == nil {
		return nil
	}
	return []render.LocalAsset{*info.Local}
}

// document wraps body into a standalone page: page size from the spec,
// margin 0, no document stylesheet. Overflow is not hidden, so Chrome prints it
// on extra pages, which the build detects, warns about and cuts off.
func document(spec config.DocumentArtefactSpec, css, body string) []byte {
	w, h := PageSizeInches(spec.Format, spec.Orientation)
	size := formatInches(w) + " " + formatInches(h)
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang='")
	b.WriteString(html.EscapeString(spec.Locale))
	b.WriteString("'>\n<head>\n<meta charset='utf-8'>\n<title>")
	b.WriteString(html.EscapeString(spec.Title))
	b.WriteString("</title>\n<style>\n@page { size: ")
	b.WriteString(size)
	b.WriteString("; margin: 0; }\n*, *::before, *::after { box-sizing: border-box; }\nhtml, body { margin: 0; padding: 0; width: ")
	b.WriteString(formatInches(w))
	b.WriteString("; height: ")
	b.WriteString(formatInches(h))
	b.WriteString("; }\nbody { position: relative; -webkit-print-color-adjust: exact; print-color-adjust: exact; }\n</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(body)
	// After the body, so it overrides the styles a template puts there.
	if css != "" {
		b.WriteString("\n<style>\n")
		b.WriteString(css)
		b.WriteString("\n</style>")
	}
	b.WriteString("\n</body>\n</html>\n")
	return []byte(b.String())
}

// AssetNames lists the Asset names a cover references: image and pdf entries,
// the classic logo, and asset: URLs in html and css. Order is stable, no duplicates.
func AssetNames(spec *config.CoverSpec) []string {
	if spec == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, entry := range append(append([]config.CoverPage(nil), spec.Front...), spec.Back...) {
		add(entry.Image)
		add(entry.PDF)
		if logo, ok := entry.Params["logo"].(string); ok && entry.Template != "" {
			add(logo)
		}
		rewriteAssetRefs(entry.HTML+"\n"+entry.CSS, func(name string) string {
			add(name)
			return ""
		})
	}
	return names
}

// PageSizeInches returns the page width and height for a document format and
// orientation. Unknown formats fall back to A4, like the document page CSS.
func PageSizeInches(format, orientation string) (width, height float64) {
	w, h := chrome.PaperSizeInches(format)
	if w == 0 || h == 0 {
		w, h = chrome.PaperSizeInches("a4")
	}
	if strings.EqualFold(orientation, "landscape") {
		w, h = h, w
	}
	return w, h
}

func formatInches(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64) + "in"
}

var monthNames = map[string][12]string{
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

// FormatDate formats t for a locale: de "2. Januar 2026", en "January 2, 2026",
// any other locale ISO "2026-01-02". Region suffixes (de-AT, en_GB) use the language.
func FormatDate(t time.Time, locale string) string {
	lang := strings.ToLower(locale)
	if i := strings.IndexAny(lang, "-_"); i >= 0 {
		lang = lang[:i]
	}
	months, ok := monthNames[lang]
	if !ok {
		return t.Format("2006-01-02")
	}
	month := months[t.Month()-1]
	if lang == "de" {
		return fmt.Sprintf("%d. %s %d", t.Day(), month, t.Year())
	}
	return fmt.Sprintf("%s %d, %d", month, t.Day(), t.Year())
}

// PreviewHTML renders pages for the preview: each HTML page in an iframe sized
// to the page, so its CSS stays apart from the document CSS, and each PDF page
// as a placeholder card.
func PreviewHTML(pages []Page, format, orientation, class string) string {
	if len(pages) == 0 {
		return ""
	}
	w, h := PageSizeInches(format, orientation)
	style := "display:block;width:" + formatInches(w) + ";height:" + formatInches(h) + ";border:0;background:#fff;"
	var b strings.Builder
	b.WriteString("<div class='bn-cover-preview " + class + "'>\n")
	for _, p := range pages {
		if p.IsPDF() {
			fmt.Fprintf(&b, "<div class='bn-cover-pdf' data-cover='%s' style='%sdisplay:flex;align-items:center;justify-content:center;font:14px sans-serif;color:#666;'>PDF page %d of asset %s</div>\n",
				html.EscapeString(p.Label), style, p.PDFPage, html.EscapeString(p.PDFAsset))
			continue
		}
		fmt.Fprintf(&b, "<iframe class='bn-cover-frame' data-cover='%s' style='%s' srcdoc='%s'></iframe>\n",
			html.EscapeString(p.Label), style, html.EscapeString(string(p.HTML)))
	}
	b.WriteString("</div>\n")
	return b.String()
}
