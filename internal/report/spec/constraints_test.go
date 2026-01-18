package spec

import (
	"testing"

	"bino.bi/bino/internal/schema"
)

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    *Constraint
		wantErr bool
	}{
		{
			name: "mode equals",
			expr: "mode==preview",
			want: &Constraint{Raw: "mode==preview", Left: "mode", Operator: "==", Right: "preview"},
		},
		{
			name: "mode not equals",
			expr: "mode!=build",
			want: &Constraint{Raw: "mode!=build", Left: "mode", Operator: "!=", Right: "build"},
		},
		{
			name: "labels equals",
			expr: "labels.env==prod",
			want: &Constraint{Raw: "labels.env==prod", Left: "labels.env", Operator: "==", Right: "prod"},
		},
		{
			name: "labels not equals",
			expr: "labels.preview!=false",
			want: &Constraint{Raw: "labels.preview!=false", Left: "labels.preview", Operator: "!=", Right: "false"},
		},
		{
			name: "spec equals",
			expr: "spec.format==a4",
			want: &Constraint{Raw: "spec.format==a4", Left: "spec.format", Operator: "==", Right: "a4"},
		},
		{
			name: "spec nested path",
			expr: "spec.output.format==pdf",
			want: &Constraint{Raw: "spec.output.format==pdf", Left: "spec.output.format", Operator: "==", Right: "pdf"},
		},
		{
			name: "with spaces around operator",
			expr: "mode == preview",
			want: &Constraint{Raw: "mode == preview", Left: "mode", Operator: "==", Right: "preview"},
		},
		{
			name: "with leading/trailing spaces",
			expr: "  labels.env == prod  ",
			want: &Constraint{Raw: "labels.env == prod", Left: "labels.env", Operator: "==", Right: "prod"},
		},
		{
			name:    "empty expression",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "missing operator",
			expr:    "mode preview",
			wantErr: true,
		},
		{
			name:    "missing left operand",
			expr:    "==preview",
			wantErr: true,
		},
		{
			name:    "missing right operand",
			expr:    "mode==",
			wantErr: true,
		},
		{
			name:    "unknown root",
			expr:    "foo.bar==baz",
			wantErr: true,
		},
		{
			name:    "spec without path",
			expr:    "spec==value",
			wantErr: true,
		},
		{
			name:    "labels without key",
			expr:    "labels==value",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConstraint(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseConstraint(%q) expected error, got nil", tt.expr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseConstraint(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if got.Left != tt.want.Left {
				t.Errorf("Left = %q, want %q", got.Left, tt.want.Left)
			}
			if got.Operator != tt.want.Operator {
				t.Errorf("Operator = %q, want %q", got.Operator, tt.want.Operator)
			}
			if got.Right != tt.want.Right {
				t.Errorf("Right = %q, want %q", got.Right, tt.want.Right)
			}
		})
	}
}

