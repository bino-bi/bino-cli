package spec

import (
	"testing"
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
