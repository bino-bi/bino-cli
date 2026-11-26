package cli

// `bino build --screenshot <ref>` renders PNGs of pages or components without
// a ScreenshotArtefact manifest. Each ref is one HTML render and one Chrome
// session; nothing is written to the project's manifests.
//
// Ref forms:
//   - ReportArtefact/<name>: every page of the report, as in its PDF.
//   - LayoutPage/<name>: one page, rendered in the first ReportArtefact (by
//     name) that lists it, so it has the report's format, language and style.
//     A page no report lists is rendered alone with its own pageFormat.
//   - <ComponentKind>/<name>: one component, rendered on its page like a
//     LayoutPage ref. A standalone component that is on no page is rendered
//     the way a ScreenshotArtefact renders it.
//   - <name>: a LayoutPage, or else a ReportArtefact, with that name.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/embed"
	"bino.bi/bino/internal/report/pipeline"
)

// screenshotDirName is the folder below --out-dir that holds --screenshot PNGs.
const screenshotDirName = "screenshots"

// screenshotJob is one --screenshot ref, resolved to what to render and what
// to capture. Exactly one of Report and Shot is set.
type screenshotJob struct {
	Ref     string
	Report  *config.Artifact
	Shot    *config.ScreenshotArtefact
	Capture pipeline.ScreenshotRef
}

// screenshotIndex is what --screenshot refs can name in a project.
type screenshotIndex struct {
	reports    []config.Artifact
	pages      map[string]config.Document
	components map[string]string // "Kind/Name" of a page child -> first page that has it
	standalone map[string]bool   // "Kind/Name" of a standalone component document
}

func newScreenshotIndex(docs []config.Document) (*screenshotIndex, error) {
	reports, err := config.CollectArtefacts(docs)
	if err != nil {
		return nil, err
	}
	idx := &screenshotIndex{
		reports:    reports,
		pages:      map[string]config.Document{},
		components: map[string]string{},
		standalone: map[string]bool{},
	}
	for _, doc := range docs {
		switch {
		case doc.Kind == "LayoutPage":
			if _, dup := idx.pages[doc.Name]; dup {
				continue
			}
			idx.pages[doc.Name] = doc
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue
			}
			if m, ok := root.(map[string]any); ok {
				collectPageChildren(m["spec"], doc.Name, idx.components)
			}
		case embed.IsRootRenderable(doc.Kind) && doc.Name != "":
			idx.standalone[doc.Kind+"/"+doc.Name] = true
		}
	}
	return idx, nil
}

// collectPageChildren records every object below v that has a kind and a
// name (metadata.name, or ref for a reference), at any depth, so components
// inside cards and grids are found too.
func collectPageChildren(v any, page string, out map[string]string) {
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			collectPageChildren(item, page, out)
		}
	case map[string]any:
		if kind, ok := t["kind"].(string); ok && kind != "" {
			name, _ := t["ref"].(string)
			if md, ok := t["metadata"].(map[string]any); ok {
				if n, ok := md["name"].(string); ok && n != "" {
					name = n
				}
			}
			if name != "" {
				key := kind + "/" + name
				if _, seen := out[key]; !seen {
					out[key] = page
				}
			}
		}
		for _, child := range t {
			collectPageChildren(child, page, out)
		}
	}
}

