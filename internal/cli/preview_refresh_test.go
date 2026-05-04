package cli

import (
	"slices"
	"testing"
)

func TestMergeRefreshRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       []refreshRequest
		wantReason  string
		wantFiles   []string
		wantNilFile bool
	}{
		{
			name:        "empty input",
			input:       nil,
			wantReason:  "unknown",
			wantNilFile: true,
		},
		{
			name: "single incremental request",
			input: []refreshRequest{
				{reason: "change /a", files: []string{"/abs/a"}},
			},
			wantReason: "change /a",
			wantFiles:  []string{"/abs/a"},
		},
		{
			name: "multiple incremental requests dedupe and concat files",
			input: []refreshRequest{
				{reason: "change /a", files: []string{"/abs/a"}},
				{reason: "change /b", files: []string{"/abs/b"}},
				{reason: "change /a", files: []string{"/abs/a"}},
			},
			wantReason: "change /a (+2 more)",
			wantFiles:  []string{"/abs/a", "/abs/b"},
		},
		{
			name: "any nil files entry forces full rebuild",
			input: []refreshRequest{
				{reason: "change /a", files: []string{"/abs/a"}},
				{reason: "manual reload", files: nil},
				{reason: "change /b", files: []string{"/abs/b"}},
			},
			wantReason:  "change /a (+2 more)",
			wantNilFile: true,
		},
		{
			name: "all-nil input stays nil",
			input: []refreshRequest{
				{reason: "manual reload", files: nil},
				{reason: "manual reload", files: nil},
			},
			wantReason:  "manual reload (+1 more)",
			wantNilFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReason, gotFiles := mergeRefreshRequests(tt.input)
			if gotReason != tt.wantReason {
				t.Errorf("reason = %q, want %q", gotReason, tt.wantReason)
			}
			if tt.wantNilFile {
				if gotFiles != nil {
					t.Errorf("files = %v, want nil (full rebuild signal)", gotFiles)
				}
				return
			}
			slices.Sort(gotFiles)
			want := append([]string(nil), tt.wantFiles...)
			slices.Sort(want)
			if !slices.Equal(gotFiles, want) {
				t.Errorf("files = %v, want %v", gotFiles, want)
			}
		})
	}
}
