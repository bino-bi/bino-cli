package render

import (
	"strings"
	"testing"
)

func TestSmallMultiple_SpanEngineGate(t *testing.T) {
	for _, smSpec := range []string{
		`{` + spanHead + `, "panels": {"by": "rowGroup", "overrides": [{"value": "Berlin", "rowSpan": 2}]}}`,
		`{` + spanHead + `, "axisTitles": "rowEnd", "panels": {"by": "rowGroup"}}`,
	} {
		_, _, err := renderSmallMultipleWith(smSpec, "v1.0.0-next.45")
		if err == nil || !strings.Contains(err.Error(), "small-multiple-span-engine-version") || !strings.Contains(err.Error(), "v1.0.0-next.46") {
			t.Errorf("%s: err = %v", smSpec, err)
		}
	}
	if _, _, err := renderSmallMultipleWith(`{`+spanHead+`, "panels": {"by": "rowGroup"}}`, "v1.0.0-next.45"); err != nil {
		t.Errorf("a SmallMultiple without the new fields must render on the older engine: %v", err)
	}
}
