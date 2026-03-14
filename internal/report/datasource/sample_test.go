package datasource

import (
	"encoding/json"
	"testing"
)

func TestSampleSpecUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSize   string
		wantMethod string
		wantErr    bool
	}{
		{
			name:     "number (row count)",
			input:    `1000`,
			wantSize: "1000",
		},
		{
			name:     "string (percentage)",
			input:    `"10%"`,
			wantSize: "10%",
		},
		{
			name:       "object with size and method",
			input:      `{"size": 500, "method": "reservoir"}`,
			wantSize:   "500",
			wantMethod: "reservoir",
		},
		{
			name:       "object with percentage and method",
			input:      `{"size": "20%", "method": "bernoulli"}`,
			wantSize:   "20%",
			wantMethod: "bernoulli",
		},
		{
			name:     "object with size only",
			input:    `{"size": 100}`,
			wantSize: "100",
		},
		{
			name:       "method is lowercased",
			input:      `{"size": "5%", "method": "System"}`,
			wantSize:   "5%",
			wantMethod: "system",
		},
		{
			name:    "null",
			input:   `null`,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   `true`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s sampleSpec
			err := json.Unmarshal([]byte(tt.input), &s)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if s.Size != tt.wantSize {
				t.Errorf("Size = %q, want %q", s.Size, tt.wantSize)
			}
			if s.Method != tt.wantMethod {
				t.Errorf("Method = %q, want %q", s.Method, tt.wantMethod)
			}
		})
	}
}

func TestBuildSampleClause(t *testing.T) {
	tests := []struct {
		name   string
		sample *sampleSpec
		want   string
	}{
		{
			name:   "nil sample",
			sample: nil,
			want:   "",
		},
		{
			name:   "empty size",
			sample: &sampleSpec{},
			want:   "",
		},
		{
			name:   "row count",
			sample: &sampleSpec{Size: "1000"},
			want:   " USING SAMPLE 1000",
		},
		{
			name:   "percentage",
			sample: &sampleSpec{Size: "10%"},
			want:   " USING SAMPLE 10%",
		},
		{
			name:   "row count with method",
			sample: &sampleSpec{Size: "500", Method: "reservoir"},
			want:   " USING SAMPLE 500 (reservoir)",
		},
		{
			name:   "percentage with method",
			sample: &sampleSpec{Size: "20%", Method: "bernoulli"},
			want:   " USING SAMPLE 20% (bernoulli)",
		},
		{
			name:   "system method",
			sample: &sampleSpec{Size: "5%", Method: "system"},
			want:   " USING SAMPLE 5% (system)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSampleClause(tt.sample)
			if got != tt.want {
				t.Errorf("buildSampleClause() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSpecWithSample(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSize   string
		wantMethod string
	}{
		{
			name:     "sample as number",
			raw:      `{"spec":{"type":"csv","path":"./data.csv","sample":1000}}`,
			wantSize: "1000",
		},
		{
			name:     "sample as percentage string",
			raw:      `{"spec":{"type":"csv","path":"./data.csv","sample":"10%"}}`,
			wantSize: "10%",
		},
		{
			name:       "sample as object",
			raw:        `{"spec":{"type":"csv","path":"./data.csv","sample":{"size":500,"method":"reservoir"}}}`,
			wantSize:   "500",
			wantMethod: "reservoir",
		},
		{
			name: "no sample",
			raw:  `{"spec":{"type":"csv","path":"./data.csv"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseSpec(json.RawMessage(tt.raw))
			if err != nil {
				t.Fatalf("parseSpec() error = %v", err)
			}
			if tt.wantSize == "" {
				if spec.Sample != nil {
					t.Errorf("expected nil Sample, got %+v", spec.Sample)
				}
				return
			}
			if spec.Sample == nil {
				t.Fatal("expected non-nil Sample")
			}
			if spec.Sample.Size != tt.wantSize {
				t.Errorf("Sample.Size = %q, want %q", spec.Sample.Size, tt.wantSize)
			}
			if spec.Sample.Method != tt.wantMethod {
				t.Errorf("Sample.Method = %q, want %q", spec.Sample.Method, tt.wantMethod)
			}
		})
	}
}
