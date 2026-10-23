package chrome

import (
	"strings"
	"testing"
)

func TestPluginSettleError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string // substrings of the error; nil means no error
	}{
		{name: "null", raw: `null`},
		{name: "empty", raw: ``},
		{name: "empty lists", raw: `{"failures":[],"undefined":[]}`},
		{
			name: "one failure",
			raw:  `{"failures":[{"tag":"highcharts-chart","id":"rev","message":"no data"}],"undefined":[]}`,
			want: []string{"plugin component highcharts-chart#rev failed to render: no data"},
		},
		{
			name: "failure without id",
			raw:  `{"failures":[{"tag":"highcharts-chart","id":"","message":"no data"}]}`,
			want: []string{"plugin component highcharts-chart failed to render: no data"},
		},
		{
			name: "script error",
			raw:  `{"failures":[{"tag":"","id":"","message":"boom (/plugins/hc/chart.js)"}]}`,
			want: []string{"plugin script error: boom (/plugins/hc/chart.js)"},
		},
		{
			name: "undefined",
			raw:  `{"failures":[],"undefined":["highcharts-chart"]}`,
			want: []string{"custom element highcharts-chart was never defined"},
		},
		{
			name: "several",
			raw:  `{"failures":[{"tag":"a-b","id":"x","message":"m"}],"undefined":["c-d"]}`,
			want: []string{"a-b#x failed to render: m", "c-d was never defined"},
		},
		{name: "malformed", raw: `{"failures":`, want: []string{"read plugin component state"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pluginSettleError([]byte(tt.raw))
			if tt.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %q, want it to contain %q", err, w)
				}
			}
		})
	}
}
