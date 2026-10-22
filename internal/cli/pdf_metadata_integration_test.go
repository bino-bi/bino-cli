//go:build integration

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
)

// renderStack returns the engine version, CDN cache dir and Chrome path, and
// fails the test when Chrome or the template engine is missing.
func renderStack(t *testing.T) (engineVersion, cacheDir, chromePath string) {
	t.Helper()
	requireRenderStack(t)
	engineMgr, err := engine.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	engineInfo, err := engineMgr.EnsureVersion(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err = pathutil.CacheDir("cdn")
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := chrome.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	chromePath, err = mgr.ResolveExecPath()
	if err != nil {
		t.Fatal(err)
	}
	return engineInfo.Version, cacheDir, chromePath
}

// pdfInfo returns the decoded string entries of the PDF's info dict.
func pdfInfo(t *testing.T, data []byte) map[string]string {
	t.Helper()
	ctx, err := api.ReadContext(bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	if ctx.Info == nil {
		return out
	}
	d, err := ctx.DereferenceDict(*ctx.Info)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range d {
		if s, err := ctx.DereferenceStringOrHexLiteral(v, model.V10, nil); err == nil {
			out[k] = s
		}
	}
	return out
}

// requireInfoInNewestTrailer fails unless the last trailer of the file names
// the info dict. Readers such as pdf.js take /Info only from that trailer.
func requireInfoInNewestTrailer(t *testing.T, data []byte) {
	t.Helper()
	i := bytes.LastIndex(data, []byte("startxref"))
	var off int
	if _, err := fmt.Sscan(string(data[i+len("startxref"):]), &off); err != nil {
		t.Fatal(err)
	}
	section := data[off:]
	end := bytes.Index(section, []byte("startxref"))
	if s := bytes.Index(section, []byte("stream")); s >= 0 && s < end {
		end = s
	}
	if !bytes.Contains(section[:end], []byte("/Info")) {
		t.Errorf("newest trailer has no /Info: %q", section[:end])
	}
}

var pdfDateRe = regexp.MustCompile(`/(?:CreationDate|ModDate)\s*\(D:(\d{8})`)

// requireDates fails unless every info date in the file is on day.
func requireDates(t *testing.T, data []byte, day string) {
	t.Helper()
	m := pdfDateRe.FindAllSubmatch(data, -1)
	if len(m) == 0 {
		t.Fatal("no dates found")
	}
	for _, d := range m {
		if string(d[1]) != day {
			t.Errorf("date %s, want %s", d[1], day)
		}
	}
}

func pdfInfoFile(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return pdfInfo(t, data)
}

func requireInfo(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

var wantMetadata = map[string]string{
	"Title": "Qualitätsbericht", "Author": "Jürgen Weiß", "Subject": "Prüfung Q3", "Keywords": "qualität, q3",
}

func setMetadata(title, author, subject *string, keywords *[]string) {
	*title, *author, *subject = wantMetadata["Title"], wantMetadata["Author"], wantMetadata["Subject"]
	*keywords = []string{"qualität", " ", "q3"}
}

type reportBuild struct {
	builder pipeline.Builder
	docs    []config.Document
	art     config.Artifact
	chrome  string
}

func newReportBuild(t *testing.T) reportBuild {
	t.Helper()
	engineVersion, cacheDir, chromePath := renderStack(t)
	workdir := t.TempDir()
	if err := os.CopyFS(workdir, os.DirFS(filepath.Join("..", "report", "pipeline", "testdata", "sample-bundle"))); err != nil {
		t.Fatal(err)
	}
	loaded, err := pipeline.LoadManifests(context.Background(), workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	arts, err := config.CollectArtefacts(loaded.Documents)
	if err != nil || len(arts) != 1 {
		t.Fatalf("report artefacts = %v, %v", arts, err)
	}
	r := reportBuild{
		builder: pipeline.Builder{Workdir: workdir, EngineVersion: engineVersion, CacheDir: cacheDir},
		docs:    loaded.Documents,
		art:     arts[0],
		chrome:  chromePath,
	}
	s := &r.art.Spec
	setMetadata(&s.Title, &s.Author, &s.Subject, &s.Keywords)
	return r
}

func (r reportBuild) build(t *testing.T, profiles map[string]config.SigningProfile) string {
	t.Helper()
	res, err := buildArtefact(context.Background(), buildArtefactConfig{
		Builder: &r.builder, Docs: r.docs, Artifact: r.art,
		SigningProfiles: profiles, OutputDir: t.TempDir(), ChromePath: r.chrome,
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	return res.PDFPath
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIntegration_ReportPDFMetadata(t *testing.T) {
	r := newReportBuild(t)
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	a := readFile(t, r.build(t, nil))
	requireInfo(t, pdfInfo(t, a), wantMetadata)
	if b := readFile(t, r.build(t, nil)); !bytes.Equal(a, b) {
		t.Errorf("two report builds with SOURCE_DATE_EPOCH are not byte-identical")
	}
	requireDates(t, a, "20231114")
}

func TestIntegration_ReportPDFMetadataSigned(t *testing.T) {
	r := newReportBuild(t)
	r.art.Spec.SigningProfile = "signer"
	out := r.build(t, testSigningProfiles(t, r.builder.Workdir))
	requireOneValidSignature(t, out)
	requireInfo(t, pdfInfoFile(t, out), wantMetadata)
	requireInfoInNewestTrailer(t, readFile(t, out))
}

func newMetadataDocBuild(t *testing.T) coverBuild {
	t.Helper()
	c := newCoverBuild(t, "")
	s := &c.art.Spec
	setMetadata(&s.Title, &s.Author, &s.Subject, &s.Keywords)
	return c
}

func TestIntegration_DocumentPDFMetadata(t *testing.T) {
	t.Run("with toc", func(t *testing.T) {
		c := newMetadataDocBuild(t)
		out, err := c.build(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		requireInfo(t, pdfInfoFile(t, out), wantMetadata)
	})

	t.Run("with cover", func(t *testing.T) {
		c := newCoverBuild(t, fullCover)
		s := &c.art.Spec
		setMetadata(&s.Title, &s.Author, &s.Subject, &s.Keywords)
		out, err := c.build(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		requireInfo(t, pdfInfoFile(t, out), wantMetadata)
	})

	t.Run("fields unset", func(t *testing.T) {
		c := newCoverBuild(t, "")
		c.art.Spec.Title, c.art.Spec.Author, c.art.Spec.Subject, c.art.Spec.Keywords = "", "", "", nil
		out, err := c.build(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := pdfInfoFile(t, out)
		for _, k := range []string{"Author", "Subject", "Keywords"} {
			if v, ok := got[k]; ok {
				t.Errorf("unexpected %s = %q", k, v)
			}
		}
		if v, ok := got["Title"]; ok && v == "" {
			t.Errorf("empty Title written")
		}
	})

	t.Run("reproducible without toc", func(t *testing.T) {
		c := newMetadataDocBuild(t)
		c.art.Spec.TableOfContents = false
		t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
		outA, err := c.build(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		a := readFile(t, outA)
		requireInfo(t, pdfInfo(t, a), wantMetadata)
		c.outDir = t.TempDir()
		outB, err := c.build(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, readFile(t, outB)) {
			t.Errorf("two document builds with SOURCE_DATE_EPOCH are not byte-identical")
		}
		requireDates(t, a, "20231114")
	})

	t.Run("signed", func(t *testing.T) {
		c := newMetadataDocBuild(t)
		c.art.Spec.SigningProfile = "signer"
		out, err := c.build(t, testSigningProfiles(t, c.builder.Workdir))
		if err != nil {
			t.Fatal(err)
		}
		requireOneValidSignature(t, out)
		requireInfo(t, pdfInfoFile(t, out), wantMetadata)
		requireInfoInNewestTrailer(t, readFile(t, out))
	})
}