// resolveScreenshotJobs turns --screenshot values into render jobs.
func resolveScreenshotJobs(docs []config.Document, values []string) ([]screenshotJob, error) {
	idx, err := newScreenshotIndex(docs)
	if err != nil {
		return nil, err
	}
	jobs := make([]screenshotJob, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		job, err := idx.resolve(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		if seen[job.Ref] {
			continue
		}
		seen[job.Ref] = true
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (idx *screenshotIndex) resolve(value string) (screenshotJob, error) {
	kind, name, hasKind := strings.Cut(value, "/")
	// A scoped name ("@acme/kit/page") has slashes but no kind.
	if !hasKind || strings.HasPrefix(value, "@") {
		kind, name = "", value
	}
	if name == "" {
		return screenshotJob{}, idx.unknown(value)
	}

	if kind == "" {
		_, isPage := idx.pages[name]
		_, isReport := idx.report(name)
		switch {
		case isPage && isReport:
			return screenshotJob{}, fmt.Errorf("--screenshot %q is both a LayoutPage and a ReportArtefact; write LayoutPage/%s or ReportArtefact/%s", value, name, name)
		case isPage:
			kind = "LayoutPage"
		case isReport:
			kind = "ReportArtefact"
		default:
			return screenshotJob{}, idx.unknown(value)
		}
	}

	ref := kind + "/" + name
	fileBase := strings.ToLower(kind) + "-" + url.PathEscape(name)

	switch kind {
	case "ReportArtefact":
		report, ok := idx.report(name)
		if !ok {
			return screenshotJob{}, idx.unknown(value)
		}
		return screenshotJob{
			Ref:     ref,
			Report:  &report,
			Capture: pipeline.ScreenshotRef{Kind: "LayoutPage", FileBase: fileBase},
		}, nil
	case "LayoutPage":
		page, ok := idx.pages[name]
		if !ok {
			return screenshotJob{}, idx.unknown(value)
		}
		report := idx.reportForPage(page)
		return screenshotJob{
			Ref:     ref,
			Report:  &report,
			Capture: pipeline.ScreenshotRef{Kind: "LayoutPage", Name: name, FileBase: fileBase},
		}, nil
	}

	capture := pipeline.ScreenshotRef{Kind: kind, Name: name, FileBase: fileBase}
	if pageName, ok := idx.components[ref]; ok {
		report := idx.reportForPage(idx.pages[pageName])
		return screenshotJob{Ref: ref, Report: &report, Capture: capture}, nil
	}
	if idx.standalone[ref] {
		shot := standaloneComponentShot(kind, name)
		return screenshotJob{Ref: ref, Shot: &shot, Capture: capture}, nil
	}
	return screenshotJob{}, idx.unknown(value)
}

func (idx *screenshotIndex) report(name string) (config.Artifact, bool) {
	for _, r := range idx.reports {
		if r.Document.Name == name {
			return r, true
		}
	}
	return config.Artifact{}, false
}

// reportForPage returns the first report that lists page, cut down to that
// page, or a report of only that page when none lists it.
func (idx *screenshotIndex) reportForPage(page config.Document) config.Artifact {
	for _, r := range idx.reports {
		if refs := pageRefsFor(r.Spec.LayoutPages, page.Name); len(refs) > 0 {
			r.Spec.LayoutPages = refs
			return r
		}
	}
	return pageOnlyReport(page)
}

// pageRefsFor keeps the layoutPages entries that select page. A glob entry
// becomes the exact page name. Empty refs select every page.
func pageRefsFor(refs config.LayoutPagesOrRefs, page string) config.LayoutPagesOrRefs {
	if len(refs) == 0 {
		return config.LayoutPagesOrRefs{{Page: page}}
	}
	var out config.LayoutPagesOrRefs
	globbed := false
	for _, r := range refs {
		name := strings.TrimSpace(r.Page)
		switch {
		case r.IsGlob():
			if ok, _ := path.Match(name, page); ok && !globbed { //nolint:errcheck // an invalid pattern counts as no match
				globbed = true
				out = append(out, config.LayoutPageRef{Page: page})
			}
		case name == page:
			out = append(out, r)
		}
	}
	return out
}

// pageOnlyReport is an in-memory ReportArtefact that renders one page with the
// page's own format and orientation.
func pageOnlyReport(page config.Document) config.Artifact {
	var payload struct {
		Spec struct {
			PageFormat      string `json:"pageFormat"`
			PageOrientation string `json:"pageOrientation"`
		} `json:"spec"`
	}
	_ = json.Unmarshal(page.Raw, &payload) //nolint:errcheck // a page that does not parse keeps the defaults
	spec := config.ReportArtefactSpec{
		Format:      cmpOr(payload.Spec.PageFormat, config.DefaultArtefactFormat),
		Orientation: cmpOr(payload.Spec.PageOrientation, config.DefaultArtefactOrientation),
		Language:    config.DefaultArtefactLanguage,
		LayoutPages: config.LayoutPagesOrRefs{{Page: page.Name}},
	}
	return config.Artifact{Document: syntheticDoc("ReportArtefact", spec), Spec: spec}
}

// standaloneComponentShot is an in-memory ScreenshotArtefact for a component
// that is on no page; its render wraps the component in a page of its own.
func standaloneComponentShot(kind, name string) config.ScreenshotArtefact {
	spec := config.ScreenshotArtefactSpec{
		Refs:            []config.ScreenshotRef{{Kind: kind, Name: name}},
		Format:          config.DefaultArtefactFormat,
		Orientation:     config.DefaultArtefactOrientation,
		Language:        config.DefaultArtefactLanguage,
		FilenamePattern: config.DefaultScreenshotFilenamePattern,
		ImageFormat:     config.DefaultScreenshotImageFormat,
	}
	return config.ScreenshotArtefact{Document: syntheticDoc("ScreenshotArtefact", spec), Spec: spec}
}

// syntheticDoc builds the document of an in-memory artefact. The render reads
// its raw spec for constraint matching.
func syntheticDoc(kind string, spec any) config.Document {
	raw, _ := json.Marshal(map[string]any{ //nolint:errcheck // plain structs always marshal
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       kind,
		"metadata":   map[string]any{"name": "_screenshot"},
		"spec":       spec,
	})
	return config.Document{Kind: kind, Name: "_screenshot", Raw: raw}
}

func cmpOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// unknown reports a ref that names nothing, with every ref that would work.
func (idx *screenshotIndex) unknown(value string) error {
	available := make([]string, 0, len(idx.reports)+len(idx.pages)+len(idx.components)+len(idx.standalone))
	for _, r := range idx.reports {
		available = append(available, "ReportArtefact/"+r.Document.Name)
	}
	for name := range idx.pages {
		available = append(available, "LayoutPage/"+name)
	}
	seen := map[string]bool{}
	for ref := range idx.components {
		seen[ref] = true
	}
	for ref := range idx.standalone {
		seen[ref] = true
	}
	components := make([]string, 0, len(seen))
	for ref := range seen {
		components = append(components, ref)
	}
	sort.Strings(available)
	sort.Strings(components)
	available = append(available, components...)
	if len(available) == 0 {
		return fmt.Errorf("--screenshot %q: nothing to screenshot, the project has no ReportArtefact, LayoutPage or component", value)
	}
	return fmt.Errorf("--screenshot %q: no such ReportArtefact, LayoutPage or component (use Kind/Name). Available:\n  %s", value, strings.Join(available, "\n  "))
}

// runScreenshotJobs renders each job and writes its PNGs to
// <out-dir>/screenshots. Any failed capture fails the build.
func runScreenshotJobs(ctx context.Context, out *Output, docs []config.Document, jobs []screenshotJob, scale float64, cfg buildExecutionConfig) ([]buildlog.ScreenshotEntry, []string, error) {
	shotDir := filepath.Join(cfg.OutputDir, screenshotDirName)
	spinnerCfg := SpinnerConfig{Stdout: cfg.Stdout, NoColor: cfg.NoColor}
	out.Step(fmt.Sprintf("Capturing %d screenshot(s)...", len(jobs)))

	var entries []buildlog.ScreenshotEntry
	var warnings []string
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		cfg.clearPluginDatasets()
		logger := cfg.Logger.Channel(job.Ref)
		spinner := NewSpinner(spinnerCfg)
		spinner.Start(fmt.Sprintf("Rendering %s", job.Ref))

		var (
			res                 pipeline.RenderResult
			err                 error
			format, orientation string
		)
		if job.Report != nil {
			res, err = cfg.Builder.RenderArtefactHTML(ctx, docs, *job.Report)
			format, orientation = job.Report.Spec.Format, job.Report.Spec.Orientation
		} else {
			res, err = cfg.Builder.RenderScreenshotHTML(ctx, docs, *job.Shot)
			format, orientation = job.Shot.Spec.Format, job.Shot.Spec.Orientation
		}
		pipeline.LogDiagnostics(logger.Channel("datasource"), res.Diagnostics)
		if err != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", job.Ref))
			return nil, nil, RuntimeErrorf("screenshot %s: %w", job.Ref, err)
		}
		for _, w := range res.Warnings {
			logger.Warnf("%s", w)
			warnings = append(warnings, job.Ref+": "+w)
		}

		capture := &layoutStateCapture{Logger: logger}
		if cfg.CaptureLayoutState {
			capture.SnapshotPath = filepath.Join(shotDir, job.Capture.FileBase+".layout.json")
		}
		spinner.Update(fmt.Sprintf("Capturing %s", job.Ref))
		results, err := cfg.Builder.CaptureScreenshotsWithData(ctx, res.HTML, res.LocalAssets, res.EmittedData, pipeline.ScreenshotRenderOptions{
			OutputDir:             shotDir,
			ChromePath:            cfg.ChromePath,
			Format:                format,
			Orientation:           orientation,
			Debug:                 cfg.Debug,
			WaitForComponentReady: true,
			ReadyConsolePrefix:    componentReadyConsolePrefix,
			Refs:                  []pipeline.ScreenshotRef{job.Capture},
			Scale:                 scale,
			OnLayoutState:         capture.Handle,
			PaperViewport:         true,
		})
		if err == nil && len(results) == 0 {
			err = errors.New("no screenshot produced")
		}
		for _, r := range results {
			if err == nil && r.Error != nil {
				err = r.Error
			}
		}
		if err != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to capture %s", job.Ref))
			return nil, nil, RuntimeErrorf("screenshot %s: %w", job.Ref, err)
		}
		spinner.Stop()

		layoutState := ""
		if capture.SnapshotPath != "" {
			if _, statErr := os.Stat(capture.SnapshotPath); statErr == nil {
				layoutState = capture.SnapshotPath
			}
		}
		for _, f := range capture.Findings {
			logger.Warnf("%s", f)
		}
		warnings = append(warnings, capture.Warnings(job.Ref)...)
		for _, r := range results {
			entries = append(entries, buildlog.ScreenshotEntry{
				Ref:         job.Ref,
				Page:        r.Page,
				LayoutPage:  r.PageName,
				Path:        r.FilePath,
				Width:       r.Width,
				Height:      r.Height,
				LayoutState: layoutState,
			})
		}
	}
	return entries, warnings, nil
}

// printScreenshotSummary lists the --screenshot PNGs.
func printScreenshotSummary(ctx context.Context, out *Output, projectRoot string, entries []buildlog.ScreenshotEntry) {
	style := StyleFromContext(ctx)
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		label := e.Ref
		if e.Page > 0 {
			label += fmt.Sprintf(" page %d", e.Page)
		}
		items = append(items, fmt.Sprintf("%s %s %s (%dx%d)", label, style.Dim.Sprint(SymbolArrow), FormatPath(pathutil.RelPath(projectRoot, e.Path)), e.Width, e.Height))
	}
	out.Blank()
	out.Summary(fmt.Sprintf("Generated %d screenshot(s):", len(entries)), items)
}
