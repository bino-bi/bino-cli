//go:build integration

package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitorus/pdfsign"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
)

// threePagePDF is a PDF whose page 2 has a size (400x600pt) no other page has.
func threePagePDF() []byte {
	sizes := [][2]int{{595, 842}, {400, 600}, {595, 842}}
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

// writeCoverFixture writes a project with a TOC document, header/footer and
// the given cover YAML block (empty for none).
func writeCoverFixture(t *testing.T, cover string) string {
	t.Helper()
	workdir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for x := 0; x < 40; x++ {
		for y := 0; y < 60; y++ {
			img.Set(x, y, color.RGBA{R: 11, G: 83, B: 148, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	var md strings.Builder
	md.WriteString("# Handbook\n\n")
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&md, "## Chapter %d\n\n", i)
		for p := 0; p < 6; p++ {
			md.WriteString(strings.Repeat("Lorem ipsum dolor sit amet. ", 40) + "\n\n")
		}
	}
	files := map[string][]byte{
		"assets/hero.png":     pngBuf.Bytes(),
		"assets/brochure.pdf": threePagePDF(),
		"docs/handbook.md":    []byte(md.String()),
		"assets.yaml": []byte(`apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: hero
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./assets/hero.png
---
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: brochure
spec:
  type: file
  mediaType: application/pdf
  source:
    localPath: ./assets/brochure.pdf
`),
		"doc.yaml": []byte(`apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata:
  name: handbook
spec:
  format: a4
  orientation: portrait
  locale: en
  title: Handbook
  author: Ops Team
  tableOfContents: true
  displayHeaderFooter: true
  headerTemplate: '<div style="font-size:8px;width:100%;text-align:center">Handbook</div>'
  footerTemplate: '<div style="font-size:8px;width:100%;text-align:right;padding:0 10mm;color:rgb(255,0,0)">Page <span class="pageNumber"></span> of <span class="totalPages"></span></div>'
  sources:
    - docs/handbook.md
` + cover),
	}
	for name, content := range files {
		path := filepath.Join(workdir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return workdir
}

const fullCover = `  cover:
    front:
      - template: classic
        params: { subtitle: "Operations guide", logo: hero, accent: "#0b5394" }
      - html: |
          <div class="note">{{ .Date }} · {{ .Author }}</div>
        css: |
          .note { position: absolute; bottom: 3cm; left: 2cm; }
    back:
      - pdf: brochure
        page: 2
      - image: hero
        fit: cover
`

type coverBuild struct {
	ctx     context.Context
	builder pipeline.Builder
	docs    []config.Document
	art     config.DocumentArtefact
	outDir  string
	chrome  string
	logger  logx.Logger
}

func newCoverBuild(t *testing.T, cover string) coverBuild {
	t.Helper()
	ctx := context.Background()
	engineVersion, cacheDir, chromePath := renderStack(t)
	workdir := writeCoverFixture(t, cover)
	loaded, err := pipeline.LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	arts, err := config.CollectDocumentArtefacts(loaded.Documents)
	if err != nil || len(arts) != 1 {
		t.Fatalf("document artefacts = %v, %v", arts, err)
	}
	return coverBuild{
		ctx:     ctx,
		builder: pipeline.Builder{Workdir: workdir, EngineVersion: engineVersion, CacheDir: cacheDir},
		docs:    loaded.Documents,
		art:     arts[0],
		outDir:  t.TempDir(),
		chrome:  chromePath,
		logger:  logx.Nop(),
	}
}

func (c coverBuild) build(t *testing.T, profiles map[string]config.SigningProfile) (string, error) {
	t.Helper()
	res, err := buildDocumentArtefact(c.ctx, buildDocumentArtefactConfig{
		Builder: &c.builder, Logger: c.logger, Docs: c.docs, Artifact: c.art,
		SigningProfiles: profiles, OutputDir: c.outDir, ChromePath: c.chrome,
	})
	return res.PDFPath, err
}

// pageContents returns the decoded content stream of each page.
func pageContents(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := pdf.PageCount(path)
	if err != nil {
		t.Fatal(err)
	}
	contents := make([][]byte, n)
	err = api.ExtractContent(bytes.NewReader(data), nil, func(r io.Reader, pageNr int) error {
		c, rerr := io.ReadAll(r)
		contents[pageNr-1] = c
		return rerr
	}, nil)
	if err != nil {
		t.Fatalf("extract content: %v", err)
	}
	return contents
}

// footerMarker is the text color of the fixture footer (rgb 255,0,0).
var footerMarker = []byte("1 0 0 rg")

// linkTargetPages returns the page numbers internal links on page pageNr point to.
func linkTargetPages(t *testing.T, path string, pageNr int) []int {
	t.Helper()
	ctx, err := api.ReadContextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageDict, _, _, err := ctx.PageDict(pageNr, false)
	if err != nil {
		t.Fatal(err)
	}
	annots, _ := ctx.DereferenceArray(pageDict["Annots"])
	var pages []int
	for _, obj := range annots {
		annot, err := ctx.DereferenceDict(obj)
		if err != nil || annot == nil {
			continue
		}
		dest, err := ctx.DereferenceArray(annot["Dest"])
		if err != nil || len(dest) == 0 {
			continue
		}
		if ref, ok := dest[0].(types.IndirectRef); ok {
			if n, err := ctx.PageNumber(ref.ObjectNumber.Value()); err == nil {
				pages = append(pages, n)
			}
		}
	}
	return pages
}

func TestIntegration_DocumentCoverPages(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	plain := newCoverBuild(t, "")
	plainPDF, err := plain.build(t, nil)
	if err != nil {
		t.Fatalf("build without cover: %v", err)
	}
	bodyPages, err := pdf.PageCount(plainPDF)
	if err != nil {
		t.Fatal(err)
	}
	headingPages, err := pdf.HeadingPageMap(plainPDF, []string{"handbook", "chapter-1", "chapter-2", "chapter-3", "chapter-4"})
	if err != nil || len(headingPages) == 0 {
		t.Fatalf("heading pages = %v, %v", headingPages, err)
	}
	tocPages := bodyPages
	for _, p := range headingPages {
		tocPages = min(tocPages, p-1)
	}
	if tocPages == 0 || bodyPages <= tocPages {
		t.Fatalf("body has %d pages, %d TOC pages", bodyPages, tocPages)
	}
	plainContents := pageContents(t, plainPDF)
	if !bytes.Contains(plainContents[tocPages], footerMarker) {
		t.Fatalf("fixture footer marker not found on the first content page")
	}

	c := newCoverBuild(t, fullCover)
	out, err := c.build(t, nil)
	if err != nil {
		t.Fatalf("build with cover: %v", err)
	}
	total, err := pdf.PageCount(out)
	if err != nil {
		t.Fatal(err)
	}
	if total != bodyPages+4 {
		t.Fatalf("pages = %d, want %d (4 covers + %d body)", total, bodyPages+4, bodyPages)
	}

	contents := pageContents(t, out)
	for _, i := range []int{0, 1, total - 2, total - 1} {
		if bytes.Contains(contents[i], footerMarker) {
			t.Errorf("cover page %d has the footer", i+1)
		}
	}
	// Same footer bytes as without covers: still "Page 1 of <body pages>".
	firstContent := 2 + tocPages
	if !bytes.Equal(contents[firstContent], plainContents[tocPages]) {
		t.Errorf("first content page differs from the build without covers")
	}

	dims, err := api.PageDimsFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if d := dims[total-2]; d.Width != 400 || d.Height != 600 {
		t.Errorf("pdf cover page size = %vx%v, want page 2 of the asset (400x600)", d.Width, d.Height)
	}

	// TOC links point at the same content pages as without covers, shifted by
	// the two front pages.
	want := map[int]bool{}
	for _, p := range linkTargetPages(t, plainPDF, 1) {
		want[p+2] = true
	}
	got := linkTargetPages(t, out, 3)
	if len(got) == 0 || len(want) == 0 {
		t.Fatalf("toc links: with cover %v, without cover %v", got, want)
	}
	for _, p := range got {
		if !want[p] || p <= 2+tocPages {
			t.Errorf("toc link targets page %d, want one of %v", p, want)
		}
	}

	// Same input and SOURCE_DATE_EPOCH: the cover pages, including .Date, are
	// the same. The whole file is not compared: the pdfcpu merge numbers
	// objects in a different order on each run.
	again, err := c.build(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	againContents := pageContents(t, again)
	for _, i := range []int{0, 1} {
		if !bytes.Equal(contents[i], againContents[i]) {
			t.Errorf("cover page %d differs between two builds with SOURCE_DATE_EPOCH", i+1)
		}
	}
}

func TestIntegration_DocumentCoverOverflowAndPageSize(t *testing.T) {
	c := newCoverBuild(t, `  cover:
    front:
      - html: '<div style="font-size:40pt">`+strings.Repeat("Overflow line<br>", 60)+`</div>'
    back:
      - pdf: brochure
        page: 2
`)
	var logs bytes.Buffer
	c.logger = logx.NewTerminal(&logs, &logs, false)
	c.builder.Logger = c.logger
	out, err := c.build(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := newCoverBuild(t, "")
	plainPDF, err := plain.build(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := pdf.PageCount(out)
	body, _ := pdf.PageCount(plainPDF)
	if got != body+2 {
		t.Errorf("pages = %d, want %d: the overflowing cover must stay one page", got, body+2)
	}
	for _, want := range []string{"cover.front[0] overflows one page", `page 2 of pdf asset "brochure" is 400x600pt`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %q:\n%s", want, logs.String())
		}
	}
}

func TestIntegration_DocumentCoverErrors(t *testing.T) {
	for name, tc := range map[string]struct{ cover, want string }{
		"missing asset":     {"  cover:\n    front:\n      - image: ghost\n", `asset "ghost"`},
		"page out of range": {"  cover:\n    back:\n      - pdf: brochure\n        page: 5\n", "page 5 out of range: pdf asset \"brochure\" has 3 page(s)"},
	} {
		t.Run(name, func(t *testing.T) {
			c := newCoverBuild(t, tc.cover)
			_, err := c.build(t, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
			if !strings.Contains(fmt.Sprint(err), "handbook") {
				t.Errorf("error does not name the artefact: %v", err)
			}
		})
	}
}

// testSigningProfiles returns a signing profile "signer" with a fresh
// self-signed certificate.
func testSigningProfiles(t *testing.T, workdir string) map[string]config.SigningProfile {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "bino test signer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]config.SigningProfile{"signer": {
		Document: config.Document{Name: "signer", File: filepath.Join(workdir, "signing.yaml")},
		Spec: config.SigningProfileSpec{
			Certificate: config.PEMSource{Inline: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))},
			PrivateKey:  config.PEMSource{Inline: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))},
			Signer:      config.SigningProfileSigner{Name: "Test Signer", Reason: "Integration test"},
		},
	}}
}

// requireOneValidSignature fails unless the PDF at path has exactly one valid signature.
func requireOneValidSignature(t *testing.T, path string) {
	t.Helper()
	doc, err := pdfsign.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result := doc.Verify().TrustSelfSigned(true).SkipRevocationCheck(true).AllowedEKUs(x509.ExtKeyUsageAny)
	if err := result.Err(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Count() != 1 || !result.Valid() {
		t.Fatalf("signatures = %+v", result.Signatures())
	}
	// The signature must cover the whole file: nothing may be appended after signing.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := byteRangeRe.FindAllSubmatch(data, -1)
	if len(m) == 0 {
		t.Fatal("no /ByteRange found")
	}
	last := m[len(m)-1]
	start, _ := strconv.Atoi(string(last[1]))
	length, _ := strconv.Atoi(string(last[2]))
	if start+length != len(data) {
		t.Errorf("signature covers bytes up to %d of %d; the file changed after signing", start+length, len(data))
	}
}

var byteRangeRe = regexp.MustCompile(`/ByteRange\s*\[\s*\d+\s+\d+\s+(\d+)\s+(\d+)\s*\]`)

func TestIntegration_DocumentCoverSigned(t *testing.T) {
	c := newCoverBuild(t, fullCover+"  signingProfile: signer\n")
	out, err := c.build(t, testSigningProfiles(t, c.builder.Workdir))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	requireOneValidSignature(t, out)
	if n, err := pdf.PageCount(out); err != nil || n < 5 {
		t.Errorf("signed file has %d pages (%v), want covers and body", n, err)
	}
}
