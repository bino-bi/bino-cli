package lint

import (
	"strings"
	"testing"

	"bino.bi/bino/internal/report/annotation"
)

func panelNote(t *testing.T, panel, point string) Document {
	t.Helper()
	target := `{"kind":"SmallMultiple","name":"sm","panel":"` + panel + `"`
	if point != "" {
		target += `,"point":` + point
	}
	return lintDoc(t, "Annotation", "a", `{"target":`+target+`},"title":"T"}`)
}

const smChild = `{"kind":"SmallMultiple","ref":"sm"}`

func TestAnnotationTargetMissing_Panel(t *testing.T) {
	byPanels := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},"panels":{"by":"rowGroup"}}`)
	items := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},"panels":{"items":[{"label":"North"},{"label":"South"}]}}`)

	t.Run("item label", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, items, panelNote(t, "South", ""), pageWith(t, "p", smChild)), "", 0, "")
	})
	t.Run("unknown item label", func(t *testing.T) {
		findings := runAnnotationRule(annotationTargetMissing, items, panelNote(t, "West", ""), pageWith(t, "p", smChild))
		wantFindings(t, findings, "annotation-target-missing", 1, "error")
		if findings[0].Path != "spec.target.panel" || !strings.Contains(findings[0].Message, `has no panel "West"`) {
			t.Errorf("finding = %+v", findings[0])
		}
	})
	t.Run("by panels are only known with the data", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, byPanels, panelNote(t, "West", ""), pageWith(t, "p", smChild)), "", 0, "")
	})
	t.Run("in a card", func(t *testing.T) {
		card := `{"kind":"LayoutCard","spec":{"children":[` + smChild + `]}}`
		wantFindings(t, runAnnotationRule(annotationTargetMissing, items, panelNote(t, "North", ""), pageWith(t, "p", card)), "", 0, "")
	})
	t.Run("SmallMultiple not on a page", func(t *testing.T) {
		findings := runAnnotationRule(annotationTargetMissing, items, panelNote(t, "North", ""), pageWith(t, "p"))
		wantFindings(t, findings, "annotation-target-missing", 1, "error")
		if findings[0].Path != "spec.target.name" {
			t.Errorf("path = %s", findings[0].Path)
		}
	})
	t.Run("SmallMultiple as a whole", func(t *testing.T) {
		wantFindings(t, runAnnotationRule(annotationTargetMissing, byPanels, annotationOn(t, "SmallMultiple", "sm"), pageWith(t, "p", smChild)), "", 0, "")
	})
}

