package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/annotation"
	"bino.bi/bino/internal/report/spec"
)

func lintDoc(t *testing.T, kind, name, specJSON string) Document {
	t.Helper()
	raw := fmt.Sprintf(`{"metadata":{"name":%q},"spec":%s}`, name, specJSON)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("bad json: %s", raw)
	}
	return Document{Kind: kind, Name: name, File: name + ".yaml", Position: 1, Raw: json.RawMessage(raw)}
}

func annotationOn(t *testing.T, kind, target string) Document {
	t.Helper()
	return lintDoc(t, "Annotation", "a", fmt.Sprintf(`{"target":{"kind":%q,"name":%q},"title":"T"}`, kind, target))
}

func pageWith(t *testing.T, name string, children ...string) Document {
	t.Helper()
	return lintDoc(t, "LayoutPage", name, `{"children":[`+strings.Join(children, ",")+`]}`)
}

func runAnnotationRule(rule Rule, docs ...Document) []Finding {
	return rule.Check(context.Background(), docs)
}

func wantFindings(t *testing.T, findings []Finding, ruleID string, count int, severity string) {
	t.Helper()
	if len(findings) != count {
		t.Fatalf("got %d findings, want %d: %+v", len(findings), count, findings)
	}
	for _, f := range findings {
		if f.RuleID != ruleID || f.Severity != severity {
			t.Errorf("finding = %+v, want rule %s severity %s", f, ruleID, severity)
		}
	}
}

const (
	chartChild = `{"kind":"ChartTime","ref":"trend"}`
	listAll    = `{"kind":"AnnotationList","spec":{}}`
)

