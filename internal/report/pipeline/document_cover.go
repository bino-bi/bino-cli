package pipeline

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/cover"
)

const (
	coverDownloadTimeout = 60 * time.Second
	coverDownloadMaxSize = 100 << 20
)

// CoverPDFs holds the one-page PDFs of the front and back cover pages.
type CoverPDFs struct {
	Front []string
	Back  []string
}

// Remove deletes the temp files.
func (c CoverPDFs) Remove() {
	for _, p := range append(append([]string(nil), c.Front...), c.Back...) {
		os.Remove(p)
	}
}

// RenderCoverPDFs prints each cover page of artifact into its own one-page
// temp PDF: no header, no footer, margin 0. HTML pages that overflow one page
// are cut to the first page with a warning. On error no temp files are left.
func (b *Builder) RenderCoverPDFs(ctx context.Context, docs []config.Document, artifact config.DocumentArtefact, base PDFRenderOptions) (CoverPDFs, error) {
	name := artifact.Document.Name
	front, back, err := cover.Resolve(docs, artifact.Spec)
	if err != nil {
		return CoverPDFs{}, fmt.Errorf("document artefact %s: %w", name, err)
	}

	opts := base
	opts.DisplayHeaderFooter = false
	opts.HeaderTemplate = ""
	opts.FooterTemplate = ""
	opts.MarginTop = ""
	opts.MarginBottom = ""

	var out CoverPDFs
	render := func(pages []cover.Page) ([]string, error) {
		var paths []string
		for _, page := range pages {
			path, err := b.renderCoverPage(ctx, page, artifact.Spec, opts)
			if err != nil {
				return paths, fmt.Errorf("document artefact %s: %s: %w", name, page.Label, err)
			}
			paths = append(paths, path)
		}
		return paths, nil
	}
	out.Front, err = render(front)
	if err != nil {
		out.Remove()
		return CoverPDFs{}, err
	}
	out.Back, err = render(back)
	if err != nil {
		out.Remove()
		return CoverPDFs{}, err
	}
	return out, nil
}

func (b *Builder) renderCoverPage(ctx context.Context, page cover.Page, spec config.DocumentArtefactSpec, opts PDFRenderOptions) (string, error) {
	if page.IsPDF() {
		return b.cutCoverPDFPage(ctx, page, spec)
	}
	path, err := b.RenderPDFToTempFile(ctx, page.HTML, page.LocalAssets, opts)
	if err != nil {
		return "", err
	}
	n, err := pdf.PageCount(path)
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if n > 1 {
		b.logger().Warnf("%s overflows one page; only the first page is kept", page.Label)
		if err := api.TrimFile(path, path, []string{"1"}, nil); err != nil {
			os.Remove(path)
			return "", fmt.Errorf("cut to one page: %w", err)
		}
	}
	return path, nil
}

// cutCoverPDFPage writes the selected page of a PDF asset to a temp file.
func (b *Builder) cutCoverPDFPage(ctx context.Context, page cover.Page, spec config.DocumentArtefactSpec) (string, error) {
	src, cleanup, err := coverPDFSource(ctx, page)
	if err != nil {
		return "", err
	}
	defer cleanup()

	n, err := pdf.PageCount(src)
	if err != nil {
		return "", fmt.Errorf("read pdf asset %q: %w", page.PDFAsset, err)
	}
	if page.PDFPage > n {
		return "", fmt.Errorf("page %d out of range: pdf asset %q has %d page(s)", page.PDFPage, page.PDFAsset, n)
	}

	out, err := os.CreateTemp("", "bino-cover-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp pdf file: %w", err)
	}
	outPath := out.Name()
	out.Close() //nolint:errcheck // reserves a temp name; TrimFile rewrites it
	if err := api.TrimFile(src, outPath, []string{strconv.Itoa(page.PDFPage)}, nil); err != nil {
		os.Remove(outPath)
		return "", fmt.Errorf("cut page %d from pdf asset %q: %w", page.PDFPage, page.PDFAsset, err)
	}

	if dims, err := api.PageDimsFile(outPath); err == nil && len(dims) == 1 {
		w, h := cover.PageSizeInches(spec.Format, spec.Orientation)
		if math.Abs(dims[0].Width-w*72) > 1 || math.Abs(dims[0].Height-h*72) > 1 {
			b.logger().Warnf("%s: page %d of pdf asset %q is %.0fx%.0fpt, the document is %s %s (%.0fx%.0fpt); the page keeps its own size",
				page.Label, page.PDFPage, page.PDFAsset, dims[0].Width, dims[0].Height, spec.Format, spec.Orientation, w*72, h*72)
		}
	}
	return outPath, nil
}

// coverPDFSource returns a local file path holding the PDF asset bytes and a
// cleanup func for temp files.
func coverPDFSource(ctx context.Context, page cover.Page) (path string, cleanup func(), err error) {
	info := page.PDF
	noop := func() {}
	switch {
	case info.Local != nil:
		return info.Local.FilePath, noop, nil
	case info.InlineBase64 != "":
		// Tolerate line-wrapped base64 in the manifest.
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(info.InlineBase64), ""))
		if err != nil {
			return "", noop, fmt.Errorf("decode pdf asset %q: %w", page.PDFAsset, err)
		}
		return writeTempPDF(data)
	case info.RemoteURL != "":
		data, err := downloadCoverPDF(ctx, info.RemoteURL)
		if err != nil {
			return "", noop, fmt.Errorf("download pdf asset %q: %w", page.PDFAsset, err)
		}
		return writeTempPDF(data)
	}
	return "", noop, fmt.Errorf("pdf asset %q has no source", page.PDFAsset)
}

func writeTempPDF(data []byte) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "bino-cover-src-*.pdf")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp pdf file: %w", err)
	}
	path = f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(path)
		return "", func() {}, fmt.Errorf("write temp pdf file: %w", err)
	}
	return path, func() { os.Remove(path) }, nil
}

func downloadCoverPDF(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, coverDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, coverDownloadMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > coverDownloadMaxSize {
		return nil, fmt.Errorf("GET %s: larger than %d MB", url, coverDownloadMaxSize>>20)
	}
	return data, nil
}
