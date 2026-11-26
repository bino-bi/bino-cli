package render

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
)

func annotationDocument(name, spec string) config.Document {
	return makeTestDoc("Annotation", name, json.RawMessage(fmt.Sprintf(`{"metadata":{"name":%q},"spec":%s}`, name, spec)))
}

func annotationTestPage(children string) config.Document {
	return makeTestDoc("LayoutPage", "page", json.RawMessage(fmt.Sprintf(`{"metadata":{"name":"page"},"spec":{"pageLayout":"2-over-1","children":[%s]}}`, children)))
}

var (
	chartRefChild  = `{"kind":"ChartTime","ref":"trend"}`
	chartDoc       = makeTestDoc("ChartTime", "trend", json.RawMessage(`{"metadata":{"name":"trend"},"spec":{"dataset":"sales"}}`))
	salesData      = []dataset.Result{{Name: "sales", Data: json.RawMessage(`[{"date":"2024-01-01","ac1":1},{"date":"2025-01-01","ac1":2}]`)}}
	marker2024     = annotationDocument("first", `{"target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01","scenario":"ac1"}},"title":"First"}`)
	marker2025     = annotationDocument("second", `{"target":{"kind":"ChartTime","name":"trend","point":{"date":"2025-01-01","scenario":"ac1"}},"text":"Second **bold**"}`)
	missingPoint   = annotationDocument("missing", `{"target":{"kind":"ChartTime","name":"trend","point":{"date":"1999-01-01"}},"title":"Missing"}`)
	frameNoText    = annotationDocument("framed", `{"mark":"frame","color":"negative","target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01","part":"total"}}}`)
	arrowNoText    = annotationDocument("trend_arrow", `{"mark":"arrow","target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01"}},"to":{"kind":"ChartTime","name":"trend","point":{"date":"2025-01-01"}}}`)
	annotationsAll = `{"kind":"AnnotationList","metadata":{"name":"list"},"spec":{}}`
)

func renderAnnotations(t *testing.T, mode Mode, docs ...config.Document) string {
	t.Helper()
	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, salesData, "en", "", "", mode, nil, nil, "v1.0.0-next.30", nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return string(result.HTML)
}

var attrPattern = func(tag string) *regexp.Regexp {
	return regexp.MustCompile(`<` + tag + `[^>]* items='([^']*)'`)
}

func layerItems(t *testing.T, page string) []annotationLayerItem {
	t.Helper()
	m := attrPattern("bn-annotation-layer").FindStringSubmatch(page)
	if m == nil {
		return nil
	}
	var items []annotationLayerItem
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func listItems(t *testing.T, page string) []annotationListItem {
	t.Helper()
	m := attrPattern("bn-annotation-list").FindStringSubmatch(page)
	if m == nil {
		return nil
	}
	var items []annotationListItem
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func labelsOf(items []annotationListItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.Label+":"+item.Title)
	}
	return strings.Join(parts, ",")
}

func TestAnnotations_ListOrderDecidesTheNumbers(t *testing.T) {
	page := annotationTestPage(chartRefChild + `,{"kind":"AnnotationList","spec":{"annotations":["second","first"]}}`)
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, marker2025, page)

	items := listItems(t, out)
	if len(items) != 2 || items[0].Label != "1" || items[0].HTML != "Second <strong>bold</strong>" || items[1].Label != "2" || items[1].Title != "First" {
		t.Fatalf("list items = %+v", items)
	}
	marks := map[string]string{}
	for _, item := range layerItems(t, out) {
		marks[item.Point.Date] = item.Label
	}
	if marks["2025-01-01"] != "1" || marks["2024-01-01"] != "2" {
		t.Fatalf("marks and list disagree: %v", marks)
	}
}

