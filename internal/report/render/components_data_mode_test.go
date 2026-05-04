package render

import (
	"strings"
	"testing"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
)

func TestRenderDatasetsURLMode(t *testing.T) {
	t.Parallel()
	body := []byte(`[{"a":1}]`)
	results := []dataset.Result{{Name: "sales", Data: body}}

	segs, emitted := renderDatasets(results, DataModeURL, "")

	if len(segs) != 1 {
		t.Fatalf("segments len = %d, want 1", len(segs))
	}
	wantHash := ContentHash(body)
	wantURL := "/__bino/data/dataset/sales?hash=" + wantHash
	if !strings.Contains(segs[0], ">"+wantURL+"<") {
		t.Fatalf("segment missing URL body %q\n  got %q", wantURL, segs[0])
	}
	if strings.Contains(segs[0], `raw='false'`) {
		t.Fatalf("url mode should NOT set raw='false'; got %q", segs[0])
	}
	if !strings.Contains(segs[0], `static='true'`) {
		t.Fatalf("dataset must keep static='true' in url mode; got %q", segs[0])
	}
	if !strings.Contains(segs[0], `name='sales'`) {
		t.Fatalf("dataset name attribute missing; got %q", segs[0])
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted len = %d, want 1", len(emitted))
	}
	if emitted[0].Kind != EmittedKindDataset {
		t.Errorf("emitted Kind = %q, want %q", emitted[0].Kind, EmittedKindDataset)
	}
	if emitted[0].Name != "sales" || emitted[0].Hash != wantHash || string(emitted[0].Body) != string(body) {
		t.Errorf("emitted = %+v, want name=sales hash=%s body=%s", emitted[0], wantHash, body)
	}
}

func TestRenderDatasourcesURLModeAbsolute(t *testing.T) {
	t.Parallel()
	body := []byte(`[1,2,3]`)
	results := []datasource.Result{{Name: "raw events", Data: body}}

	segs, emitted := renderDatasources(results, DataModeURL, "http://127.0.0.1:45678")

	if len(segs) != 1 {
		t.Fatalf("segments len = %d, want 1", len(segs))
	}
	wantHash := ContentHash(body)
	// Name "raw events" must be path-escaped.
	wantURL := "http://127.0.0.1:45678/__bino/data/datasource/raw%20events?hash=" + wantHash
	if !strings.Contains(segs[0], ">"+wantURL+"<") {
		t.Fatalf("segment missing absolute URL body %q\n  got %q", wantURL, segs[0])
	}
	if len(emitted) != 1 || emitted[0].Kind != EmittedKindDatasource {
		t.Fatalf("emitted = %+v, want one datasource entry", emitted)
	}
}

func TestRenderDatasetsInlineModeUnchanged(t *testing.T) {
	t.Parallel()
	body := []byte(`[{"a":1}]`)
	results := []dataset.Result{{Name: "sales", Data: body}}

	segs, emitted := renderDatasets(results, "", "")

	if len(emitted) != 0 {
		t.Fatalf("emitted len = %d, want 0 in inline mode", len(emitted))
	}
	if len(segs) != 1 {
		t.Fatalf("segments len = %d, want 1", len(segs))
	}
	if !strings.Contains(segs[0], `raw='false'`) {
		t.Fatalf("inline mode should set raw='false'; got %q", segs[0])
	}
	if !strings.Contains(segs[0], ContentHash(body)+":") {
		t.Fatalf("inline body missing FNV hash prefix; got %q", segs[0])
	}
}

func TestRenderDatasetsDedupesByName(t *testing.T) {
	t.Parallel()
	// Same name appearing twice (e.g. constraint-gated variants the caller
	// didn't filter). Renderer must collapse to a single element.
	first := []byte(`[{"v":1}]`)
	second := []byte(`[{"v":2}]`)
	results := []dataset.Result{
		{Name: "x", Data: first},
		{Name: "y", Data: []byte(`[]`)},
		{Name: "x", Data: second}, // last wins
	}

	segs, emitted := renderDatasets(results, DataModeURL, "")

	if len(segs) != 2 {
		t.Fatalf("segments len = %d, want 2 (deduped)", len(segs))
	}
	if len(emitted) != 2 {
		t.Fatalf("emitted len = %d, want 2", len(emitted))
	}
	// First emitted should be x with second's hash (last-wins), then y.
	if emitted[0].Name != "x" || emitted[0].Hash != ContentHash(second) {
		t.Errorf("emitted[0] = %+v, want x at second's hash", emitted[0])
	}
	if emitted[1].Name != "y" {
		t.Errorf("emitted[1] = %+v, want y", emitted[1])
	}
}

func TestRenderDatasourcesDedupesByName(t *testing.T) {
	t.Parallel()
	results := []datasource.Result{
		{Name: "a", Data: []byte(`[]`)},
		{Name: "a", Data: []byte(`[1]`)},
	}
	segs, emitted := renderDatasources(results, DataModeURL, "")
	if len(segs) != 1 || len(emitted) != 1 {
		t.Fatalf("dedup failed: segs=%d emitted=%d", len(segs), len(emitted))
	}
}

func TestContentHashStable(t *testing.T) {
	t.Parallel()
	a := ContentHash([]byte("payload"))
	b := ContentHash([]byte("payload"))
	if a != b {
		t.Fatalf("ContentHash not deterministic: %s vs %s", a, b)
	}
	if ContentHash([]byte("payload")) == ContentHash([]byte("Payload")) {
		t.Fatalf("ContentHash collision on differing inputs")
	}
}