func TestConstraintEvaluate(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		ctx     *ConstraintContext
		want    bool
		wantErr bool
	}{
		{
			name: "mode equals match",
			expr: "mode==preview",
			ctx:  &ConstraintContext{Mode: ModePreview},
			want: true,
		},
		{
			name: "mode equals no match",
			expr: "mode==preview",
			ctx:  &ConstraintContext{Mode: ModeBuild},
			want: false,
		},
		{
			name: "mode not equals match",
			expr: "mode!=build",
			ctx:  &ConstraintContext{Mode: ModePreview},
			want: true,
		},
		{
			name: "mode not equals no match",
			expr: "mode!=preview",
			ctx:  &ConstraintContext{Mode: ModePreview},
			want: false,
		},
		{
			name: "labels equals match",
			expr: "labels.env==prod",
			ctx:  &ConstraintContext{Labels: map[string]string{"env": "prod"}},
			want: true,
		},
		{
			name: "labels equals no match",
			expr: "labels.env==prod",
			ctx:  &ConstraintContext{Labels: map[string]string{"env": "dev"}},
			want: false,
		},
		{
			name:    "labels missing key error",
			expr:    "labels.env==prod",
			ctx:     &ConstraintContext{Labels: map[string]string{"other": "value"}},
			wantErr: true,
		},
		{
			name:    "labels nil map error",
			expr:    "labels.env==prod",
			ctx:     &ConstraintContext{Labels: nil},
			wantErr: true,
		},
		{
			name: "spec equals match",
			expr: "spec.format==a4",
			ctx:  &ConstraintContext{Spec: map[string]any{"format": "a4"}},
			want: true,
		},
		{
			name: "spec equals no match",
			expr: "spec.format==a4",
			ctx:  &ConstraintContext{Spec: map[string]any{"format": "letter"}},
			want: false,
		},
		{
			name: "spec boolean true",
			expr: "spec.enabled==true",
			ctx:  &ConstraintContext{Spec: map[string]any{"enabled": true}},
			want: true,
		},
		{
			name: "spec boolean false",
			expr: "spec.enabled==false",
			ctx:  &ConstraintContext{Spec: map[string]any{"enabled": false}},
			want: true,
		},
		{
			name: "spec nested path match",
			expr: "spec.output.format==pdf",
			ctx:  &ConstraintContext{Spec: map[string]any{"output": map[string]any{"format": "pdf"}}},
			want: true,
		},
		{
			name:    "spec missing field error",
			expr:    "spec.unknown==value",
			ctx:     &ConstraintContext{Spec: map[string]any{"format": "a4"}},
			wantErr: true,
		},
		{
			name:    "spec nil map error",
			expr:    "spec.format==a4",
			ctx:     &ConstraintContext{Spec: nil},
			wantErr: true,
		},
		{
			name:    "nil context error",
			expr:    "mode==preview",
			ctx:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.expr)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.expr, err)
			}

			got, err := c.Evaluate(tt.ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Evaluate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Evaluate() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateConstraints(t *testing.T) {
	tests := []struct {
		name        string
		constraints []string
		ctx         *ConstraintContext
		want        bool
		wantErr     bool
	}{
		{
			name:        "empty constraints always match",
			constraints: nil,
			ctx:         &ConstraintContext{Mode: ModeBuild},
			want:        true,
		},
		{
			name:        "single constraint match",
			constraints: []string{"mode==preview"},
			ctx:         &ConstraintContext{Mode: ModePreview},
			want:        true,
		},
		{
			name:        "single constraint no match",
			constraints: []string{"mode==build"},
			ctx:         &ConstraintContext{Mode: ModePreview},
			want:        false,
		},
		{
			name:        "multiple constraints all match",
			constraints: []string{"mode==preview", "labels.env==prod"},
			ctx: &ConstraintContext{
				Mode:   ModePreview,
				Labels: map[string]string{"env": "prod"},
			},
			want: true,
		},
		{
			name:        "multiple constraints one fails",
			constraints: []string{"mode==preview", "labels.env==dev"},
			ctx: &ConstraintContext{
				Mode:   ModePreview,
				Labels: map[string]string{"env": "prod"},
			},
			want: false,
		},
		{
			name:        "invalid constraint syntax",
			constraints: []string{"invalid"},
			ctx:         &ConstraintContext{Mode: ModePreview},
			wantErr:     true,
		},
		{
			name:        "constraint with missing label",
			constraints: []string{"labels.missing==value"},
			ctx:         &ConstraintContext{Labels: map[string]string{}},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateConstraints(tt.constraints, tt.ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("EvaluateConstraints() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("EvaluateConstraints() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateConstraints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConstraintErrorFormat(t *testing.T) {
	err := &ConstraintError{
		Constraint: "labels.env==prod",
		Reason:     "label \"env\" not defined on artefact",
		Hint:       "add 'labels.env' to the ReportArtefact's metadata",
		Kind:       "DataSet",
		Name:       "sales_data",
	}

	errStr := err.Error()

	// Check that all parts are present
	if !containsAll(errStr, "DataSet", "sales_data", "label \"env\" not defined", "labels.env==prod", "add 'labels.env'") {
		t.Errorf("Error message missing expected parts: %s", errStr)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Tests for new "in" and "not-in" operators
func TestParseConstraint_InOperator(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    *Constraint
		wantErr bool
	}{
		{
			name: "mode in array",
			expr: "mode in [build,preview]",
			want: &Constraint{
				Raw:      "mode in [build,preview]",
				Left:     "mode",
				Operator: OpIn,
				Values:   []string{"build", "preview"},
			},
		},
		{
			name: "labels in array",
			expr: "labels.env in [dev,staging,prod]",
			want: &Constraint{
				Raw:      "labels.env in [dev,staging,prod]",
				Left:     "labels.env",
				Operator: OpIn,
				Values:   []string{"dev", "staging", "prod"},
			},
		},
		{
			name: "spec in array",
			expr: "spec.format in [pdf,png]",
			want: &Constraint{
				Raw:      "spec.format in [pdf,png]",
				Left:     "spec.format",
				Operator: OpIn,
				Values:   []string{"pdf", "png"},
			},
		},
		{
			name: "mode not-in array",
			expr: "mode not-in [serve]",
			want: &Constraint{
				Raw:      "mode not-in [serve]",
				Left:     "mode",
				Operator: OpNotIn,
				Values:   []string{"serve"},
			},
		},
		{
			name: "labels not-in array",
			expr: "labels.region not-in [europe,asia]",
			want: &Constraint{
				Raw:      "labels.region not-in [europe,asia]",
				Left:     "labels.region",
				Operator: OpNotIn,
				Values:   []string{"europe", "asia"},
			},
		},
		{
			name:    "in without array syntax",
			expr:    "mode in build",
			wantErr: true,
		},
		{
			name:    "empty array",
			expr:    "mode in []",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConstraint(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseConstraint(%q) expected error, got nil", tt.expr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseConstraint(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if got.Left != tt.want.Left {
				t.Errorf("Left = %q, want %q", got.Left, tt.want.Left)
			}
			if got.Operator != tt.want.Operator {
				t.Errorf("Operator = %q, want %q", got.Operator, tt.want.Operator)
			}
			if len(got.Values) != len(tt.want.Values) {
				t.Errorf("Values length = %d, want %d", len(got.Values), len(tt.want.Values))
				return
			}
			for i, v := range got.Values {
				if v != tt.want.Values[i] {
					t.Errorf("Values[%d] = %q, want %q", i, v, tt.want.Values[i])
				}
			}
		})
	}
}

func TestConstraintEvaluate_InOperator(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		ctx     *ConstraintContext
		want    bool
		wantErr bool
	}{
		{
			name: "mode in array - match",
			expr: "mode in [build,preview]",
			ctx:  &ConstraintContext{Mode: ModePreview},
			want: true,
		},
		{
			name: "mode in array - no match",
			expr: "mode in [build,preview]",
			ctx:  &ConstraintContext{Mode: ModeServe},
			want: false,
		},
		{
			name: "mode not-in array - match",
			expr: "mode not-in [serve]",
			ctx:  &ConstraintContext{Mode: ModeBuild},
			want: true,
		},
		{
			name: "mode not-in array - no match",
			expr: "mode not-in [build,preview]",
			ctx:  &ConstraintContext{Mode: ModePreview},
			want: false,
		},
		{
			name: "labels in array - match",
			expr: "labels.env in [dev,staging,prod]",
			ctx:  &ConstraintContext{Labels: map[string]string{"env": "staging"}},
			want: true,
		},
		{
			name: "labels in array - no match",
			expr: "labels.env in [dev,staging]",
			ctx:  &ConstraintContext{Labels: map[string]string{"env": "prod"}},
			want: false,
		},
		{
			name: "spec in array - match",
			expr: "spec.format in [pdf,png,html]",
			ctx:  &ConstraintContext{Spec: map[string]any{"format": "pdf"}},
			want: true,
		},
		{
			name: "spec not-in array - match",
			expr: "spec.format not-in [html]",
			ctx:  &ConstraintContext{Spec: map[string]any{"format": "pdf"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.expr)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.expr, err)
			}

			got, err := c.Evaluate(tt.ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Evaluate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Evaluate() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Tests for structured constraint format
func TestParseStructuredConstraint(t *testing.T) {
	tests := []struct {
		name    string
		sc      *schema.StructuredConstraint
		want    *Constraint
		wantErr bool
	}{
		{
			name: "equals operator",
			sc:   &schema.StructuredConstraint{Field: "labels.env", Operator: "==", Value: "prod"},
			want: &Constraint{Left: "labels.env", Operator: OpEquals, Right: "prod"},
		},
		{
			name: "not equals operator",
			sc:   &schema.StructuredConstraint{Field: "mode", Operator: "!=", Value: "serve"},
			want: &Constraint{Left: "mode", Operator: OpNotEquals, Right: "serve"},
		},
		{
			name: "in operator with array",
			sc:   &schema.StructuredConstraint{Field: "spec.format", Operator: "in", Value: []any{"pdf", "png"}},
			want: &Constraint{Left: "spec.format", Operator: OpIn, Values: []string{"pdf", "png"}},
		},
		{
			name: "not-in operator with array",
			sc:   &schema.StructuredConstraint{Field: "labels.region", Operator: "not-in", Value: []any{"asia"}},
			want: &Constraint{Left: "labels.region", Operator: OpNotIn, Values: []string{"asia"}},
		},
		{
			name: "boolean value true",
			sc:   &schema.StructuredConstraint{Field: "spec.enabled", Operator: "==", Value: true},
			want: &Constraint{Left: "spec.enabled", Operator: OpEquals, Right: "true"},
		},
		{
			name: "boolean value false",
			sc:   &schema.StructuredConstraint{Field: "spec.enabled", Operator: "==", Value: false},
			want: &Constraint{Left: "spec.enabled", Operator: OpEquals, Right: "false"},
		},
		{
			name:    "missing field",
			sc:      &schema.StructuredConstraint{Field: "", Operator: "==", Value: "prod"},
			wantErr: true,
		},
		{
			name:    "missing operator",
			sc:      &schema.StructuredConstraint{Field: "labels.env", Operator: "", Value: "prod"},
			wantErr: true,
		},
		{
			name:    "invalid operator",
			sc:      &schema.StructuredConstraint{Field: "labels.env", Operator: "equals", Value: "prod"},
			wantErr: true,
		},
		{
			name:    "invalid field (unknown root)",
			sc:      &schema.StructuredConstraint{Field: "foo.bar", Operator: "==", Value: "prod"},
			wantErr: true,
		},
		{
			name:    "in operator with non-array value",
			sc:      &schema.StructuredConstraint{Field: "labels.env", Operator: "in", Value: "prod"},
			wantErr: true,
		},
		{
			name:    "in operator with empty array",
			sc:      &schema.StructuredConstraint{Field: "labels.env", Operator: "in", Value: []any{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStructuredConstraint(tt.sc)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseStructuredConstraint() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseStructuredConstraint() unexpected error: %v", err)
				return
			}
			if got.Left != tt.want.Left {
				t.Errorf("Left = %q, want %q", got.Left, tt.want.Left)
			}
			if got.Operator != tt.want.Operator {
				t.Errorf("Operator = %q, want %q", got.Operator, tt.want.Operator)
			}
			if tt.want.Right != "" && got.Right != tt.want.Right {
				t.Errorf("Right = %q, want %q", got.Right, tt.want.Right)
			}
			if len(tt.want.Values) > 0 {
				if len(got.Values) != len(tt.want.Values) {
					t.Errorf("Values length = %d, want %d", len(got.Values), len(tt.want.Values))
					return
				}
				for i, v := range got.Values {
					if v != tt.want.Values[i] {
						t.Errorf("Values[%d] = %q, want %q", i, v, tt.want.Values[i])
					}
				}
			}
		})
	}
}

// Tests for ParseMixedConstraints (supports both string and object formats)
func TestParseMixedConstraints(t *testing.T) {
	tests := []struct {
		name    string
		raw     []any
		wantLen int
		wantErr bool
	}{
		{
			name:    "empty list",
			raw:     nil,
			wantLen: 0,
		},
		{
			name:    "string only",
			raw:     []any{"mode==preview", "labels.env==prod"},
			wantLen: 2,
		},
		{
			name: "object only",
			raw: []any{
				map[string]any{"field": "mode", "operator": "==", "value": "preview"},
				map[string]any{"field": "labels.env", "operator": "in", "value": []any{"dev", "prod"}},
			},
			wantLen: 2,
		},
		{
			name: "mixed string and object",
			raw: []any{
				"mode==preview",
				map[string]any{"field": "labels.env", "operator": "==", "value": "prod"},
			},
			wantLen: 2,
		},
		{
			name:    "invalid type",
			raw:     []any{123},
			wantErr: true,
		},
		{
			name:    "invalid string syntax",
			raw:     []any{"invalid"},
			wantErr: true,
		},
		{
			name: "invalid object",
			raw: []any{
				map[string]any{"field": "invalid.field", "operator": "==", "value": "prod"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMixedConstraints(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseMixedConstraints() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseMixedConstraints() unexpected error: %v", err)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("ParseMixedConstraints() returned %d constraints, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// Tests for EvaluateParsedConstraints
func TestEvaluateParsedConstraints(t *testing.T) {
	ctx := &ConstraintContext{
		Mode:   ModePreview,
		Labels: map[string]string{"env": "prod", "region": "europe"},
		Spec:   map[string]any{"format": "a4"},
	}

	tests := []struct {
		name        string
		constraints []*Constraint
		want        bool
		wantErr     bool
	}{
		{
			name:        "empty constraints always match",
			constraints: nil,
			want:        true,
		},
		{
			name: "all constraints match",
			constraints: []*Constraint{
				{Left: "mode", Operator: OpEquals, Right: "preview"},
				{Left: "labels.env", Operator: OpEquals, Right: "prod"},
			},
			want: true,
		},
		{
			name: "one constraint fails",
			constraints: []*Constraint{
				{Left: "mode", Operator: OpEquals, Right: "preview"},
				{Left: "labels.env", Operator: OpEquals, Right: "dev"},
			},
			want: false,
		},
		{
			name: "in operator match",
			constraints: []*Constraint{
				{Left: "labels.env", Operator: OpIn, Values: []string{"dev", "prod"}},
			},
			want: true,
		},
		{
			name: "not-in operator match",
			constraints: []*Constraint{
				{Left: "labels.env", Operator: OpNotIn, Values: []string{"dev", "staging"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateParsedConstraints(tt.constraints, ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("EvaluateParsedConstraints() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("EvaluateParsedConstraints() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateParsedConstraints() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test that string and structured formats produce identical results
func TestStringAndStructuredFormatEquivalence(t *testing.T) {
	ctx := &ConstraintContext{
		Mode:   ModePreview,
		Labels: map[string]string{"env": "prod"},
		Spec:   map[string]any{"format": "pdf"},
	}

	tests := []struct {
		name       string
		strExpr    string
		structured *schema.StructuredConstraint
	}{
		{
			name:       "mode equals",
			strExpr:    "mode==preview",
			structured: &schema.StructuredConstraint{Field: "mode", Operator: "==", Value: "preview"},
		},
		{
			name:       "labels equals",
			strExpr:    "labels.env==prod",
			structured: &schema.StructuredConstraint{Field: "labels.env", Operator: "==", Value: "prod"},
		},
		{
			name:       "spec not equals",
			strExpr:    "spec.format!=png",
			structured: &schema.StructuredConstraint{Field: "spec.format", Operator: "!=", Value: "png"},
		},
		{
			name:       "mode in array",
			strExpr:    "mode in [build,preview]",
			structured: &schema.StructuredConstraint{Field: "mode", Operator: "in", Value: []any{"build", "preview"}},
		},
		{
			name:       "labels not-in array",
			strExpr:    "labels.env not-in [dev,staging]",
			structured: &schema.StructuredConstraint{Field: "labels.env", Operator: "not-in", Value: []any{"dev", "staging"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse string format
			strConstraint, err := ParseConstraint(tt.strExpr)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.strExpr, err)
			}

			// Parse structured format
			structConstraint, err := ParseStructuredConstraint(tt.structured)
			if err != nil {
				t.Fatalf("ParseStructuredConstraint() error: %v", err)
			}

			// Evaluate both
			strResult, strErr := strConstraint.Evaluate(ctx)
			structResult, structErr := structConstraint.Evaluate(ctx)

			// Both should produce same error status
			if (strErr != nil) != (structErr != nil) {
				t.Errorf("Error status mismatch: string=%v, structured=%v", strErr, structErr)
			}

			// Both should produce same result
			if strResult != structResult {
				t.Errorf("Result mismatch: string=%v, structured=%v", strResult, structResult)
			}
		})
	}
}
