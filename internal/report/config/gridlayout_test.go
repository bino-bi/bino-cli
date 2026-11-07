package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsesGridCellLayout(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"standalone grid with equal rows", `{"kind":"Grid","spec":{"cellHeight":"equal","children":[]}}`, true},
		{"standalone grid with defaults", `{"kind":"Grid","spec":{"cellHeight":"auto","cellWidth":"auto","cellVerticalAlign":"top","cellHorizontalAlign":"left","children":[]}}`, false},
		{"plain grid", `{"kind":"Grid","spec":{"cellGap":"4px","children":[{"row":0,"column":0,"kind":"Text"}]}}`, false},
		{"child caption", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","caption":"North"}]}}`, true},
		{"empty caption", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","caption":""}]}}`, false},
		{"grid inside a card inside a page", `{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"Grid","spec":{"cellVerticalAlign":"baseline"}}]}}]}}`, true},
		{"field name outside a grid", `{"kind":"Table","spec":{"cellHeight":"equal"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := UsesGridCellLayout(docs); got != tt.want {
				t.Fatalf("UsesGridCellLayout = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUsesGridCellLayout_SmallMultiple(t *testing.T) {
	docs := []Document{{Kind: "LayoutPage", Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[{"kind":"SmallMultiple","ref":"sm"}]}}`)}}
	if !UsesGridCellLayout(docs) {
		t.Fatal("a SmallMultiple always renders as an aligned grid")
	}
}

// The panel placeholders must survive environment expansion at load and the
// params of a parameterized page, so the renderer can replace them per panel.
func TestPanelPlaceholdersSurviveLoadAndPageParams(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
  params:
    - name: REGION
      default: north
spec:
  children:
    - kind: SmallMultiple
      metadata:
        name: sm
      spec:
        dataset: profit
        unitScaling: 10
        scale: "0.5"
        caption: "${panel.index}: ${panel.label}"
        base:
          kind: ChartTime
          spec:
            chartTitle: "${panel.label} in ${REGION}"
        panels:
          by: rowGroup
`
	if err := os.WriteFile(filepath.Join(root, "page.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var page *Document
	for i := range docs {
		if docs[i].Kind == "LayoutPage" {
			page = &docs[i]
		}
	}
	if page == nil {
		t.Fatal("no page loaded")
	}
	for _, raw := range []json.RawMessage{page.Raw, mustExpand(page)} {
		s := string(raw)
		if !strings.Contains(s, "${panel.label}") || !strings.Contains(s, "${panel.index}") {
			t.Fatalf("placeholders lost: %s", s)
		}
	}
	if s := string(mustExpand(page)); !strings.Contains(s, "${panel.label} in south") {
		t.Fatalf("page param not expanded next to the placeholder: %s", s)
	}
}

func mustExpand(page *Document) json.RawMessage {
	raw := page.OriginalRaw
	if raw == nil {
		raw = page.Raw
	}
	out, _ := ExpandDocParams(raw, page.Params, map[string]string{"REGION": "south"})
	return out
}
