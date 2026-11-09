package render

import (
	"context"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestTableOrderMeasure_EngineGate(t *testing.T) {
	docs := []config.Document{pageDoc(`{"titleOrder": "dac1_pp1_pos@_November", "children": [{"kind": "Text", "spec": {"value": "x"}}]}`)}
	_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.34")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.35") || !strings.Contains(err.Error(), "table-order-engine-version") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.35"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	plain := []config.Document{pageDoc(`{"titleOrder": "ac1", "children": [{"kind": "Text", "spec": {"value": "x"}}]}`)}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), plain, "en", "", "", ModeBuild, "v1.0.0-next.34"); err != nil {
		t.Fatalf("no variance order: %v", err)
	}
}