func TestAnnotations_RemainingAnnotationsFollowInManifestOrder(t *testing.T) {
	page := annotationTestPage(chartRefChild + `,{"kind":"AnnotationList","spec":{"annotations":["second"]}}`)
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, marker2025, page)
	labels := map[string]string{}
	for _, item := range layerItems(t, out) {
		labels[item.Point.Date] = item.Label
	}
	if labels["2025-01-01"] != "1" || labels["2024-01-01"] != "2" {
		t.Fatalf("labels = %v", labels)
	}
	if items := listItems(t, out); len(items) != 1 {
		t.Fatalf("the list must show only what it names: %+v", items)
	}
}

func TestAnnotations_ListWithoutSpecShowsAllOfThePage(t *testing.T) {
	for _, list := range []string{annotationsAll, `{"kind":"AnnotationList"}`} {
		out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, marker2025, annotationTestPage(chartRefChild+","+list))
		if got := labelsOf(listItems(t, out)); got != "1:First,2:" {
			t.Fatalf("%s: list = %s", list, got)
		}
	}
}

func TestAnnotations_NoTextNoNumberAndNoListEntry(t *testing.T) {
	out := renderAnnotations(t, ModeBuild, chartDoc, frameNoText, marker2024, annotationTestPage(chartRefChild+","+annotationsAll))
	items := layerItems(t, out)
	if len(items) != 2 || items[0].Label != "" || items[0].Mark != "frame" || items[0].Color != "negative" || items[1].Label != "1" || items[1].Mark != "marker" {
		t.Fatalf("layer = %+v", items)
	}
	if got := labelsOf(listItems(t, out)); got != "1:First" {
		t.Fatalf("list = %s", got)
	}
}

func TestAnnotations_MissingPointHasNoNumberAndLaterOnesMoveUp(t *testing.T) {
	out := renderAnnotations(t, ModeBuild, chartDoc, missingPoint, marker2024, annotationTestPage(chartRefChild+","+annotationsAll))
	if got := labelsOf(listItems(t, out)); got != ":Missing,1:First" {
		t.Fatalf("list = %s", got)
	}
	items := layerItems(t, out)
	if len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("a missing point must draw nothing: %+v", items)
	}
}

func TestAnnotations_MissingPointIsAWarning(t *testing.T) {
	docs := []config.Document{chartDoc, missingPoint, annotationTestPage(chartRefChild)}
	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, salesData, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.30", nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "annotation-point-missing") || !strings.Contains(result.Warnings[0], "missing") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestAnnotations_UnknownDataIsNotChecked(t *testing.T) {
	result, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{chartDoc, missingPoint, annotationTestPage(chartRefChild)}, "en", "", "", ModeBuild, "v1.0.0-next.30")
	if err != nil {
		t.Fatal(err)
	}
	if items := layerItems(t, string(result.HTML)); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("items = %+v", items)
	}
}

func TestAnnotations_DataSourceRowsAreChecked(t *testing.T) {
	sources := []datasource.Result{{Name: "raw", Data: json.RawMessage(`[{"date":"2024-01-01","ac1":1}]`)}}
	rc := &renderCtx{}
	if err := rc.withAnnotations([]config.Document{marker2024}, nil, sources, ModeBuild, "v1.0.0-next.30"); err != nil {
		t.Fatal(err)
	}
	if rows := rc.annotationData.rows("$raw"); len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if rows := rc.annotationData.rows("raw"); rows != nil {
		t.Fatalf("a bare name is a DataSet: %v", rows)
	}
}

func TestAnnotations_ComponentTwiceOnAPageGetsTwoMarks(t *testing.T) {
	page := annotationTestPage(chartRefChild + "," + chartRefChild)
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, page)
	items := layerItems(t, out)
	if len(items) != 2 || items[0].Target == items[1].Target || items[0].Label != "1" || items[1].Label != "1" {
		t.Fatalf("layer = %+v", items)
	}
	for _, item := range items {
		if !strings.Contains(out, "data-bino-annotation-target='"+item.Target+"'") {
			t.Errorf("no slot with target %s", item.Target)
		}
	}
}

