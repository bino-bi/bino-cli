package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMinimalPDF writes a minimal but valid PDF with the given number of
// pages to path. If dests is non-empty, an old-style /Dests dictionary is
// added to the catalog mapping each name to its 1-based page number.
func writeMinimalPDF(t *testing.T, path string, pageCount int, dests map[string]int) {
	t.Helper()

	var buf bytes.Buffer
	var offsets []int

	addObj := func(body string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(body)
	}

	buf.WriteString("%PDF-1.4\n")

	// Object numbering: 1 catalog, 2 pages, 3..2+pageCount page objects,
	// optional 3+pageCount dests dictionary.
	destsObjNr := 0
	catalog := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	if len(dests) > 0 {
		destsObjNr = 3 + pageCount
		catalog = fmt.Sprintf("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Dests %d 0 R >>\nendobj\n", destsObjNr)
	}
	addObj(catalog)

	kids := make([]string, 0, pageCount)
	for i := range pageCount {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	addObj(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
		strings.Join(kids, " "), pageCount))

	for i := range pageCount {
		addObj(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> >>\nendobj\n", 3+i))
	}

	if destsObjNr > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d 0 obj\n<<", destsObjNr)
		for name, pageNr := range dests {
			fmt.Fprintf(&b, " /%s [%d 0 R /Fit]", name, 2+pageNr)
		}
		b.WriteString(" >>\nendobj\n")
		addObj(b.String())
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, xrefOffset)

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write minimal pdf: %v", err)
	}
}

func TestToRoman(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, ""},
		{-3, ""},
		{1, "i"},
		{4, "iv"},
		{9, "ix"},
		{14, "xiv"},
		{40, "xl"},
		{90, "xc"},
		{400, "cd"},
		{1987, "mcmlxxxvii"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.n), func(t *testing.T) {
			if got := toRoman(tt.n); got != tt.want {
				t.Errorf("toRoman(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestPageCount(t *testing.T) {
	t.Run("counts pages", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "three.pdf")
		writeMinimalPDF(t, path, 3, nil)

		got, err := PageCount(path)
		if err != nil {
			t.Fatalf("PageCount() error = %v", err)
		}
		if got != 3 {
			t.Errorf("PageCount() = %d, want 3", got)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		if _, err := PageCount(filepath.Join(t.TempDir(), "missing.pdf")); err == nil {
			t.Error("PageCount() should error on missing file")
		}
	})
}

func TestMergeFiles(t *testing.T) {
	t.Run("no input files errors", func(t *testing.T) {
		err := MergeFiles(nil, filepath.Join(t.TempDir(), "out.pdf"))
		if err == nil {
			t.Fatal("MergeFiles() should error with no input files")
		}
		if !strings.Contains(err.Error(), "no input files") {
			t.Errorf("error = %v, want no input files", err)
		}
	})

	t.Run("merges page counts", func(t *testing.T) {
		tmp := t.TempDir()
		a := filepath.Join(tmp, "a.pdf")
		b := filepath.Join(tmp, "b.pdf")
		out := filepath.Join(tmp, "out.pdf")
		writeMinimalPDF(t, a, 2, nil)
		writeMinimalPDF(t, b, 1, nil)

		if err := MergeFiles([]string{a, b}, out); err != nil {
			t.Fatalf("MergeFiles() error = %v", err)
		}

		got, err := PageCount(out)
		if err != nil {
			t.Fatalf("PageCount(out) error = %v", err)
		}
		if got != 3 {
			t.Errorf("merged page count = %d, want 3", got)
		}
	})

	t.Run("invalid input errors", func(t *testing.T) {
		tmp := t.TempDir()
		bad := filepath.Join(tmp, "bad.pdf")
		if err := os.WriteFile(bad, []byte("not a pdf"), 0o644); err != nil {
			t.Fatalf("write bad file: %v", err)
		}

		if err := MergeFiles([]string{bad}, filepath.Join(tmp, "out.pdf")); err == nil {
			t.Error("MergeFiles() should error on invalid input")
		}
	})
}

func TestStampRomanPageNumbers(t *testing.T) {
	t.Run("stamps all pages in place", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "doc.pdf")
		writeMinimalPDF(t, path, 2, nil)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read original: %v", err)
		}

		if err := StampRomanPageNumbers(path, "2026-06-10"); err != nil {
			t.Fatalf("StampRomanPageNumbers() error = %v", err)
		}

		// The stamped file must remain a valid PDF with unchanged page count.
		got, err := PageCount(path)
		if err != nil {
			t.Fatalf("PageCount(stamped) error = %v", err)
		}
		if got != 2 {
			t.Errorf("stamped page count = %d, want 2", got)
		}

		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read stamped: %v", err)
		}
		if bytes.Equal(before, after) {
			t.Error("stamped file content is unchanged")
		}

		// No temp file left behind.
		if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
			t.Error("temp file was not cleaned up")
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		err := StampRomanPageNumbers(filepath.Join(t.TempDir(), "missing.pdf"), "")
		if err == nil {
			t.Fatal("StampRomanPageNumbers() should error on missing file")
		}
		if !strings.Contains(err.Error(), "count pages") {
			t.Errorf("error = %v, want count pages", err)
		}
	})

	t.Run("invalid pdf errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.pdf")
		if err := os.WriteFile(path, []byte("not a pdf"), 0o644); err != nil {
			t.Fatalf("write bad file: %v", err)
		}
		if err := StampRomanPageNumbers(path, ""); err == nil {
			t.Error("StampRomanPageNumbers() should error on invalid pdf")
		}
	})
}

