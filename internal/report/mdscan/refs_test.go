package mdscan

import (
	"reflect"
	"testing"
)

func TestScanRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []Ref
	}{
		{
			name:    "plain ref",
			content: "Intro\n\n:ref[ChartTime:sales_chart]\n",
			want:    []Ref{{Kind: "ChartTime", Name: "sales_chart"}},
		},
		{
			name:    "ref with caption",
			content: `:ref[Table:overview]{caption="Regions"}`,
			want:    []Ref{{Kind: "Table", Name: "overview"}},
		},
		{
			name:    "multiple refs on one line",
			content: ":ref[Text:a] and :ref[Grid:b]",
			want:    []Ref{{Kind: "Text", Name: "a"}, {Kind: "Grid", Name: "b"}},
		},
		{
			name:    "duplicates collapse in first-occurrence order",
			content: ":ref[Table:x]\n:ref[Text:y]\n:ref[Table:x]\n",
			want:    []Ref{{Kind: "Table", Name: "x"}, {Kind: "Text", Name: "y"}},
		},
		{
			// Documented over-approximation: the plain text scan also matches
			// inside fenced code blocks. A false positive only widens the
			// dependency set, which is safe for refresh and embedding scope.
			name:    "fenced code block still matches",
			content: "```md\n:ref[Table:example]\n```\n",
			want:    []Ref{{Kind: "Table", Name: "example"}},
		},
		{
			name:    "invalid kind charset does not match",
			content: ":ref[Chart Time:x] :ref[Table:na me]",
			want:    nil,
		},
		{
			name:    "no refs",
			content: "# Heading\n\nPlain prose only.\n",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ScanRefs([]byte(tt.content))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanRefs() = %v, want %v", got, tt.want)
			}
		})
	}
}
