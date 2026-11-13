package render

import (
	"context"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestLayoutPage_TitleID(t *testing.T) {
	render := func(spec string) string {
		res, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{pageDoc(spec)}, "en", "", "", ModeBuild, "v1.0.0-next.46")
		if err != nil {
			t.Fatal(err)
		}
		return openTag(t, string(res.HTML), "bn-layout-page")
	}
	if got := attrsOf(render(`{"titleId": "13A", "children": []}`))["title-id"]; got != "13A" {
		t.Errorf("title-id = %q", got)
	}
	if tag := render(`{"children": []}`); strings.Contains(tag, "title-id") {
		t.Errorf("unexpected title-id in %s", tag)
	}
}
