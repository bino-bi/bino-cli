package dataset

import (
	"reflect"
	"testing"
)

func TestRewriteInlineRefs(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		inlineNames []string
		want        string
		wantErr     bool
	}{
		{
			name:        "no inline refs",
			query:       "SELECT * FROM sales",
			inlineNames: nil,
			want:        "SELECT * FROM sales",
			wantErr:     false,
		},
		{
			name:        "single inline ref",
			query:       "SELECT * FROM @inline(0)",
			inlineNames: []string{"_inline_datasource_abc123"},
			want:        `SELECT * FROM "_inline_datasource_abc123"`,
			wantErr:     false,
		},
		{
			name:        "multiple inline refs same index",
			query:       "SELECT * FROM @inline(0) WHERE id IN (SELECT id FROM @inline(0))",
			inlineNames: []string{"_inline_ds_1"},
			want:        `SELECT * FROM "_inline_ds_1" WHERE id IN (SELECT id FROM "_inline_ds_1")`,
			wantErr:     false,
		},
		{
			name:        "multiple inline refs different indices",
			query:       "SELECT a.*, b.name FROM @inline(0) a JOIN @inline(1) b ON a.id = b.id",
			inlineNames: []string{"_inline_ds_a", "_inline_ds_b"},
			want:        `SELECT a.*, b.name FROM "_inline_ds_a" a JOIN "_inline_ds_b" b ON a.id = b.id`,
			wantErr:     false,
		},
		{
			name:        "inline ref with WHERE clause",
			query:       "SELECT * FROM @inline(0) WHERE region = 'US'",
			inlineNames: []string{"_inline_csv_data"},
			want:        `SELECT * FROM "_inline_csv_data" WHERE region = 'US'`,
			wantErr:     false,
		},
		{
			name:        "index out of bounds",
			query:       "SELECT * FROM @inline(5)",
			inlineNames: []string{"_inline_ds_0", "_inline_ds_1"},
			want:        "",
			wantErr:     true,
		},
		{
			name:        "inline ref without inline names",
			query:       "SELECT * FROM @inline(0)",
			inlineNames: nil,
			want:        "",
			wantErr:     true,
		},
		{
			name:        "mixed named and inline refs",
			query:       "SELECT * FROM named_table JOIN @inline(0) ON named_table.id = @inline(0).id",
			inlineNames: []string{"_inline_ds_join"},
			want:        `SELECT * FROM named_table JOIN "_inline_ds_join" ON named_table.id = "_inline_ds_join".id`,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RewriteInlineRefs(tt.query, tt.inlineNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("RewriteInlineRefs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("RewriteInlineRefs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateInlineRefs(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		inlineCount int
		wantErrs    int
	}{
		{
			name:        "no refs, count 0",
			query:       "SELECT * FROM sales",
			inlineCount: 0,
			wantErrs:    0,
		},
		{
			name:        "valid ref",
			query:       "SELECT * FROM @inline(0)",
			inlineCount: 1,
			wantErrs:    0,
		},
		{
			name:        "valid multiple refs",
			query:       "SELECT * FROM @inline(0) JOIN @inline(1)",
			inlineCount: 2,
			wantErrs:    0,
		},
		{
			name:        "out of bounds",
			query:       "SELECT * FROM @inline(2)",
			inlineCount: 2,
			wantErrs:    1,
		},
		{
			name:        "multiple out of bounds",
			query:       "SELECT * FROM @inline(5) JOIN @inline(10)",
			inlineCount: 2,
			wantErrs:    2,
		},
		{
			name:        "ref with no inline deps",
			query:       "SELECT * FROM @inline(0)",
			inlineCount: 0,
			wantErrs:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateInlineRefs(tt.query, tt.inlineCount)
			if len(errs) != tt.wantErrs {
				t.Errorf("ValidateInlineRefs() returned %d errors, want %d; errors: %v", len(errs), tt.wantErrs, errs)
			}
		})
	}
}

func TestHasInlineRefs(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"SELECT * FROM sales", false},
		{"SELECT * FROM @inline(0)", true},
		{"SELECT * FROM @inline(0) WHERE @inline(1).id = 1", true},
		{"SELECT '@inline(0)' FROM sales", true}, // String literal still matches - this is expected
		{"SELECT * FROM inline0", false},
		{"SELECT * FROM _inline_ds_123", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := HasInlineRefs(tt.query); got != tt.want {
				t.Errorf("HasInlineRefs(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestExtractInlineIndices(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []int
	}{
		{
			name:  "no refs",
			query: "SELECT * FROM sales",
			want:  nil,
		},
		{
			name:  "single ref",
			query: "SELECT * FROM @inline(0)",
			want:  []int{0},
		},
		{
			name:  "multiple refs same index",
			query: "SELECT * FROM @inline(0) WHERE id IN (SELECT id FROM @inline(0))",
			want:  []int{0},
		},
		{
			name:  "multiple refs different indices",
			query: "SELECT * FROM @inline(1) JOIN @inline(0) ON @inline(2).id = 1",
			want:  []int{0, 1, 2},
		},
		{
			name:  "out of order indices",
			query: "SELECT * FROM @inline(5) JOIN @inline(2) JOIN @inline(8)",
			want:  []int{2, 5, 8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractInlineIndices(tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractInlineIndices() = %v, want %v", got, tt.want)
			}
		})
	}
}
