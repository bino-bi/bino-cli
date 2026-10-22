package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// onePagePDF is a minimal PDF with an info dict like the one Chrome writes.
var onePagePDF = []string{
	"<< /Type /Catalog /Pages 2 0 R >>",
	"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
	"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>",
	"<< /Creator (Chromium) /Producer (Skia/PDF) /CreationDate (D:20260101000000+00'00') /ModDate (D:20260101000000+00'00') >>",
}

func writeInfoPDF(t *testing.T, path string) {
	t.Helper()
	writePDF(t, path, onePagePDF)
	// Point the trailer at the info dict (object 4).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("/Root 1 0 R >>"), []byte("/Root 1 0 R /Info 4 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>"), 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readInfo returns the decoded entries of the PDF's info dict.
func readInfo(t *testing.T, path string) map[string]string {
	t.Helper()
	ctx, err := api.ReadContextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Info == nil {
		t.Fatal("no info dict")
	}
	d, err := ctx.DereferenceDict(*ctx.Info)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range d {
		if s, err := ctx.DereferenceStringOrHexLiteral(v, model.V10, nil); err == nil {
			out[k] = s
		}
	}
	return out
}

// newestTrailer returns the trailer dict text of the last xref section.
func newestTrailer(t *testing.T, data []byte) string {
	t.Helper()
	i := bytes.LastIndex(data, []byte("startxref"))
	var off int
	if _, err := fmt.Sscan(string(data[i+len("startxref"):]), &off); err != nil {
		t.Fatal(err)
	}
	section := data[off:]
	if !bytes.HasPrefix(section, []byte("xref")) {
		t.Fatalf("newest xref section is not a table: %.40q", section)
	}
	start := bytes.Index(section, []byte("trailer"))
	end := bytes.Index(section, []byte("startxref"))
	return string(section[start:end])
}

func TestSetInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writeInfoPDF(t, path)
	before, _ := os.ReadFile(path)

	err := SetInfo(path, Info{
		Title:    "Prüfbericht Größe",
		Author:   "Jürgen Weiß",
		Subject:  "Qualität",
		Keywords: []string{"a", " ", "ä b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := readInfo(t, path)
	for k, want := range map[string]string{
		"Title": "Prüfbericht Größe", "Author": "Jürgen Weiß", "Subject": "Qualität", "Keywords": "a, ä b", "Creator": "Chromium",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.HasPrefix(after, before) {
		t.Errorf("original bytes were rewritten; want an incremental update")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, %v; want 0644 kept", info.Mode().Perm(), err)
	}
}

func TestSetInfoWritesOnlySetFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writeInfoPDF(t, path)
	if err := SetInfo(path, Info{Title: "Only title", Subject: "  ", Keywords: []string{"", " "}}); err != nil {
		t.Fatal(err)
	}
	got := readInfo(t, path)
	if got["Title"] != "Only title" {
		t.Errorf("Title = %q", got["Title"])
	}
	for _, k := range []string{"Author", "Subject", "Keywords"} {
		if _, ok := got[k]; ok {
			t.Errorf("unexpected %s = %q", k, got[k])
		}
	}
}

func TestSetInfoEmptyLeavesFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writeInfoPDF(t, path)
	before, _ := os.ReadFile(path)
	if err := SetInfo(path, Info{Title: " ", Keywords: []string{""}}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("file changed without metadata")
	}
}

func TestSetInfoWithoutInfoDict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	writePDF(t, path, onePagePDF[:3])
	if err := SetInfo(path, Info{Author: "Ops"}); err != nil {
		t.Fatal(err)
	}
	if got := readInfo(t, path); got["Author"] != "Ops" {
		t.Errorf("Author = %q", got["Author"])
	}
}

func TestSetInfoOnMergedPDF(t *testing.T) {
	dir := t.TempDir()
	a, b, out := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf"), filepath.Join(dir, "out.pdf")
	writeInfoPDF(t, a)
	writeInfoPDF(t, b)
	if err := MergeFiles([]string{a, b}, out); err != nil {
		t.Fatal(err)
	}
	if err := SetInfo(out, Info{Title: "Merged", Keywords: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	got := readInfo(t, out)
	if got["Title"] != "Merged" || got["Keywords"] != "x" {
		t.Errorf("info = %v", got)
	}
	data, _ := os.ReadFile(out)
	if trailer := newestTrailer(t, data); !strings.Contains(trailer, "/Info") || !strings.Contains(trailer, "/Prev") {
		t.Errorf("newest trailer = %q, want /Info and /Prev", trailer)
	}
	if n, err := PageCount(out); err != nil || n != 2 {
		t.Errorf("pages = %d, %v", n, err)
	}
}

func TestSetInfoIsReproducible(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	build := func(name string) []byte {
		path := filepath.Join(t.TempDir(), name)
		writeInfoPDF(t, path)
		if err := SetInfo(path, Info{Title: "Bericht", Author: "Ops", Keywords: []string{"q3"}}); err != nil {
			t.Fatal(err)
		}
		if err := NormalizeForReproducibility(path, ts); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		return data
	}
	a := build("a.pdf")
	time.Sleep(1100 * time.Millisecond) // pdfcpu stamps the current second
	if b := build("b.pdf"); !bytes.Equal(a, b) {
		t.Errorf("two runs differ:\n%s\n---\n%s", a, b)
	}
}

func TestSetInfoAfterPDF20Cover(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "v20.pdf")
	writePDF(t, src, onePagePDF[:3])
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(src, bytes.Replace(data, []byte("%PDF-1.7"), []byte("%PDF-2.0"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	// pdfcpu cannot set info on PDF 2.0 without an info dict. Cover pages are
	// cut with TrimFile, which writes 1.7 with an info dict, so the merged
	// file never is 2.0.
	cut, body, out := filepath.Join(dir, "cut.pdf"), filepath.Join(dir, "body.pdf"), filepath.Join(dir, "out.pdf")
	if err := api.TrimFile(src, cut, []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	writePDF(t, body, onePagePDF[:3])
	if err := MergeFiles([]string{cut, body}, out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); bytes.HasPrefix(data, []byte("%PDF-2.0")) {
		t.Fatalf("merged file is PDF 2.0")
	}
	if err := SetInfo(out, Info{Title: "Zwei"}); err != nil {
		t.Fatal(err)
	}
	if got := readInfo(t, out); got["Title"] != "Zwei" {
		t.Errorf("info = %v", got)
	}
}