func TestAnnotationPointFindings_Panel(t *testing.T) {
	sm := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},
		"panels":{"by":"rowGroup","items":[{"label":"Average","dataset":"avg"}]}}`)
	rows := func(binding string) annotation.Rows {
		switch binding {
		case "profit":
			return annotation.Rows{
				{"rowGroup": "Berlin", "date": "2024-01-01", "ac1": 1},
				{"rowGroup": "Berlin", "date": "2025-01-01", "ac1": 2},
				{"rowGroup": "Munich", "date": "2024-01-01", "ac1": 3},
			}
		case "avg":
			return annotation.Rows{{"date": "2023-01-01", "ac1": 2}}
		}
		return nil
	}
	run := func(note Document) []Finding {
		return AnnotationPointFindings([]Document{sm, note, pageWith(t, "p", smChild)}, rows)
	}

	t.Run("point in the panel", func(t *testing.T) {
		wantFindings(t, run(panelNote(t, "Berlin", `{"date":"2025-01-01","scenario":"ac1"}`)), "", 0, "")
	})
	t.Run("point only in another panel", func(t *testing.T) {
		findings := run(panelNote(t, "Munich", `{"date":"2025-01-01","scenario":"ac1"}`))
		wantFindings(t, findings, "annotation-point-missing", 1, "warning")
		if findings[0].Path != "spec.target.point" {
			t.Errorf("path = %s", findings[0].Path)
		}
	})
	t.Run("a field the base kind does not have", func(t *testing.T) {
		findings := run(panelNote(t, "Berlin", `{"category":"x"}`))
		wantFindings(t, findings, "annotation-point-missing", 1, "warning")
		if !strings.Contains(findings[0].Message, "a data point of ChartTime has no category") {
			t.Errorf("message = %s", findings[0].Message)
		}
	})
	t.Run("panel that the data does not have", func(t *testing.T) {
		findings := run(panelNote(t, "Hamburg", ""))
		wantFindings(t, findings, "annotation-target-missing", 1, "error")
		if findings[0].Path != "spec.target.panel" {
			t.Errorf("path = %s", findings[0].Path)
		}
	})
	t.Run("item with its own dataset", func(t *testing.T) {
		wantFindings(t, run(panelNote(t, "Average", `{"date":"2023-01-01"}`)), "", 0, "")
		wantFindings(t, run(panelNote(t, "Average", `{"date":"2024-01-01"}`)), "annotation-point-missing", 1, "warning")
	})
	t.Run("point without panel", func(t *testing.T) {
		note := lintDoc(t, "Annotation", "a", `{"target":{"kind":"SmallMultiple","name":"sm","point":{"date":"2024-01-01"}}}`)
		findings := run(note)
		wantFindings(t, findings, "annotation-point-missing", 1, "warning")
		if !strings.Contains(findings[0].Message, "set panel") {
			t.Errorf("message = %s", findings[0].Message)
		}
	})
	t.Run("unknown data", func(t *testing.T) {
		unknown := func(string) annotation.Rows { return nil }
		note := panelNote(t, "Hamburg", `{"date":"2025-01-01"}`)
		wantFindings(t, AnnotationPointFindings([]Document{sm, note, pageWith(t, "p", smChild)}, unknown), "", 0, "")
	})
}

// Lint builds the by panels as the build does: after order and limit.
func TestAnnotationPointFindings_PanelLimitAndOrder(t *testing.T) {
	rows := func(string) annotation.Rows {
		return annotation.Rows{
			{"rowGroup": "A", "ac1": 1}, {"rowGroup": "B", "ac1": 4}, {"rowGroup": "C", "ac1": 3}, {"rowGroup": "D", "ac1": 2},
		}
	}
	run := func(panels, panel string) []Finding {
		sm := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},"panels":`+panels+`}`)
		return AnnotationPointFindings([]Document{sm, panelNote(t, panel, ""), pageWith(t, "p", smChild)}, rows)
	}
	wantFindings(t, run(`{"by":"rowGroup","limit":2}`, "B"), "", 0, "")
	findings := run(`{"by":"rowGroup","limit":2}`, "C")
	wantFindings(t, findings, "annotation-target-missing", 1, "error")
	if findings[0].Path != "spec.target.panel" {
		t.Errorf("path = %s", findings[0].Path)
	}
	// by ac1 descending the first two panels are B and C
	wantFindings(t, run(`{"by":"rowGroup","order":"ac1 desc","limit":2}`, "C"), "", 0, "")
	wantFindings(t, run(`{"by":"rowGroup","order":"ac1 desc","limit":2}`, "A"), "annotation-target-missing", 1, "error")
}

func TestAnnotationPanelAmbiguous(t *testing.T) {
	rows := func(string) annotation.Rows {
		return annotation.Rows{{"rowGroup": "Berlin", "date": "2024-01-01", "ac1": 1}, {"rowGroup": "Munich", "date": "2025-01-01", "ac1": 2}}
	}
	t.Run("two items with one label", func(t *testing.T) {
		sm := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},"panels":{"items":[{"label":"North"},{"label":"North"},{"label":"South"}]}}`)
		findings := runAnnotationRule(annotationTargetMissing, sm, panelNote(t, "North", ""), pageWith(t, "p", smChild))
		wantFindings(t, findings, "annotation-panel-ambiguous", 1, "warning")
		if findings[0].Path != "spec.target.panel" {
			t.Errorf("path = %s", findings[0].Path)
		}
		wantFindings(t, runAnnotationRule(annotationTargetMissing, sm, panelNote(t, "South", ""), pageWith(t, "p", smChild)), "", 0, "")
		// the data adds nothing to it
		wantFindings(t, AnnotationPointFindings([]Document{sm, panelNote(t, "North", ""), pageWith(t, "p", smChild)}, rows), "", 0, "")
	})
	t.Run("a by value equal to an item label", func(t *testing.T) {
		sm := lintDoc(t, "SmallMultiple", "sm", `{"dataset":"profit","base":{"kind":"ChartTime"},"panels":{"by":"rowGroup","items":[{"label":"Berlin","filter":"1=1"}]}}`)
		docs := []Document{sm, panelNote(t, "Berlin", ""), pageWith(t, "p", smChild)}
		wantFindings(t, runAnnotationRule(annotationTargetMissing, docs...), "", 0, "")
		wantFindings(t, AnnotationPointFindings(docs, rows), "annotation-panel-ambiguous", 1, "warning")
		// the by panel is checked too: the item has the point, the by panel is one place more
		wantFindings(t, AnnotationPointFindings([]Document{sm, panelNote(t, "Munich", ""), pageWith(t, "p", smChild)}, rows), "", 0, "")
	})
}