func TestAnnotations_ArrowOnAPage(t *testing.T) {
	out := renderAnnotations(t, ModeBuild, chartDoc, arrowNoText, annotationTestPage(chartRefChild))
	items := layerItems(t, out)
	if len(items) != 1 || items[0].Mark != "arrow" || items[0].To != items[0].Target || items[0].ToPoint.Date != "2025-01-01" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_ArrowEndOnAnotherPageIsNotDrawn(t *testing.T) {
	other := annotationDocument("across", `{"mark":"arrow","target":{"kind":"ChartTime","name":"trend"},"to":{"kind":"Table","name":"elsewhere"},"title":"Across"}`)
	out := renderAnnotations(t, ModeBuild, chartDoc, other, annotationTestPage(chartRefChild+","+annotationsAll))
	if items := layerItems(t, out); items != nil {
		t.Fatalf("layer = %+v", items)
	}
	if got := labelsOf(listItems(t, out)); got != ":Across" {
		t.Fatalf("list = %s", got)
	}
}

func TestAnnotations_TargetInsideACard(t *testing.T) {
	card := `{"kind":"LayoutCard","spec":{"cardLayout":"split-horizontal","children":[` + chartRefChild + `,` + annotationsAll + `]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, annotationTestPage(card))
	if !regexp.MustCompile(`slot='card-slot-0'[^>]*data-bino-annotation-target='t1'`).MatchString(out) {
		t.Fatalf("no target on the card slot:\n%s", out)
	}
	if got := labelsOf(listItems(t, out)); got != "1:First" {
		t.Fatalf("list = %s", got)
	}
}

func TestAnnotations_ACardInATreeHasNoTargets(t *testing.T) {
	tree := `{"kind":"Tree","metadata":{"name":"tr"},"spec":{"nodes":[{"id":"n","kind":"LayoutCard","spec":{"children":[` + chartRefChild + `]}}]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, annotationTestPage(tree))
	if strings.Contains(out, "data-bino-annotation-target") || strings.Contains(out, "bn-annotation-layer") {
		t.Fatalf("a card in a tree must not count as a slot:\n%s", out)
	}
}

func TestAnnotations_ATreeNodeWithARefIsATarget(t *testing.T) {
	tree := `{"kind":"Tree","metadata":{"name":"tr"},"spec":{"edges":[],"nodes":[{"id":"n","kind":"ChartTime","ref":"trend"},{"id":"m","kind":"ChartTime","spec":{"dataset":"sales"}}]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, annotationTestPage(tree))
	if !regexp.MustCompile(`<div slot='n' data-bino-annotation-target='t1'[ >]`).MatchString(out) {
		t.Fatalf("no target on the tree node:\n%s", out)
	}
	if strings.Contains(out, "<div slot='m' data-bino-annotation-target") {
		t.Fatalf("an inline node has no name and cannot be a target:\n%s", out)
	}
	if items := layerItems(t, out); len(items) != 1 || items[0].Target != "t1" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestTreeScale(t *testing.T) {
	for scale, want := range map[string]string{`"auto"`: `scale='auto'`, `0.6`: `scale='0.6'`} {
		tree := `{"kind":"Tree","metadata":{"name":"tr"},"spec":{"edges":[],"scale":` + scale + `,"nodes":[]}}`
		out := renderAnnotations(t, ModeBuild, annotationTestPage(tree))
		if !strings.Contains(openTag(t, out, "bn-tree"), want) {
			t.Fatalf("scale %s: no %s on <bn-tree>:\n%s", scale, want, out)
		}
	}
}

func TestAnnotations_GridItselfIsATarget(t *testing.T) {
	grid := `{"kind":"Grid","metadata":{"name":"g"},"spec":{"rowHeaders":[],"columnHeaders":[],"children":[]}}`
	note := annotationDocument("on_grid", `{"target":{"kind":"Grid","name":"g"},"title":"Whole"}`)
	out := renderAnnotations(t, ModeBuild, note, annotationTestPage(grid))
	if items := layerItems(t, out); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_InlineChildIsATarget(t *testing.T) {
	inline := `{"kind":"ChartTime","metadata":{"name":"trend"},"spec":{"dataset":"sales"}}`
	out := renderAnnotations(t, ModeBuild, marker2024, annotationTestPage(inline))
	if items := layerItems(t, out); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_RefToAStandaloneList(t *testing.T) {
	list := makeTestDoc("AnnotationList", "comments", json.RawMessage(`{"metadata":{"name":"comments"},"spec":{"annotations":["first"],"selectedStyle":"corp"}}`))
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, list, annotationTestPage(chartRefChild+`,{"kind":"AnnotationList","ref":"comments"}`))
	if got := labelsOf(listItems(t, out)); got != "1:First" {
		t.Fatalf("list = %s", got)
	}
	if !strings.Contains(out, "selected-style='corp'") {
		t.Fatalf("the list lost its style:\n%s", out)
	}
}

func TestAnnotations_PageStyleReachesLayerAndList(t *testing.T) {
	page := makeTestDoc("LayoutPage", "page", json.RawMessage(`{"metadata":{"name":"page"},"spec":{"selectedStyle":"corp","pageLayout":"split-horizontal","children":[`+chartRefChild+","+annotationsAll+`]}}`))
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, page)
	for _, want := range []string{`<bn-annotation-layer slot='annotations' items=`, `<bn-annotation-list items=`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "selected-style='corp'"); got < 2 {
		t.Fatalf("selected-style on %d elements:\n%s", got, out)
	}
}

func TestAnnotations_PageWithoutAnnotationsIsUnchanged(t *testing.T) {
	out := renderAnnotations(t, ModeBuild, chartDoc, annotationTestPage(chartRefChild))
	for _, unwanted := range []string{"bn-annotation", "data-bino-annotation-target"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("found %s in:\n%s", unwanted, out)
		}
	}
}

func TestAnnotations_OnlyTheTargetSlotIsMarked(t *testing.T) {
	text := `{"kind":"Text","metadata":{"name":"note"},"spec":{"value":"x"}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, annotationTestPage(text+","+chartRefChild))
	if got := strings.Count(out, "data-bino-annotation-target"); got != 1 {
		t.Fatalf("%d targets in:\n%s", got, out)
	}
}

func TestAnnotations_PagesAreNumberedOnTheirOwn(t *testing.T) {
	tableDoc := makeTestDoc("Table", "products", json.RawMessage(`{"metadata":{"name":"products"},"spec":{"dataset":"sales"}}`))
	onTable := annotationDocument("on_table", `{"target":{"kind":"Table","name":"products"},"title":"Table"}`)
	pageTwo := makeTestDoc("LayoutPage", "two", json.RawMessage(`{"metadata":{"name":"two"},"spec":{"pageLayout":"split-horizontal","children":[{"kind":"Table","ref":"products"},`+annotationsAll+`]}}`))
	out := renderAnnotations(t, ModeBuild, chartDoc, tableDoc, marker2024, onTable, annotationTestPage(chartRefChild), pageTwo)
	if got := labelsOf(listItems(t, out)); got != "1:Table" {
		t.Fatalf("list on page two = %s", got)
	}
}

func TestAnnotations_ServeModeDropsEverythingAndKeepsTheSlot(t *testing.T) {
	page := annotationTestPage(chartRefChild + "," + annotationsAll + "," + `{"kind":"Text","metadata":{"name":"after"},"spec":{"value":"x"}}`)
	docs := []config.Document{chartDoc, marker2024, missingPoint, page}

	frame, _, err := GenerateFrameAndContext(context.Background(), docs, salesData, "en", "", ModeServe, nil, nil, "v1.0.0-next.30", nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	full := renderAnnotations(t, ModeServe, docs...)
	for name, out := range map[string]string{"frame": string(frame.ContextHTML), "full": full} {
		for _, unwanted := range []string{"bn-annotation", "data-bino-annotation-target", "bino-annotation-list"} {
			if strings.Contains(out, unwanted) {
				t.Fatalf("%s: found %s in:\n%s", name, unwanted, out)
			}
		}
		// the Text keeps its slot: the page must not shift when the list is gone
		if !regexp.MustCompile(`slot='slot-2'[^>]*data-bino-name='after'`).MatchString(out) {
			t.Fatalf("%s: the slot of the list was given away:\n%s", name, out)
		}
	}
}

func TestAnnotations_PreviewPathShowsThem(t *testing.T) {
	docs := []config.Document{chartDoc, marker2024, annotationTestPage(chartRefChild + "," + annotationsAll)}
	frame, _, err := GenerateFrameAndContext(context.Background(), docs, salesData, "en", "", ModePreview, nil, nil, "v1.0.0-next.30", nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if items := listItems(t, string(frame.ContextHTML)); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("list = %+v", items)
	}
	if items := layerItems(t, string(frame.ContextHTML)); len(items) != 1 {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_PresentationShowsThem(t *testing.T) {
	docs := []config.Document{chartDoc, marker2024, annotationTestPage(chartRefChild + "," + annotationsAll)}
	frame, _, err := GeneratePresentationFrameAndContext(context.Background(), docs, salesData, config.Artifact{}, config.PresentationConfig{}, nil, nil, "v1.0.0-next.30", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if items := listItems(t, string(frame.ContextHTML)); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("list = %+v", items)
	}
	if items := layerItems(t, string(frame.ContextHTML)); len(items) != 1 {
		t.Fatalf("layer = %+v", items)
	}
	standalone, _, err := GeneratePresentationHTML(context.Background(), docs, salesData, config.Artifact{}, config.PresentationConfig{}, nil, nil, "v1.0.0-next.30", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if items := layerItems(t, string(standalone.HTML)); len(items) != 1 {
		t.Fatalf("standalone layer = %+v", items)
	}
}

func TestAnnotations_TwoPlacesWithDifferentDatasets(t *testing.T) {
	other := `{"kind":"ChartTime","ref":"trend","spec":{"dataset":"unknown"}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, missingPoint, annotationTestPage(chartRefChild+","+other))
	if items := layerItems(t, out); len(items) != 2 {
		t.Fatalf("one place can have the point, so both keep the mark: %+v", items)
	}
}

func TestAnnotations_SeveralDatasetsAreNotChecked(t *testing.T) {
	chart := makeTestDoc("ChartTime", "trend", json.RawMessage(`{"metadata":{"name":"trend"},"spec":{"dataset":["sales","other"]}}`))
	out := renderAnnotations(t, ModeBuild, chart, missingPoint, annotationTestPage(chartRefChild))
	if items := layerItems(t, out); len(items) != 1 || items[0].Label != "1" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_EngineGate(t *testing.T) {
	docs := []config.Document{chartDoc, marker2024, annotationTestPage(chartRefChild)}
	_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.29")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.30") || !strings.Contains(err.Error(), "annotation-engine-version") {
		t.Fatalf("err = %v", err)
	}
	// without an Annotation, and in serve mode, an old engine stays fine
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs[:1], "en", "", "", ModeBuild, "v1.0.0-next.29"); err != nil {
		t.Fatalf("no annotations: %v", err)
	}
	if _, _, err := GenerateFrameAndContext(context.Background(), docs, nil, "en", "", ModeServe, nil, nil, "v1.0.0-next.29", nil, nil, "", ""); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestAnnotations_MarkupInTextIsEscapedInTheAttribute(t *testing.T) {
	danger := annotationDocument("danger", `{"target":{"kind":"ChartTime","name":"trend"},"title":"a'b<c","text":"x"}`)
	out := renderAnnotations(t, ModeBuild, chartDoc, danger, annotationTestPage(chartRefChild+","+annotationsAll))
	items := listItems(t, out)
	if len(items) != 1 || items[0].Title != "a'b<c" {
		t.Fatalf("items = %+v", items)
	}
}

func TestAnnotations_GridChildIsATarget(t *testing.T) {
	grid := `{"kind":"Grid","metadata":{"name":"g"},"spec":{"rowHeaders":["r"],"columnHeaders":["c"],"children":[{"row":0,"column":0,"kind":"ChartTime","ref":"trend"}]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, missingPoint, annotationTestPage(grid+","+annotationsAll))
	if !regexp.MustCompile(`<div slot='[^']*'[^>]*data-bino-annotation-target='t1'`).MatchString(out) {
		t.Fatalf("no target on the grid cell:\n%s", out)
	}
	// the point check runs for a grid child too: the missing point gets no mark
	if items := layerItems(t, out); len(items) != 1 || items[0].Label != "1" || items[0].Target != "t1" {
		t.Fatalf("layer = %+v", items)
	}
}

func TestAnnotations_ACardInAGridHasNoTargets(t *testing.T) {
	grid := `{"kind":"Grid","metadata":{"name":"g"},"spec":{"rowHeaders":["r"],"columnHeaders":["c"],"children":[{"row":0,"column":0,"kind":"LayoutCard","spec":{"children":[` + chartRefChild + `]}}]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, annotationTestPage(grid))
	if strings.Contains(out, "data-bino-annotation-target") || strings.Contains(out, "bn-annotation-layer") {
		t.Fatalf("a card in a grid must not count as a slot:\n%s", out)
	}
}

func TestAnnotations_DifferenceAndFrameText(t *testing.T) {
	difference := annotationDocument("growth", `{"mark":"difference","percent":true,"highlight":true,"digits":0,"sentiment":"good-down","target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01"}},"to":{"kind":"ChartTime","name":"trend","point":{"date":"2025-01-01"}}}`)
	share := annotationDocument("share", `{"mark":"frame","markText":"61%","target":{"kind":"ChartTime","name":"trend","point":{"date":"2025-01-01"}}}`)
	docs := []config.Document{chartDoc, difference, share, annotationTestPage(chartRefChild)}

	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, salesData, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.49", nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	items := layerItems(t, string(result.HTML))
	if len(items) != 2 {
		t.Fatalf("layer = %+v", items)
	}
	d := items[0]
	if d.Mark != "difference" || !d.Percent || !d.Highlight || d.Digits == nil || *d.Digits != 0 || d.Sentiment != "good-down" || d.ToPoint.Date != "2025-01-01" {
		t.Errorf("difference = %+v", d)
	}
	if f := items[1]; f.Mark != "frame" || f.Text != "61%" || f.Percent {
		t.Errorf("frame = %+v", f)
	}

	_, _, err = GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, salesData, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.48", nil, nil, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "stack-series-engine-version") {
		t.Errorf("an older engine must fail the render, got %v", err)
	}
}

func TestLabelsHideAttr(t *testing.T) {
	if got := labelsHideAttr([]any{float64(2022), " 2023-03 ", "2024-01-01", "2025-02-01T00:00:00Z", true}); got != "2022,2023-03,2024-01-01,2025-02-01" {
		t.Fatalf("got %q", got)
	}
	if got := labelsHideAttr(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAnnotations_GridChildAndTreeNodeCarryTheirTarget(t *testing.T) {
	grid := `{"kind":"Grid","metadata":{"name":"g"},"spec":{"rowHeaders":["r"],"columnHeaders":["a","b"],"children":[` +
		`{"row":0,"column":0,"kind":"ChartTime","ref":"trend"},` +
		`{"row":0,"column":1,"kind":"Table","metadata":{"name":"inline_t"},"spec":{"dataset":"sales"}}]}}`
	tree := `{"kind":"Tree","metadata":{"name":"tr"},"spec":{"edges":[],"nodes":[{"id":"n","kind":"ChartTime","ref":"trend"},{"id":"m","kind":"ChartTime","spec":{"dataset":"sales"}}]}}`
	// No annotation on the page: a client needs the target before there is one.
	out := renderAnnotations(t, ModeBuild, chartDoc, annotationTestPage(grid+","+tree))
	for _, want := range []string{
		`<div slot='[^']*' id='bino-charttime-trend' data-bino-target-kind='ChartTime' data-bino-target-name='trend'><bn-chart-time`,
		`<div slot='[^']*' id='bino-table-inline_t' data-bino-target-kind='Table' data-bino-target-name='inline_t'><bn-table`,
		`<div slot='n' data-bino-target-kind='ChartTime' data-bino-target-name='trend'>`,
		`<div slot='m'>`, // an inline node cannot be a target
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("no match for %s in:\n%s", want, out)
		}
	}
	if strings.Count(out, "data-bino-target-kind") != 3 {
		t.Errorf("want 3 targets in:\n%s", out)
	}
}

func TestAnnotations_CardInATreeHasNoTargetAttributes(t *testing.T) {
	grid := `{"kind":"Grid","spec":{"rowHeaders":["r"],"columnHeaders":["c"],"children":[{"row":0,"column":0,"kind":"ChartTime","ref":"trend"}]}}`
	tree := `{"kind":"Tree","metadata":{"name":"tr"},"spec":{"nodes":[{"id":"n","kind":"LayoutCard","spec":{"children":[` + grid + `]}}]}}`
	out := renderAnnotations(t, ModeBuild, chartDoc, annotationTestPage(tree))
	if !strings.Contains(out, "<bn-grid") || strings.Contains(out, "data-bino-target-") {
		t.Fatalf("a Grid in a card in a Tree has no targets:\n%s", out)
	}
}

func TestAnnotations_ItemsCarryTheName(t *testing.T) {
	out := renderAnnotations(t, ModeBuild, chartDoc, marker2024, arrowNoText, annotationTestPage(chartRefChild+","+annotationsAll))
	items := layerItems(t, out)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
		if item.SourceFile != "" || item.Line != 0 {
			t.Fatalf("a build has no source location: %+v", item)
		}
	}
	if strings.Join(names, ",") != "first,trend_arrow" {
		t.Fatalf("layer names = %v", names)
	}
	if list := listItems(t, out); len(list) != 1 || list[0].Name != "first" {
		t.Fatalf("list = %+v", list)
	}
}

func TestAnnotations_PreviewItemsCarryTheSourceLocation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("chart.yaml", "apiVersion: bino.bi/v1alpha1\nkind: ChartTime\nmetadata:\n  name: trend\nspec:\n  dataset: sales\n")
	write("notes.yaml", "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: other\nspec:\n  value: x\n---\n"+
		"apiVersion: bino.bi/v1alpha1\nkind: Annotation\nmetadata:\n  name: first\nspec:\n  target: {kind: ChartTime, name: trend, point: {date: \"2024-01-01\", scenario: ac1}}\n  title: First\n")
	write("page.yaml", "apiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: page\nspec:\n  children:\n    - kind: ChartTime\n      ref: trend\n    - kind: AnnotationList\n      metadata:\n        name: list\n      spec: {}\n")
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, salesData, "en", "", "", ModePreview, nil, nil, "v1.0.0-next.30", nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.yaml")
	layer := layerItems(t, string(result.HTML))
	if len(layer) != 1 || layer[0].Name != "first" || layer[0].SourceFile != notes || layer[0].Line != 8 {
		t.Fatalf("layer = %+v, want first at %s:8", layer, notes)
	}
	list := listItems(t, string(result.HTML))
	if len(list) != 1 || list[0].SourceFile != notes || list[0].Line != 8 {
		t.Fatalf("list = %+v", list)
	}
}
