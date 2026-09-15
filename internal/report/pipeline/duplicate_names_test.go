package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// dupNamePage builds a LayoutPage whose only child is a Text showing marker.
func dupNamePage(t *testing.T, file, name, marker string, constraints ...string) config.Document {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1",
		"kind":       "LayoutPage",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"children": []any{
				map[string]any{"kind": "Text", "metadata": map[string]any{"name": "text"}, "spec": map[string]any{"value": marker}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return config.Document{
		File:        file,
		Position:    1,
		Kind:        "LayoutPage",
		Name:        name,
		Constraints: parseConstraints(t, constraints...),
		Raw:         raw,
	}
}

// dupNameArtefact builds a ReportArtefact without labels that lists refs.
func dupNameArtefact(refs config.LayoutPagesOrRefs) config.Artifact {
	return config.Artifact{
		Document: config.Document{
			Kind: "ReportArtefact",
			Name: "report",
			File: "report.yaml",
			Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1","kind":"ReportArtefact","metadata":{"name":"report"},"spec":{}}`),
		},
		Spec: config.ReportArtefactSpec{
			Format:      config.DefaultArtefactFormat,
			Orientation: config.DefaultArtefactOrientation,
			Language:    "en",
			LayoutPages: refs,
		},
	}
}

func TestRenderArtefactHTML_DuplicateLayoutPageName(t *testing.T) {
	docs := []config.Document{
		dupNamePage(t, "a.yaml", "main", "alpha"),
		dupNamePage(t, "b.yaml", "main", "beta"),
	}
	for _, ref := range []string{"main", "ma*"} {
		t.Run(ref, func(t *testing.T) {
			_, err := RenderArtefactHTML(context.Background(), t.TempDir(), docs, dupNameArtefact(config.LayoutPagesOrRefs{{Page: ref}}), RenderArtefactOptions{EngineVersion: "v1.0.0"})
			if err == nil || !strings.Contains(err.Error(), `duplicate LayoutPage name "main"`) {
				t.Fatalf("expected duplicate LayoutPage error, got %v", err)
			}
		})
	}
}

func TestRenderArtefact_DuplicateLayoutPageNameInEveryReportPath(t *testing.T) {
	docs := []config.Document{
		dupNamePage(t, "a.yaml", "main", "alpha"),
		dupNamePage(t, "b.yaml", "main", "beta"),
	}
	artifact := dupNameArtefact(config.LayoutPagesOrRefs{{Page: "main"}})
	renders := map[string]func(ctx context.Context, workdir string) error{
		"frame": func(ctx context.Context, workdir string) error {
			_, err := RenderArtefactFrameAndContextWithModeAndOptions(ctx, workdir, docs, artifact, spec.ModePreview, FrameRenderOptions{EngineVersion: "v1.0.0"})
			return err
		},
		"presentation frame": func(ctx context.Context, workdir string) error {
			_, err := RenderPresentationFrameAndContext(ctx, workdir, docs, artifact, PresentationArtefactRenderOptions{EngineVersion: "v1.0.0"})
			return err
		},
		"presentation html": func(ctx context.Context, workdir string) error {
			_, err := renderPresentationArtefactHTML(ctx, workdir, docs, artifact, PresentationArtefactRenderOptions{EngineVersion: "v1.0.0"}, spec.ModeBuild, true)
			return err
		},
	}
	for name, render := range renders {
		t.Run(name, func(t *testing.T) {
			err := render(context.Background(), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), `duplicate LayoutPage name "main"`) {
				t.Fatalf("expected duplicate LayoutPage error, got %v", err)
			}
		})
	}
}

// Two pages may share a name when their constraints keep them apart; the
// variant for the current mode must be used whichever file loads last.
func TestRenderArtefact_ModeVariantsWithSameName(t *testing.T) {
	buildPage := dupNamePage(t, "build.yaml", "main", "buildvariant", "mode==build")
	previewPage := dupNamePage(t, "preview.yaml", "main", "previewvariant", "mode==preview")
	artifact := dupNameArtefact(config.LayoutPagesOrRefs{{Page: "main"}})

	orders := map[string][]config.Document{
		"build first":   {buildPage, previewPage},
		"preview first": {previewPage, buildPage},
	}
	for name, docs := range orders {
		t.Run(name+"/build", func(t *testing.T) {
			result, err := RenderArtefactHTML(context.Background(), t.TempDir(), docs, artifact, RenderArtefactOptions{EngineVersion: "v1.0.0"})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertOnlyMarker(t, string(result.HTML), "buildvariant", "previewvariant")
		})
		t.Run(name+"/preview", func(t *testing.T) {
			result, err := RenderArtefactFrameAndContextWithModeAndOptions(context.Background(), t.TempDir(), docs, artifact, spec.ModePreview, FrameRenderOptions{EngineVersion: "v1.0.0"})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertOnlyMarker(t, string(result.ContextHTML), "previewvariant", "buildvariant")
		})
	}
}

func assertOnlyMarker(t *testing.T, html, want, unwanted string) {
	t.Helper()
	if n := strings.Count(html, want); n != 1 {
		t.Errorf("expected %q exactly once, found %d times", want, n)
	}
	if strings.Contains(html, unwanted) {
		t.Errorf("did not expect %q in the output", unwanted)
	}
}

func TestRenderArtefactHTML_SamePageWithDifferentParams(t *testing.T) {
	page := dupNamePage(t, "regional.yaml", "regional", "region${REGION}")
	defaultRegion := "EU"
	page.Params = []config.LayoutPageParamSpec{{Name: "REGION", Default: &defaultRegion}}
	artifact := dupNameArtefact(config.LayoutPagesOrRefs{
		{Page: "regional", Params: map[string]string{"REGION": "EU"}},
		{Page: "regional", Params: map[string]string{"REGION": "US"}},
	})

	result, err := RenderArtefactHTML(context.Background(), t.TempDir(), []config.Document{page}, artifact, RenderArtefactOptions{EngineVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := string(result.HTML)
	if !strings.Contains(html, "regionEU") || !strings.Contains(html, "regionUS") {
		t.Fatalf("expected both regions to be rendered, got:\n%s", html)
	}
}

// A LayoutPage the artefact does not list must not fail the render just
// because its constraints cannot be evaluated for this artefact.
func TestRenderArtefactHTML_UnlistedPageWithUnevaluableConstraint(t *testing.T) {
	docs := []config.Document{
		dupNamePage(t, "cover.yaml", "cover", "coverpage"),
		dupNamePage(t, "main.yaml", "mainPage", "mainpage", "labels.variant==simple"),
	}

	t.Run("unlisted", func(t *testing.T) {
		artifact := dupNameArtefact(config.LayoutPagesOrRefs{{Page: "cover"}})
		result, err := RenderArtefactHTML(context.Background(), t.TempDir(), docs, artifact, RenderArtefactOptions{EngineVersion: "v1.0.0"})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		assertOnlyMarker(t, string(result.HTML), "coverpage", "mainpage")
	})

	t.Run("listed", func(t *testing.T) {
		artifact := dupNameArtefact(config.LayoutPagesOrRefs{{Page: "cover"}, {Page: "mainPage"}})
		_, err := RenderArtefactHTML(context.Background(), t.TempDir(), docs, artifact, RenderArtefactOptions{EngineVersion: "v1.0.0"})
		if err == nil || !strings.Contains(err.Error(), `label "variant"`) {
			t.Fatalf("expected constraint error for the listed page, got %v", err)
		}
	})
}

func TestRenderScreenshotArtefactHTML_DuplicateDataSourceName(t *testing.T) {
	source := func(file string) config.Document {
		return config.Document{
			File:     file,
			Position: 1,
			Kind:     "DataSource",
			Name:     "sales",
			Raw:      json.RawMessage(`{"apiVersion":"bino.bi/v1","kind":"DataSource","metadata":{"name":"sales"},"spec":{"type":"inline","content":[{"v":1}]}}`),
		}
	}
	docs := []config.Document{
		dupNamePage(t, "page.yaml", "main", "alpha"),
		source("a.yaml"),
		source("b.yaml"),
	}
	artifact := config.ScreenshotArtefact{
		Document: config.Document{
			Kind: "ScreenshotArtefact",
			Name: "shots",
			File: "shots.yaml",
			Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1","kind":"ScreenshotArtefact","metadata":{"name":"shots"},"spec":{}}`),
		},
		Spec: config.ScreenshotArtefactSpec{
			LayoutPages: config.StringOrSlice{"main"},
			Format:      config.DefaultArtefactFormat,
			Orientation: config.DefaultArtefactOrientation,
			Language:    "en",
		},
	}

	_, err := RenderScreenshotArtefactHTML(context.Background(), t.TempDir(), docs, artifact, RenderScreenshotArtefactOptions{EngineVersion: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), `artefact "shots": duplicate DataSource name "sales"`) {
		t.Fatalf("expected duplicate DataSource error, got %v", err)
	}
}