func TestAnnotationTargetMissing(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)

	t.Run("on a page", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, annotationOn(t, "ChartTime", "trend"), pageWith(t, "p", chartChild)), "", 0, "")
	})
	t.Run("inline child by name", func(t *testing.T) {
		inline := `{"kind":"Table","metadata":{"name":"t"},"spec":{"dataset":"sales"}}`
		wantFindings(t, runAnnotationRule(annotationTargetMissing, annotationOn(t, "Table", "t"), pageWith(t, "p", inline)), "", 0, "")
	})
	t.Run("in a card by ref", func(t *testing.T) {
		card := lintDoc(t, "LayoutCard", "c", `{"children":[`+chartChild+`]}`)
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, card, annotationOn(t, "ChartTime", "trend"), pageWith(t, "p", `{"kind":"LayoutCard","ref":"c"}`)), "", 0, "")
	})
	t.Run("in an inline card", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, annotationOn(t, "ChartTime", "trend"), pageWith(t, "p", `{"kind":"LayoutCard","spec":{"children":[`+chartChild+`]}}`)), "", 0, "")
	})
	t.Run("on no page", func(t *testing.T) {
		findings := runAnnotationRule(annotationTargetMissing, chart, annotationOn(t, "ChartTime", "trend"), pageWith(t, "p"))
		wantFindings(t, findings, "annotation-target-missing", 1, "error")
		if findings[0].Path != "spec.target.name" || !strings.Contains(findings[0].Message, "not on any") {
			t.Errorf("finding = %+v", findings[0])
		}
	})
	t.Run("kind must match", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, annotationOn(t, "Table", "trend"), pageWith(t, "p", chartChild)), "annotation-target-missing", 1, "error")
	})
	t.Run("the end of an arrow", func(t *testing.T) {
		arrow := lintDoc(t, "Annotation", "a", `{"mark":"arrow","target":{"kind":"ChartTime","name":"trend"},"to":{"kind":"Table","name":"gone"}}`)
		findings := runAnnotationRule(annotationTargetMissing, chart, arrow, pageWith(t, "p", chartChild))
		wantFindings(t, findings, "annotation-target-missing", 1, "error")
		if findings[0].Path != "spec.to.name" {
			t.Errorf("path = %s", findings[0].Path)
		}
	})
	for name, parent := range map[string]string{
		"grid child": `{"kind":"Grid","metadata":{"name":"g"},"spec":{"children":[{"row":"a","column":"b","kind":"Table","ref":"t"}]}}`,
		"tree child": `{"kind":"Tree","metadata":{"name":"g"},"spec":{"nodes":[{"id":"n","kind":"Table","ref":"t"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			findings := runAnnotationRule(annotationTargetMissing, lintDoc(t, "Table", "t", `{"dataset":"sales"}`), annotationOn(t, "Table", "t"), pageWith(t, "p", parent))
			wantFindings(t, findings, "annotation-target-missing", 1, "error")
			if !strings.Contains(findings[0].Message, "not supported yet") {
				t.Errorf("message = %s", findings[0].Message)
			}
		})
	}
	t.Run("grid itself is a target", func(t *testing.T) {
		grid := `{"kind":"Grid","metadata":{"name":"g"},"spec":{"children":[]}}`
		wantFindings(t, runAnnotationRule(annotationTargetMissing, annotationOn(t, "Grid", "g"), pageWith(t, "p", grid)), "", 0, "")
	})
}

func TestAnnotationTargetMissing_ArtefactsAndConstraints(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	note := annotationOn(t, "ChartTime", "trend")
	onlyHere := lintDoc(t, "ReportArtefact", "here", `{"title":"t","layoutPages":["p"]}`)
	other := lintDoc(t, "ReportArtefact", "other", `{"title":"t","layoutPages":["q"]}`)

	t.Run("one artefact showing the component is enough", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, note, onlyHere, other, pageWith(t, "p", chartChild), pageWith(t, "q")), "", 0, "")
	})
	t.Run("no artefact shows the page", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, note, other, pageWith(t, "p", chartChild), pageWith(t, "q")), "annotation-target-missing", 1, "error")
	})
	t.Run("a child constraint hides the target", func(t *testing.T) {
		child := `{"kind":"ChartTime","ref":"trend","metadata":{"constraints":["labels.env==prod"]}}`
		art := onlyHere
		art.Labels = map[string]string{"env": "dev"}
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, note, art, pageWith(t, "p", child)), "annotation-target-missing", 1, "error")
	})
	t.Run("an annotation filtered away for the artefact is no finding", func(t *testing.T) {
		filtered := note
		c, err := spec.ParseMixedConstraints([]any{"labels.env==prod"})
		if err != nil {
			t.Fatal(err)
		}
		filtered.Constraints = c
		art := onlyHere
		art.Labels = map[string]string{"env": "dev"}
		wantFindings(t, runAnnotationRule(annotationTargetMissing, chart, filtered, art, pageWith(t, "p")), "", 0, "")
	})
}

func TestAnnotationArrowPages(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	table := lintDoc(t, "Table", "t", `{"dataset":"sales"}`)
	arrow := lintDoc(t, "Annotation", "a", `{"mark":"arrow","target":{"kind":"ChartTime","name":"trend"},"to":{"kind":"Table","name":"t"}}`)
	tableChild := `{"kind":"Table","ref":"t"}`

	wantFindings(t, runAnnotationRule(annotationArrowPages, chart, table, arrow, pageWith(t, "p", chartChild, tableChild)), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationArrowPages, chart, table, arrow, pageWith(t, "p", chartChild), pageWith(t, "q", tableChild)), "annotation-arrow-pages", 1, "error")
	// a missing end is the other rule's finding
	wantFindings(t, runAnnotationRule(annotationArrowPages, chart, arrow, pageWith(t, "p", chartChild)), "", 0, "")
}

func TestAnnotationListUnknown(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	note := annotationOn(t, "ChartTime", "trend")

	wantFindings(t, runAnnotationRule(annotationListUnknown, chart, note, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["a"]}}`)), "", 0, "")
	findings := runAnnotationRule(annotationListUnknown, chart, note, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["a","nope"]}}`))
	wantFindings(t, findings, "annotation-list-unknown", 1, "error")
	if findings[0].Path != "spec.children[1].spec.annotations[1]" {
		t.Errorf("path = %s", findings[0].Path)
	}

	t.Run("filtered away for the artefact", func(t *testing.T) {
		filtered := note
		c, err := spec.ParseMixedConstraints([]any{"labels.env==prod"})
		if err != nil {
			t.Fatal(err)
		}
		filtered.Constraints = c
		art := lintDoc(t, "ReportArtefact", "r", `{"title":"t"}`)
		art.Labels = map[string]string{"env": "dev"}
		wantFindings(t, runAnnotationRule(annotationListUnknown, chart, filtered, art, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["a"]}}`)), "annotation-list-unknown", 1, "error")
	})
	t.Run("in a standalone list", func(t *testing.T) {
		list := lintDoc(t, "AnnotationList", "l", `{"annotations":["nope"]}`)
		findings := runAnnotationRule(annotationListUnknown, chart, note, list, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","ref":"l"}`))
		wantFindings(t, findings, "annotation-list-unknown", 1, "error")
		if findings[0].File != "l.yaml" || findings[0].Path != "spec.annotations[0]" {
			t.Errorf("finding = %+v", findings[0])
		}
	})
	t.Run("reported once with two artefacts", func(t *testing.T) {
		one, two := lintDoc(t, "ReportArtefact", "r1", `{"title":"t"}`), lintDoc(t, "ReportArtefact", "r2", `{"title":"t"}`)
		wantFindings(t, runAnnotationRule(annotationListUnknown, chart, note, one, two, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["nope"]}}`)), "annotation-list-unknown", 1, "error")
	})
}

func TestAnnotationListOffPage(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	note := annotationOn(t, "ChartTime", "trend")
	list := `{"kind":"AnnotationList","spec":{"annotations":["a"]}}`

	wantFindings(t, runAnnotationRule(annotationListOffPage, chart, note, pageWith(t, "p", chartChild, list)), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationListOffPage, chart, note, pageWith(t, "p", chartChild), pageWith(t, "q", list)), "annotation-list-off-page", 1, "error")
	// an unknown name is the other rule's finding
	wantFindings(t, runAnnotationRule(annotationListOffPage, chart, pageWith(t, "p", chartChild, list)), "", 0, "")
}

func TestAnnotationUnlisted(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	note := annotationOn(t, "ChartTime", "trend")

	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, note, pageWith(t, "p", chartChild)), "annotation-unlisted", 1, "info")
	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, note, pageWith(t, "p", chartChild, listAll)), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, note, pageWith(t, "p", chartChild, `{"kind":"AnnotationList"}`)), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, note, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["a"]}}`)), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, note, pageWith(t, "p", chartChild, `{"kind":"AnnotationList","spec":{"annotations":["other"]}}`)), "annotation-unlisted", 1, "info")
	// nothing to list
	bare := lintDoc(t, "Annotation", "b", `{"mark":"frame","target":{"kind":"ChartTime","name":"trend"}}`)
	wantFindings(t, runAnnotationRule(annotationUnlisted, chart, bare, pageWith(t, "p", chartChild)), "", 0, "")
}

