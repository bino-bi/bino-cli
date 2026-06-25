package dataset

import (
	"encoding/json"
	"strings"
	"testing"
)

// inlineWrap compiles a spec in inline mode (the preview path) via a fresh
// inline emitter, mirroring buildWrappedQuery but without bound parameters. It
// is the in-package equivalent of WrapQueryForPreview for tests that build a
// dataSetSpec directly rather than via raw JSON.
func inlineWrap(t *testing.T, spec dataSetSpec) string {
	t.Helper()
	if _, err := ValidateDataSetSpec(spec); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	got, err := wrapQuery(spec, baseQ, &valueEmitter{inline: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

// TestParamAndInlineModesAreEquivalent proves the build path (bound parameters)
// and the preview path (inline literals) compile the *same* logical query for a
// given spec: substituting each bound arg's SQL literal for its "?" placeholder,
// in textual order, reproduces the inline SQL byte-for-byte. Since the only
// difference between the two emitters is value representation (not structure),
// the two paths necessarily return the same rows for the same dataset.
func TestParamAndInlineModesAreEquivalent(t *testing.T) {
	specs := []struct {
		name string
		spec dataSetSpec
	}{
		{
			name: "filter only",
			spec: dataSetSpec{Filter: &filterGroup{Op: "and", Conditions: []filterNode{
				leaf("region", "in", []any{"EMEA", "APAC"}),
				leaf("sales", "gte", 1000.0),
				leaf("product", "regex", "^A"),
			}}},
		},
		{
			name: "full filter+groupBy+index",
			spec: dataSetSpec{
				Filter: &filterGroup{Op: "and", Conditions: []filterNode{
					leaf("region", "in", []any{"EMEA", "APAC"}),
					leaf("sales", "gte", 1000.0),
				}},
				GroupBy: &groupBy{
					Columns:    []string{"region"},
					Aggregates: []aggregate{{Column: "sales", Fn: "sum", As: "total"}},
				},
				IndexColumns: []indexColumn{
					{Column: "categoryIndex", Fn: "denseRank", Over: "region"},
					{Column: "rowGroupIndex", Fn: "hash", Of: "region"},
				},
			},
		},
	}

	for _, tc := range specs {
		t.Run(tc.name, func(t *testing.T) {
			paramEmitter := &valueEmitter{inline: false}
			paramSQL, err := wrapQuery(tc.spec, baseQ, paramEmitter)
			if err != nil {
				t.Fatalf("param-mode wrap failed: %v", err)
			}
			inlineSQL, err := wrapQuery(tc.spec, baseQ, &valueEmitter{inline: true})
			if err != nil {
				t.Fatalf("inline-mode wrap failed: %v", err)
			}

			// Substitute each bound arg's literal for the next "?" in textual order.
			substituted := paramSQL
			for _, arg := range paramEmitter.args {
				lit, err := sqlLiteral(arg)
				if err != nil {
					t.Fatalf("sqlLiteral(%#v): %v", arg, err)
				}
				idx := strings.Index(substituted, "?")
				if idx < 0 {
					t.Fatalf("more args than '?' placeholders in %q", paramSQL)
				}
				substituted = substituted[:idx] + lit + substituted[idx+1:]
			}
			if strings.Contains(substituted, "?") {
				t.Fatalf("unfilled '?' placeholder remained: %q", substituted)
			}
			if substituted != inlineSQL {
				t.Errorf("param-with-literals != inline SQL\n param: %s\ninline: %s", substituted, inlineSQL)
			}
		})
	}
}

func TestWrapQuery_InlineMode_GoldenSQL(t *testing.T) {
	tests := []struct {
		name string
		spec dataSetSpec
		want string
	}{
		{
			name: "filter string value is single-quoted and escaped",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "equal", "O'Brien")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" = 'O''Brien')`,
		},
		{
			name: "filter integer-valued number inline",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("sales", "gte", 1000.0)}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."sales" >= 1000)`,
		},
		{
			name: "filter fractional number inline",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("sales", "lt", 12.5)}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."sales" < 12.5)`,
		},
		{
			name: "filter bool inline",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("active", "equal", true)}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."active" = TRUE)`,
		},
		{
			name: "filter in-list inlined",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "in", []any{"EMEA", "APAC"})}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" IN ('EMEA', 'APAC'))`,
		},
		{
			name: "filter regex inlined",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("product", "regex", "^A")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (regexp_matches(_bn_base."product", '^A'))`,
		},
		{
			name: "full filter+groupBy+index stack inline",
			spec: dataSetSpec{
				Filter: &filterGroup{Op: "and", Conditions: []filterNode{
					leaf("region", "in", []any{"EMEA", "APAC"}),
					leaf("sales", "gte", 1000.0),
				}},
				GroupBy: &groupBy{
					Columns: []string{"region"},
					Aggregates: []aggregate{
						{Column: "sales", Fn: "sum", As: "total"},
						{Column: "sku", Fn: "countDistinct", As: "skus"},
					},
				},
				IndexColumns: []indexColumn{
					{Column: "categoryIndex", Fn: "denseRank", Over: "region"},
					{Column: "rowGroupIndex", Fn: "hash", Of: "region"},
				},
			},
			want: `SELECT _bn_grouped.*, ` +
				`dense_rank() OVER (ORDER BY _bn_grouped."region") AS "categoryIndex", ` +
				`hash(_bn_grouped."region") AS "rowGroupIndex" ` +
				`FROM (` +
				`SELECT _bn_base."region", sum(_bn_base."sales") AS "total", count(DISTINCT _bn_base."sku") AS "skus" ` +
				`FROM (` +
				`SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base ` +
				`WHERE (_bn_base."region" IN ('EMEA', 'APAC') AND _bn_base."sales" >= 1000)` +
				`) AS _bn_base ` +
				`GROUP BY _bn_base."region"` +
				`) AS _bn_grouped`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inlineWrap(t, tt.spec)
			if norm(got) != norm(tt.want) {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", norm(got), norm(tt.want))
			}
		})
	}
}

func TestSQLLiteral(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    string
		wantErr bool
	}{
		{name: "string", in: "x", want: "'x'"},
		{name: "string with quote", in: "a'b", want: "'a''b'"},
		{name: "bool true", in: true, want: "TRUE"},
		{name: "bool false", in: false, want: "FALSE"},
		{name: "int", in: 7, want: "7"},
		{name: "int64", in: int64(9), want: "9"},
		{name: "integer-valued float", in: 1000.0, want: "1000"},
		{name: "fractional float", in: 12.5, want: "12.5"},
		{name: "json.Number", in: json.Number("42"), want: "42"},
		{name: "nil rejected", in: nil, wantErr: true},
		{name: "unsupported type rejected", in: []any{"x"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlLiteral(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// rawSpec wraps a spec body in the document envelope parseDataSetSpec expects.
func rawSpec(t *testing.T, specBody string) json.RawMessage {
	t.Helper()
	r := json.RawMessage(`{"spec":` + specBody + `}`)
	if !json.Valid(r) {
		t.Fatalf("invalid test JSON: %s", r)
	}
	return r
}

func TestWrapQueryForPreview_Identity(t *testing.T) {
	raw := rawSpec(t, `{"query":"SELECT 1"}`)
	got, err := WrapQueryForPreview(raw, baseQ, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != baseQ {
		t.Errorf("identity not preserved:\n got: %q\nwant: %q", got, baseQ)
	}
}

func TestWrapQueryForPreview_Inline(t *testing.T) {
	raw := rawSpec(t, `{"query":"SELECT 1","filter":{"conditions":[{"column":"region","op":"equal","value":"O'Brien"}]}}`)
	got, err := WrapQueryForPreview(raw, baseQ, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" = 'O''Brien')`
	if norm(got) != norm(want) {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", norm(got), norm(want))
	}
}

