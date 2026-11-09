package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestOperationSentiment_EngineGate(t *testing.T) {
	docs := []config.Document{pageDoc(`{"titleVariances": "dac1_pl1_op", "children": [{"kind": "Text", "spec": {"value": "x"}}]}`)}
	_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.33")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.34") || !strings.Contains(err.Error(), "operation-sentiment-engine-version") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.34"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	plain := makeTestDoc("LayoutPage", "page", json.RawMessage(`{"kind":"LayoutPage","metadata":{"name":"page"},"spec":{"titleVariances":"dac1_pl1_pos","children":[]}}`))
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{plain}, "en", "", "", ModeBuild, "v1.0.0-next.33"); err != nil {
		t.Fatalf("no _op: %v", err)
	}
}
