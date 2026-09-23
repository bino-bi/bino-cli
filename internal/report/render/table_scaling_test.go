package render

import (
	"strings"
	"testing"
)

func TestTableScaling_RenderedAttributes(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{"number", `{"dataset": "test", "unitScaling": 50, "percentageScaling": 1}`, `unit-scaling='50' percentage-scaling='1'`},
		{"auto", `{"dataset": "test", "unitScaling": "auto", "percentageScaling": "auto"}`, `unit-scaling='auto' percentage-scaling='auto'`},
		{"scaling group", `{"dataset": "test", "unitScaling": "sg_units", "percentageScaling": "sg_pct"}`, `unit-scaling='sg_units' percentage-scaling='sg_pct'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := renderTablePage(t, tt.spec)
			if !strings.Contains(html, tt.want) {
				t.Fatalf("expected %s in HTML, got:\n%s", tt.want, html)
			}
		})
	}
}

func TestTableScaling_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test"}`)

	for _, attr := range []string{"unit-scaling=", "percentage-scaling="} {
		if strings.Contains(html, attr) {
			t.Errorf("expected no %s attribute in HTML, got:\n%s", attr, html)
		}
	}
}