func TestInjectHeadingLinks(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		headingIDs []string
		want       []string // substrings expected in the output
		unchanged  bool
	}{
		{
			name:       "no heading ids returns input unchanged",
			html:       "<html><body>x</body></html>",
			headingIDs: nil,
			unchanged:  true,
		},
		{
			name:       "inserts nav before closing body",
			html:       "<html><body>content</body></html>",
			headingIDs: []string{"h1", "h2"},
			want:       []string{`<a href="#h1">`, `<a href="#h2">`, `</nav></body>`},
		},
		{
			name:       "appends when body tag missing",
			html:       "<p>fragment</p>",
			headingIDs: []string{"h1"},
			want:       []string{`<p>fragment</p><nav`, `<a href="#h1">`},
		},
		{
			name:       "escapes heading ids",
			html:       "<body></body>",
			headingIDs: []string{`a"b<c`},
			want:       []string{`<a href="#a&#34;b&lt;c">`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(InjectHeadingLinks([]byte(tt.html), tt.headingIDs))
			if tt.unchanged {
				if got != tt.html {
					t.Errorf("InjectHeadingLinks() = %q, want unchanged input", got)
				}
				return
			}
			for _, sub := range tt.want {
				if !strings.Contains(got, sub) {
					t.Errorf("InjectHeadingLinks() = %q, want substring %q", got, sub)
				}
			}
		})
	}
}

func TestHeadingPageMap(t *testing.T) {
	t.Run("no heading ids returns nil", func(t *testing.T) {
		got, err := HeadingPageMap("does-not-matter.pdf", nil)
		if err != nil {
			t.Fatalf("HeadingPageMap() error = %v", err)
		}
		if got != nil {
			t.Errorf("HeadingPageMap() = %v, want nil", got)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		_, err := HeadingPageMap(filepath.Join(t.TempDir(), "missing.pdf"), []string{"h1"})
		if err == nil {
			t.Error("HeadingPageMap() should error on missing file")
		}
	})

	t.Run("resolves dests dictionary entries", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "dests.pdf")
		writeMinimalPDF(t, path, 3, map[string]int{"intro": 1, "details": 3})

		got, err := HeadingPageMap(path, []string{"intro", "details", "missing"})
		if err != nil {
			t.Fatalf("HeadingPageMap() error = %v", err)
		}
		if got["intro"] != 1 {
			t.Errorf("page for intro = %d, want 1", got["intro"])
		}
		if got["details"] != 3 {
			t.Errorf("page for details = %d, want 3", got["details"])
		}
		if _, ok := got["missing"]; ok {
			t.Error("unexpected entry for unknown heading id")
		}
	})

	t.Run("pdf without destinations yields empty map", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plain.pdf")
		writeMinimalPDF(t, path, 1, nil)

		got, err := HeadingPageMap(path, []string{"h1"})
		if err != nil {
			t.Fatalf("HeadingPageMap() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("HeadingPageMap() = %v, want empty map", got)
		}
	})
}

func TestHeadingPageMapFromAnnotations(t *testing.T) {
	t.Run("no heading ids returns nil", func(t *testing.T) {
		got, err := HeadingPageMapFromAnnotations("does-not-matter.pdf", nil)
		if err != nil {
			t.Fatalf("HeadingPageMapFromAnnotations() error = %v", err)
		}
		if got != nil {
			t.Errorf("HeadingPageMapFromAnnotations() = %v, want nil", got)
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		_, err := HeadingPageMapFromAnnotations(filepath.Join(t.TempDir(), "missing.pdf"), []string{"h1"})
		if err == nil {
			t.Error("HeadingPageMapFromAnnotations() should error on missing file")
		}
	})
}
