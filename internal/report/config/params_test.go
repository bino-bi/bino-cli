package config

import (
	"encoding/json"
	"testing"
)

func TestExpandDocParams(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		declared   []LayoutPageParamSpec
		refParams  map[string]string
		env        map[string]string
		want       string
		wantParams map[string]string
	}{
		{
			name:       "explicit param substituted",
			raw:        `{"spec":{"value":"Region: ${REGION}"}}`,
			declared:   []LayoutPageParamSpec{{Name: "REGION"}},
			refParams:  map[string]string{"REGION": "EU"},
			want:       `{"spec":{"value":"Region: EU"}}`,
			wantParams: map[string]string{"REGION": "EU"},
		},
		{
			name:       "explicit param beats default and env",
			raw:        `{"value":"${REGION}"}`,
			declared:   []LayoutPageParamSpec{{Name: "REGION", Default: new("US")}},
			refParams:  map[string]string{"REGION": "EU"},
			env:        map[string]string{"REGION": "APAC"},
			want:       `{"value":"EU"}`,
			wantParams: map[string]string{"REGION": "EU"},
		},
		{
			name:       "default applied when param not provided",
			raw:        `{"value":"${REGION}"}`,
			declared:   []LayoutPageParamSpec{{Name: "REGION", Default: new("US")}},
			want:       `{"value":"US"}`,
			wantParams: map[string]string{"REGION": "US"},
		},
		{
			name:       "env fallback for declared param without default",
			raw:        `{"value":"${REGION}"}`,
			declared:   []LayoutPageParamSpec{{Name: "REGION"}},
			env:        map[string]string{"REGION": "APAC"},
			want:       `{"value":"APAC"}`,
			wantParams: map[string]string{"REGION": "APAC"},
		},
		{
			name:      "undeclared var falls back to env",
			raw:       `{"value":"${REGION} ${OTHER}"}`,
			declared:  []LayoutPageParamSpec{{Name: "REGION"}},
			refParams: map[string]string{"REGION": "EU"},
			env:       map[string]string{"OTHER": "x"},
			want:      `{"value":"EU x"}`,
		},
		{
			name: "select param exposes label variant",
			raw:  `{"value":"${REGION} - ${REGION_LABEL}"}`,
			declared: []LayoutPageParamSpec{{
				Name: "REGION",
				Type: "select",
				Options: &LayoutPageParamOptions{Items: []LayoutPageParamOptionItem{
					{Value: "EU", Label: "Europe"},
					{Value: "US", Label: "North America"},
				}},
			}},
			refParams:  map[string]string{"REGION": "EU"},
			want:       `{"value":"EU - Europe"}`,
			wantParams: map[string]string{"REGION": "EU", "REGION_LABEL": "Europe"},
		},
		{
			name: "select default exposes label variant",
			raw:  `{"value":"${REGION_LABEL}"}`,
			declared: []LayoutPageParamSpec{{
				Name:    "REGION",
				Type:    "select",
				Default: new("US"),
				Options: &LayoutPageParamOptions{Items: []LayoutPageParamOptionItem{
					{Value: "US", Label: "North America"},
				}},
			}},
			want: `{"value":"North America"}`,
		},
		{
			name:      "ref param values expand env references",
			raw:       `{"value":"${REGION}"}`,
			declared:  []LayoutPageParamSpec{{Name: "REGION"}},
			refParams: map[string]string{"REGION": "${DEFAULT_REGION}"},
			env:       map[string]string{"DEFAULT_REGION": "EU"},
			want:      `{"value":"EU"}`,
		},
		{
			name:      "escaped reference preserved as literal",
			raw:       `{"value":"\${REGION}"}`,
			declared:  []LayoutPageParamSpec{{Name: "REGION"}},
			refParams: map[string]string{"REGION": "EU"},
			want:      `{"value":"${REGION}"}`,
		},
		{
			name:      "inline default used for missing var",
			raw:       `{"value":"${MISSING:fallback}"}`,
			declared:  []LayoutPageParamSpec{{Name: "REGION"}},
			refParams: map[string]string{"REGION": "EU"},
			want:      `{"value":"fallback"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, effective := ExpandDocParams(json.RawMessage(tt.raw), tt.declared, tt.refParams)
			if string(got) != tt.want {
				t.Errorf("expanded content = %s, want %s", got, tt.want)
			}
			if tt.wantParams != nil {
				if len(effective) != len(tt.wantParams) {
					t.Errorf("effective params = %v, want %v", effective, tt.wantParams)
				}
				for k, v := range tt.wantParams {
					if effective[k] != v {
						t.Errorf("effective[%q] = %q, want %q", k, effective[k], v)
					}
				}
			}
		})
	}
}