func TestWrapQueryForPreview_PRQLWithTransformErrors(t *testing.T) {
	raw := rawSpec(t, `{"prql":"from raw","filter":{"conditions":[{"column":"region","op":"equal","value":"EMEA"}]}}`)
	_, err := WrapQueryForPreview(raw, baseQ, true)
	if err == nil {
		t.Fatal("expected an error for prql + transform, got nil")
	}
	if !strings.Contains(err.Error(), "not supported with prql") {
		t.Errorf("error %q does not mention prql", err.Error())
	}
}

func TestValidateSpec_HappyPathNoIssues(t *testing.T) {
	raw := rawSpec(t, `{"query":"SELECT region, sales FROM raw"}`)
	warns, err := ValidateSpec(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings, got %#v", warns)
	}
}

func TestValidateSpec_OpValueMismatchErrors(t *testing.T) {
	raw := rawSpec(t, `{"query":"SELECT 1","filter":{"conditions":[{"column":"region","op":"in","value":"EMEA"}]}}`)
	_, err := ValidateSpec(raw)
	if err == nil {
		t.Fatal("expected an op/value-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "requires an array value") {
		t.Errorf("error %q does not describe the array mismatch", err.Error())
	}
}

func TestValidateSpec_FirstWithoutOrderByWarns(t *testing.T) {
	raw := rawSpec(t, `{"query":"SELECT 1","groupBy":{"columns":["region"],"aggregates":[{"column":"sales","fn":"first","as":"f"}]}}`)
	warns, err := ValidateSpec(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "nondeterministic") {
		t.Errorf("expected a nondeterminism warning, got %#v", warns)
	}
}
