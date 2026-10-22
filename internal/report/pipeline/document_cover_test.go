package pipeline

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/cover"
	"bino.bi/bino/internal/report/render"
)

// pagedPDF returns a minimal PDF with one empty page per size (width, height in pt).
func pagedPDF(sizes ...[2]int) []byte {
	objs := make([]string, 2, 2+len(sizes))
	objs[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	kids := make([]string, 0, len(sizes))
	for i, s := range sizes {
		kids = append(kids, fmt.Sprintf("%d 0 R", i+3))
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] >>", s[0], s[1]))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(sizes))
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

var threePages = [][2]int{{595, 842}, {400, 600}, {595, 842}}

func pdfCoverPage(info render.AssetInfo, page int) cover.Page {
	return cover.Page{Label: "cover.back[0]", PDFAsset: "brochure", PDFPage: page, PDF: info}
}

func TestCutCoverPDFPage(t *testing.T) {
	src := filepath.Join(t.TempDir(), "brochure.pdf")
	if err := os.WriteFile(src, pagedPDF(threePages...), 0o644); err != nil {
		t.Fatal(err)
	}
	local := render.AssetInfo{Local: &render.LocalAsset{FilePath: src}}
	inline := render.AssetInfo{InlineBase64: base64.StdEncoding.EncodeToString(pagedPDF(threePages...))}
	enc := inline.InlineBase64
	var wrapped strings.Builder
	for i := 0; i < len(enc); i += 76 {
		wrapped.WriteString(enc[i:min(i+76, len(enc))] + "\n  ")
	}
	inlineWrapped := render.AssetInfo{InlineBase64: wrapped.String()}
	spec := config.DocumentArtefactSpec{Format: "a4", Orientation: "portrait"}

	for name, info := range map[string]render.AssetInfo{"local": local, "inline": inline, "inline wrapped": inlineWrapped} {
		t.Run(name, func(t *testing.T) {
			b := &Builder{}
			out, err := b.cutCoverPDFPage(context.Background(), pdfCoverPage(info, 2), spec)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(out)
			dims, err := api.PageDimsFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(dims) != 1 || dims[0].Width != 400 || dims[0].Height != 600 {
				t.Errorf("dims = %+v, want one 400x600 page", dims)
			}
		})
	}

	_, err := (&Builder{}).cutCoverPDFPage(context.Background(), pdfCoverPage(local, 4), spec)
	if err == nil || !strings.Contains(err.Error(), "page 4 out of range") || !strings.Contains(err.Error(), "has 3 page(s)") {
		t.Errorf("out of range error = %v", err)
	}
}

func TestCutCoverPDFPageRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/brochure.pdf" {
			http.NotFound(w, r)
			return
		}
		w.Write(pagedPDF(threePages...)) //nolint:errcheck // test server
	}))
	defer srv.Close()
	spec := config.DocumentArtefactSpec{Format: "a4"}

	out, err := (&Builder{}).cutCoverPDFPage(context.Background(), pdfCoverPage(render.AssetInfo{RemoteURL: srv.URL + "/brochure.pdf"}, 3), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(out)
	if n, err := pdf.PageCount(out); err != nil || n != 1 {
		t.Errorf("page count = %d, %v", n, err)
	}

	_, err = (&Builder{}).cutCoverPDFPage(context.Background(), pdfCoverPage(render.AssetInfo{RemoteURL: srv.URL + "/missing.pdf"}, 1), spec)
	if err == nil || !strings.Contains(err.Error(), "download pdf asset") || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 error = %v", err)
	}
}

// renderDocWithCover renders the doc-bundle plaindoc with a cover and a PDF asset added.
func renderDocWithCover(t *testing.T, renderMode string) (string, error) {
	t.Helper()
	ctx := context.Background()
	workdir := copyDocBundle(t)
	pdfPath := filepath.Join(workdir, "brochure.pdf")
	if err := os.WriteFile(pdfPath, pagedPDF(threePages...), 0o644); err != nil {
		t.Fatal(err)
	}
	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1alpha1", "kind": "Asset", "metadata": map[string]any{"name": "brochure"},
		"spec": map[string]any{"type": "file", "mediaType": "application/pdf", "source": map[string]any{"localPath": pdfPath}},
	})
	docs := append(loadResult.Documents, config.Document{Kind: "Asset", Name: "brochure", File: filepath.Join(workdir, "assets.yaml"), Raw: raw})

	documentArtefacts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		t.Fatal(err)
	}
	var artifact config.DocumentArtefact
	for _, docArt := range documentArtefacts {
		if docArt.Document.Name == "plaindoc" {
			artifact = docArt
		}
	}
	artifact.Spec.Cover = &config.CoverSpec{
		Front: []config.CoverPage{{HTML: `<p class="front-marker">{{ .Title }}</p>`}},
		Back:  []config.CoverPage{{PDF: "brochure", Page: 2}},
	}
	result, err := RenderDocumentArtefactHTML(ctx, workdir, docs, artifact, DocumentArtefactRenderOptions{
		EngineVersion: "v1.0.0",
		RenderMode:    renderMode,
	})
	return string(result.HTML), err
}

func TestRenderDocumentArtefactHTML_PreviewCovers(t *testing.T) {
	html, err := renderDocWithCover(t, "preview")
	if err != nil {
		t.Fatal(err)
	}
	frame := strings.Index(html, "<iframe class='bn-cover-frame'")
	content := strings.Index(html, "<section class='bn-document-content'>")
	placeholder := strings.Index(html, "PDF page 2 of asset brochure")
	if frame < 0 || content < 0 || placeholder < 0 {
		t.Fatalf("iframe %d, content %d, placeholder %d", frame, content, placeholder)
	}
	if frame >= content || content >= placeholder {
		t.Errorf("want front iframe < content < back placeholder, got %d %d %d", frame, content, placeholder)
	}
	if !strings.Contains(html, "front-marker") || !strings.Contains(html, "Plain Document") {
		t.Errorf("front cover content missing")
	}
	if strings.Index(html, "</bn-context>") < placeholder {
		t.Errorf("covers must sit inside bn-context for live reload")
	}
}

func TestRenderDocumentArtefactHTML_BuildHasNoCovers(t *testing.T) {
	html, err := renderDocWithCover(t, "build")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "bn-cover") || strings.Contains(html, "front-marker") {
		t.Errorf("build HTML must not contain cover pages")
	}
}