func TestAnnotationListInLive(t *testing.T) {
	report := lintDoc(t, "ReportArtefact", "r", `{"title":"t","layoutPages":["p"]}`)
	withList := pageWith(t, "p", chartChild, listAll)
	viaArtefact := lintDoc(t, "LiveReportArtefact", "live", `{"title":"t","routes":{"/":{"artefact":"r"}}}`)
	viaPages := lintDoc(t, "LiveReportArtefact", "live", `{"title":"t","routes":{"/":{"layoutPages":["p*"]}}}`)
	otherPage := lintDoc(t, "LiveReportArtefact", "live", `{"title":"t","routes":{"/":{"layoutPages":["q"]}}}`)

	wantFindings(t, runAnnotationRule(annotationListInLive, report, withList, viaArtefact), "annotation-list-in-live", 1, "warning")
	wantFindings(t, runAnnotationRule(annotationListInLive, report, withList, viaPages), "annotation-list-in-live", 1, "warning")
	wantFindings(t, runAnnotationRule(annotationListInLive, report, withList, otherPage), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationListInLive, report, pageWith(t, "p", chartChild), viaArtefact), "", 0, "")
	wantFindings(t, runAnnotationRule(annotationListInLive, withList), "", 0, "")
}

func TestAnnotationPointFindings(t *testing.T) {
	chart := lintDoc(t, "ChartTime", "trend", `{"dataset":"sales"}`)
	onPoint := lintDoc(t, "Annotation", "a", `{"target":{"kind":"ChartTime","name":"trend","point":{"date":"1999-01-01"}}}`)
	rows := func(binding string) annotation.Rows {
		if binding == "sales" {
			return annotation.Rows{{"date": "2024-01-01"}}
		}
		return nil
	}

	findings := AnnotationPointFindings([]Document{chart, onPoint, pageWith(t, "p", chartChild)}, rows)
	wantFindings(t, findings, "annotation-point-missing", 1, "warning")
	if findings[0].Path != "spec.target.point" {
		t.Errorf("path = %s", findings[0].Path)
	}

	t.Run("unknown data", func(t *testing.T) {
		unknown := func(string) annotation.Rows { return nil }
		wantFindings(t, AnnotationPointFindings([]Document{chart, onPoint, pageWith(t, "p", chartChild)}, unknown), "", 0, "")
	})
	t.Run("point in the data", func(t *testing.T) {
		good := lintDoc(t, "Annotation", "a", `{"target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01"}}}`)
		wantFindings(t, AnnotationPointFindings([]Document{chart, good, pageWith(t, "p", chartChild)}, rows), "", 0, "")
	})
	t.Run("not on a page is the other rule", func(t *testing.T) {
		wantFindings(t, AnnotationPointFindings([]Document{chart, onPoint, pageWith(t, "p")}, rows), "", 0, "")
	})
	t.Run("override of the dataset wins", func(t *testing.T) {
		child := `{"kind":"ChartTime","ref":"trend","spec":{"dataset":"other"}}`
		wantFindings(t, AnnotationPointFindings([]Document{chart, onPoint, pageWith(t, "p", child)}, rows), "", 0, "")
	})
	t.Run("the end of an arrow", func(t *testing.T) {
		arrow := lintDoc(t, "Annotation", "a", `{"mark":"arrow","target":{"kind":"ChartTime","name":"trend","point":{"date":"2024-01-01"}},"to":{"kind":"ChartTime","name":"trend","point":{"date":"1999-01-01"}}}`)
		findings := AnnotationPointFindings([]Document{chart, arrow, pageWith(t, "p", chartChild)}, rows)
		wantFindings(t, findings, "annotation-point-missing", 1, "warning")
		if findings[0].Path != "spec.to.point" {
			t.Errorf("path = %s", findings[0].Path)
		}
	})
}

func TestAnnotationList_CountsAsASlotWithoutSpec(t *testing.T) {
	children := []layoutChildSpec{{Kind: "AnnotationList"}}
	count, findings := countEffectiveChildren(children, &spec.ConstraintContext{}, Document{}, nil, nil, "spec.children")
	if count != 1 || len(findings) != 0 {
		t.Fatalf("count = %d, findings = %+v", count, findings)
	}
}
