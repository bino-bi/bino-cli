package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// writePDF writes a PDF from numbered object bodies (object i+1 = objs[i]),
// with object 1 as the catalog.
func writePDF(t *testing.T, path string, objs []string) {
	t.Helper()
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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTOCLinks(t *testing.T) {
	const prefix = "https://toc.bino.invalid/#"
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writePDF(t, path, []string{
		"<< /Type /Catalog /Pages 2 0 R /Dests 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Annots [5 0 R 7 0 R 8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 10 100 20] /A << /S /URI /URI (" + prefix + "intro) >> >>",
		"<< /intro [4 0 R /XYZ 0 800 0] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 30 100 40] /A << /S /URI /URI (" + prefix + "missing) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 50 100 60] /A << /S /URI /URI (https://example.com/) >> >>",
	})
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ResolveTOCLinks(path, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("resolved = %d, want 1", n)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, %v; want 0644 kept", info.Mode().Perm(), err)
	}

	ctx, err := api.ReadContextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageDict, _, _, err := ctx.PageDict(1, false)
	if err != nil {
		t.Fatal(err)
	}
	annots, err := ctx.DereferenceArray(pageDict["Annots"])
	if err != nil || len(annots) != 3 {
		t.Fatalf("annots = %v, %v", annots, err)
	}
	link := func(i int) types.Dict {
		d, err := ctx.DereferenceDict(annots[i])
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	resolved := link(0)
	if _, ok := resolved["A"]; ok {
		t.Errorf("resolved link still has a URI action")
	}
	dest, err := ctx.DereferenceArray(resolved["Dest"])
	if err != nil || len(dest) == 0 {
		t.Fatalf("resolved link Dest = %v, %v", resolved["Dest"], err)
	}
	ref, ok := dest[0].(types.IndirectRef)
	if !ok {
		t.Fatalf("Dest[0] = %T", dest[0])
	}
	if pageNr, err := ctx.PageNumber(ref.ObjectNumber.Value()); err != nil || pageNr != 2 {
		t.Errorf("Dest page = %d, %v; want 2", pageNr, err)
	}

	missing := link(1)
	if _, ok := missing["A"]; ok {
		t.Errorf("unresolved toc link still points at the placeholder URL")
	}
	if _, ok := missing["Dest"]; ok {
		t.Errorf("unresolved toc link got a Dest")
	}

	if _, ok := link(2)["A"]; !ok {
		t.Errorf("external link lost its action")
	}
}

func TestMergeFilesAddsNoBookmarks(t *testing.T) {
	dir := t.TempDir()
	page := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>",
	}
	a, b, out := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf"), filepath.Join(dir, "out.pdf")
	writePDF(t, a, page)
	writePDF(t, b, page)
	if err := MergeFiles([]string{a, b}, out); err != nil {
		t.Fatal(err)
	}
	ctx, err := api.ReadContextFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.PageCount != 2 {
		t.Errorf("pages = %d, want 2", ctx.PageCount)
	}
	root, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root["Outlines"]; ok {
		t.Errorf("merge added bookmarks: %v", root["Outlines"])
	}
}

func TestResolveTOCLinksNoMatchLeavesFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writePDF(t, path, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>",
	})
	before, _ := os.ReadFile(path)
	if n, err := ResolveTOCLinks(path, "https://toc.bino.invalid/#"); err != nil || n != 0 {
		t.Fatalf("got %d, %v", n, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("file changed without toc links")
	}
}
