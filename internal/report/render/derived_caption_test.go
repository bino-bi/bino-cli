package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func derivingDataSet(t *testing.T, name, field, slot, shift string) config.Document {
	t.Helper()
	raw := fmt.Sprintf(`{
		"apiVersion": "bino.bi/v1",
		"kind": "DataSet",
		"metadata": {"name": %q},
		"spec": {
			"query": "SELECT 1 AS ac1, '2024-01-31' AS date",
			%q: {%q: {"from": "ac1", "shift": %q, "grain": "month"}}
		}
	}`, name, field, slot, shift)
	return makeTestDoc("DataSet", name, json.RawMessage(raw))
}

func TestCollectInternationalizations_DerivedCaption(t *testing.T) {
	cases := []struct {
		name   string
		docs   []config.Document
		locale string
		want   string // expected synthesized value, "" for none
	}{
		{
			name:   "month shift captions PP",
			docs:   []config.Document{derivingDataSet(t, "sales", "derive", "pp1", "1 month")},
			locale: "en",
			want:   `{"global.pp1":"PP"}`,
		},
		{
			name:   "year shift keeps the engine default",
			docs:   []config.Document{derivingDataSet(t, "sales", "derive", "pp1", "1 year")},
			locale: "en",
		},
		{
			name:   "assert counts too",
			docs:   []config.Document{derivingDataSet(t, "sales", "assert", "pp3", "2 week")},
			locale: "en",
			want:   `{"global.pp3":"PP"}`,
		},
		{
			name:   "unit is matched, not the literal 1 year",
			docs:   []config.Document{derivingDataSet(t, "sales", "assert", "pp3", "2 year")},
			locale: "en",
		},
		{
			name: "only the non-year slot is relabelled",
			docs: []config.Document{
				derivingDataSet(t, "a", "derive", "pp1", "1 month"),
				derivingDataSet(t, "b", "derive", "pp2", "1 year"),
			},
			locale: "en",
			want:   `{"global.pp1":"PP"}`,
		},
		{
			name:   "locale is the artefact language",
			docs:   []config.Document{derivingDataSet(t, "sales", "derive", "pp1", "1 quarter")},
			locale: "de",
			want:   `{"global.pp1":"PP"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := collectInternationalizations(tc.docs, tc.locale)
			if err != nil {
				t.Fatalf("collectInternationalizations: %v", err)
			}
			if tc.want == "" {
				if len(entries) != 0 {
					t.Fatalf("expected no entries, got %+v", entries)
				}
				return
			}
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %+v", entries)
			}
			got := entries[0]
			if got.code != tc.locale || got.namespace != "_system" || got.value != tc.want {
				t.Fatalf("got %+v, want code=%q namespace=_system value=%s", got, tc.locale, tc.want)
			}
		})
	}
}

// The engine merges bundles for the same namespace and code in DOM order and
// the later element wins per key (bn-internationalization.tsx spreads the
// existing bundle first, the new payload second). The synthesized caption
// therefore has to come before every authored document.
func TestCollectInternationalizations_ProjectBundleWinsOverDerivedCaption(t *testing.T) {
	authored := makeTestDoc("Internationalization", "en", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Internationalization",
		"metadata": {"name": "en"},
		"spec": {"code": "en", "content": {"global.pp1": "Prev. month"}}
	}`))
	// The authored document is listed first on purpose: document order must
	// not let it slip in front of the synthesized one.
	docs := []config.Document{authored, derivingDataSet(t, "sales", "derive", "pp1", "1 month")}

	entries, err := collectInternationalizations(docs, "en")
	if err != nil {
		t.Fatalf("collectInternationalizations: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected synthesized + authored entry, got %+v", entries)
	}
	if entries[0].value != `{"global.pp1":"PP"}` {
		t.Fatalf("synthesized caption must come first, got %+v", entries)
	}
	if !strings.Contains(entries[1].value, "Prev. month") {
		t.Fatalf("authored bundle must come last, got %+v", entries)
	}

	segments := renderInternationalizations(entries)
	if len(segments) != 2 {
		t.Fatalf("expected 2 elements, got %v", segments)
	}
	for i, seg := range segments {
		if !strings.Contains(seg, `code='en'`) || !strings.Contains(seg, `namespace='_system'`) {
			t.Fatalf("element %d lacks code/namespace: %s", i, seg)
		}
	}
	if !strings.Contains(segments[0], "PP") || !strings.Contains(segments[1], "Prev. month") {
		t.Fatalf("emitted order must keep the synthesized element first: %v", segments)
	}
}
