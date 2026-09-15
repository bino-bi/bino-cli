package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"

	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
)

// DocumentTOCPDFOptions configures BuildDocumentPDFWithTOC.
type DocumentTOCPDFOptions struct {
	// PDFOptions are the base Chrome render options shared by the content and
	// TOC passes. Its PDFPath is ignored; the merged result is written to OutputPath.
	PDFOptions PDFRenderOptions
	// OutputPath is the path the final merged PDF is written to.
	OutputPath string
	// Progress, when non-nil, receives human-readable stage updates.
	Progress func(message string)
}

func (o DocumentTOCPDFOptions) progress(message string) {
	if o.Progress != nil {
		o.Progress(message)
	}
}

// DefaultDocumentHeaderTemplate creates the default header template for
// DocumentArtefact PDFs. The header displays the document title centered at
// the top. Chrome header/footer templates use special CSS classes for dynamic
// content.
func DefaultDocumentHeaderTemplate(title string) string {
	escapedTitle := title
	// Basic HTML escaping for the title
	escapedTitle = strings.ReplaceAll(escapedTitle, "&", "&amp;")
	escapedTitle = strings.ReplaceAll(escapedTitle, "<", "&lt;")
	escapedTitle = strings.ReplaceAll(escapedTitle, ">", "&gt;")
	escapedTitle = strings.ReplaceAll(escapedTitle, "\"", "&quot;")
	return `<div style="width: 100%; font-size: 10px; font-family: Arial, sans-serif; text-align: center; color: #333;">` + escapedTitle + `</div>`
}

// DefaultDocumentFooterTemplate creates the default footer template for
// DocumentArtefact PDFs. The footer displays the date on the left and page
// number on the right. Chrome footer templates use special CSS classes:
// - "date" class shows the formatted print date
// - "pageNumber" class shows the current page number
// - "totalPages" class shows the total number of pages
func DefaultDocumentFooterTemplate() string {
	return `<div style="width: 100%; font-size: 9px; font-family: Arial, sans-serif; padding: 0 10mm; display: flex; justify-content: space-between; color: #666;">
  <span class="date"></span>
  <span>Page <span class="pageNumber"></span> of <span class="totalPages"></span></span>
</div>`
}

// tocFooterTemplate creates the footer template for the TOC PDF.
// It shows only the date — Roman numeral page numbers are stamped
// separately by pdfcpu after PDF generation.
func tocFooterTemplate() string {
	return `<div style="width: 100%; font-size: 9px; font-family: Arial, sans-serif; padding: 0 10mm; display: flex; justify-content: space-between; color: #666;">
  <span class="date"></span>
  <span></span>
</div>`
}

// BuildDocumentPDFWithTOC implements the 4-phase split-PDF pipeline for
// DocumentArtefacts with a Table of Contents:
//
//  1. Render content-only HTML → Chrome PrintToPDF → content.pdf (Arabic page numbers)
//  2. Parse content.pdf → extract heading ID → page number mapping
//  3. Render TOC-only HTML with correct page numbers → Chrome PrintToPDF → toc.pdf
//  4. Merge toc.pdf + content.pdf → final.pdf, stamp Roman numerals on TOC pages
func (b *Builder) BuildDocumentPDFWithTOC(ctx context.Context, docs []config.Document, artifact config.DocumentArtefact, opts DocumentTOCPDFOptions) error {
	logger := b.logger()
	artefactName := artifact.Document.Name

	// ── Phase 1: Render content-only PDF ──
	opts.progress(fmt.Sprintf("Rendering content for %s", artefactName))

	contentResult, err := b.RenderDocumentHTML(ctx, docs, artifact, DocumentArtefactRenderOptions{
		ExcludeTOC: true,
	})
	if err != nil {
		return fmt.Errorf("document artefact %s: render content: %w", artefactName, err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Inject hidden anchor links so Chrome creates internal link annotations
	// that HeadingPageMap can read for accurate page number extraction.
	contentHTML := pdf.InjectHeadingLinks(contentResult.HTML, contentResult.HeadingIDs)

	contentPDFOpts := opts.PDFOptions
	contentTmpPath, err := b.RenderPDFToTempFileWithData(ctx, contentHTML, contentResult.LocalAssets, contentResult.EmittedData, contentPDFOpts)
	if err != nil {
		return fmt.Errorf("document artefact %s: render content pdf: %w", artefactName, err)
	}
	defer os.Remove(contentTmpPath)

	// ── Phase 2: Extract heading page numbers from content PDF ──
	opts.progress(fmt.Sprintf("Collecting page numbers for %s", artefactName))

	var tocPageNumbers map[string]int
	if len(contentResult.HeadingIDs) > 0 {
		tocPageNumbers, err = pdf.HeadingPageMap(contentTmpPath, contentResult.HeadingIDs)
		if err != nil {
			logger.Warnf("Failed to extract heading pages for %s: %v (continuing without page numbers)", artefactName, err)
			tocPageNumbers = nil
		} else {
			logger.Debugf("Extracted %d heading page numbers for %s", len(tocPageNumbers), artefactName)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// ── Phase 3: Render TOC-only PDF with page numbers ──
	opts.progress(fmt.Sprintf("Rendering table of contents for %s", artefactName))

	tocResult, err := b.RenderDocumentHTML(ctx, docs, artifact, DocumentArtefactRenderOptions{
		TOCOnly:        true,
		TOCPageNumbers: tocPageNumbers,
	})
	if err != nil {
		return fmt.Errorf("document artefact %s: render toc: %w", artefactName, err)
	}

	tocPDFOpts := opts.PDFOptions
	// TOC uses a footer without page numbers — Roman numerals are stamped by pdfcpu.
	if tocPDFOpts.DisplayHeaderFooter {
		tocPDFOpts.FooterTemplate = tocFooterTemplate()
	}
	tocTmpPath, err := b.RenderPDFToTempFileWithData(ctx, tocResult.HTML, tocResult.LocalAssets, tocResult.EmittedData, tocPDFOpts)
	if err != nil {
		return fmt.Errorf("document artefact %s: render toc pdf: %w", artefactName, err)
	}
	defer os.Remove(tocTmpPath)

	// Stamp Roman numeral page numbers on the TOC PDF.
	if tocPDFOpts.DisplayHeaderFooter {
		if err := pdf.StampRomanPageNumbers(tocTmpPath, ""); err != nil {
			logger.Warnf("Failed to stamp Roman page numbers on TOC for %s: %v", artefactName, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// ── Phase 4: Merge TOC + content PDFs ──
	opts.progress(fmt.Sprintf("Merging PDFs for %s", artefactName))

	if err := b.MergePDFs(ctx, []string{tocTmpPath, contentTmpPath}, opts.OutputPath); err != nil {
		return fmt.Errorf("document artefact %s: merge pdfs: %w", artefactName, err)
	}

	return nil
}
