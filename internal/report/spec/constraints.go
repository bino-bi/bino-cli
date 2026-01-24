package spec

import (
	"encoding/json"
	"fmt"
	"strings"

	"bino.bi/bino/internal/schema"
)

// Mode represents the execution mode for constraint evaluation.
type Mode string

const (
	// ModeBuild is used when building artefacts for production output.
	ModeBuild Mode = "build"
	// ModePreview is used when previewing artefacts during development.
	ModePreview Mode = "preview"
	// ModeServe is used when serving artefacts via bino serve.
	ModeServe Mode = "serve"
)

// ConstraintContext holds the context against which constraints are evaluated.
// This includes the target artefact's labels, spec, and the current execution mode.
type ConstraintContext struct {
	// Labels from the ReportArtefact's metadata.labels
	Labels map[string]string
	// Spec holds the ReportArtefact spec as a map for field access
	Spec map[string]any
	// Mode is the current execution mode (build or preview)
	Mode Mode
	// ArtefactKind is the kind of artefact being evaluated: "report", "screenshot", "document", or "livereport"
	ArtefactKind string
}

// ConstraintError represents a constraint evaluation error with helpful details.
type ConstraintError struct {
	Constraint string // The original constraint expression
	Reason     string // Why the constraint failed
	Hint       string // Helpful suggestion for fixing
	Kind       string // The kind of the object with the bad constraint
	Name       string // The name of the object with the bad constraint
}

func (e *ConstraintError) Error() string {
	var b strings.Builder
	b.WriteString("constraint error")
	if e.Kind != "" || e.Name != "" {
		b.WriteString(fmt.Sprintf(" in %s %q", e.Kind, e.Name))
	}
	b.WriteString(fmt.Sprintf(": %s", e.Reason))
	if e.Constraint != "" {
		b.WriteString(fmt.Sprintf("\n  constraint: %s", e.Constraint))
	}
	if e.Hint != "" {
		b.WriteString(fmt.Sprintf("\n  hint: %s", e.Hint))
	}
	return b.String()
}

// Supported constraint operators.
const (
	OpEquals    = "=="
	OpNotEquals = "!="
	OpIn        = "in"
	OpNotIn     = "not-in"
)

// Constraint represents a parsed constraint expression.
// It supports both string format ("labels.env == prod") and structured format
// ({field: "labels.env", operator: "==", value: "prod"}).
type Constraint struct {
	Raw      string   // Original expression string (empty for structured format)
	Left     string   // Left operand (e.g., "spec.format", "labels.preview", "mode")
	Operator string   // "==", "!=", "in", or "not-in"
	Right    string   // Right operand value for == and != operators
	Values   []string // Right operand values for in and not-in operators
}

// ParseConstraint parses a constraint expression string into its components.
// Supported expressions:
//   - spec.<field> == <value>
//   - spec.<field> != <value>
//   - spec.<field> in [<value1>,<value2>,...]
//   - spec.<field> not-in [<value1>,<value2>,...]
//   - labels.<key> == <value>
//   - labels.<key> != <value>
//   - labels.<key> in [<value1>,<value2>,...]
//   - labels.<key> not-in [<value1>,<value2>,...]
//   - mode == <value>
//   - mode != <value>
//   - mode in [<value1>,<value2>,...]
//   - mode not-in [<value1>,<value2>,...]
func ParseConstraint(expr string) (*Constraint, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "empty constraint expression",
			Hint:       "provide a constraint like 'mode==preview' or 'labels.env==prod'",
		}
	}

	// Detect operator (check multi-char operators first)
	var operator string
	var opIdx int
	var opLen int

	// Check for "not-in" first (longest operator with word boundary)
	if idx := findOperator(expr, " not-in "); idx > 0 {
		operator = OpNotIn
		opIdx = idx + 1 // skip the leading space
		opLen = 6       // "not-in"
	} else if idx := findOperator(expr, " in "); idx > 0 {
		operator = OpIn
		opIdx = idx + 1 // skip the leading space
		opLen = 2       // "in"
	} else if idx := strings.Index(expr, "!="); idx > 0 {
		operator = OpNotEquals
		opIdx = idx
		opLen = 2
	} else if idx := strings.Index(expr, "=="); idx > 0 {
		operator = OpEquals
		opIdx = idx
		opLen = 2
	} else {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "missing operator",
			Hint:       "use '==', '!=', 'in', or 'not-in' to compare values, e.g., 'mode==preview' or 'labels.env in [dev,staging]'",
		}
	}

	left := strings.TrimSpace(expr[:opIdx])
	right := strings.TrimSpace(expr[opIdx+opLen:])

	if left == "" {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "missing left operand",
			Hint:       "specify what to compare, e.g., 'spec.format', 'labels.env', or 'mode'",
		}
	}

	if right == "" {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "missing right operand",
			Hint:       "specify the expected value after the operator",
		}
	}

	// Validate left operand
	if err := validateLeftOperand(left); err != nil {
		err.Constraint = expr
		return nil, err
	}

	// Parse right operand based on operator
	c := &Constraint{
		Raw:      expr,
		Left:     left,
		Operator: operator,
	}

	if operator == OpIn || operator == OpNotIn {
		values, err := parseArrayValue(right)
		if err != nil {
			return nil, &ConstraintError{
				Constraint: expr,
				Reason:     err.Error(),
				Hint:       "use array syntax like [value1,value2] for 'in' and 'not-in' operators",
			}
		}
		c.Values = values
	} else {
		c.Right = right
	}

	return c, nil
}

// findOperator finds an operator in the expression, returning -1 if not found.
func findOperator(expr, op string) int {
	return strings.Index(expr, op)
}

// validateLeftOperand validates the left operand of a constraint.
func validateLeftOperand(left string) *ConstraintError {
	root := left
	if idx := strings.Index(left, "."); idx > 0 {
		root = left[:idx]
	}

	switch root {
	case "spec", "labels", "mode", "artefactKind":
		// valid roots
	default:
		return &ConstraintError{
			Reason: fmt.Sprintf("unknown root %q in left operand", root),
			Hint:   "left side must start with 'spec.', 'labels.', or be 'mode' or 'artefactKind'",
		}
	}

	// For spec and labels, require a path after the dot
	if root == "spec" || root == "labels" {
		if !strings.Contains(left, ".") {
			return &ConstraintError{
				Reason: fmt.Sprintf("%s requires a field path", root),
				Hint:   fmt.Sprintf("use '%s.<fieldname>' to access a specific field", root),
			}
		}
	}

	return nil
}

// parseArrayValue parses a value in array format: [value1,value2,...]
func parseArrayValue(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("expected array format [value1,value2,...], got %q", s)
	}

	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, fmt.Errorf("empty array not allowed")
	}

	parts := strings.Split(inner, ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			return nil, fmt.Errorf("empty value in array")
		}
		values = append(values, v)
	}

	return values, nil
}

// ParseStructuredConstraint parses a structured constraint object into a Constraint.
func ParseStructuredConstraint(sc *schema.StructuredConstraint) (*Constraint, error) {
	if sc.Field == "" {
		return nil, &ConstraintError{
			Reason: "missing 'field' in structured constraint",
			Hint:   "specify the field to check, e.g., 'labels.env', 'spec.format', or 'mode'",
		}
	}

	if sc.Operator == "" {
		return nil, &ConstraintError{
			Reason: "missing 'operator' in structured constraint",
			Hint:   "use '==', '!=', 'in', or 'not-in'",
		}
	}

	// Validate operator
	switch sc.Operator {
	case OpEquals, OpNotEquals, OpIn, OpNotIn:
		// valid
	default:
		return nil, &ConstraintError{
			Reason: fmt.Sprintf("invalid operator %q in structured constraint", sc.Operator),
			Hint:   "use '==', '!=', 'in', or 'not-in'",
		}
	}

	// Validate field (left operand)
	if err := validateLeftOperand(sc.Field); err != nil {
		return nil, err
	}

	c := &Constraint{
		Left:     sc.Field,
		Operator: sc.Operator,
	}

	// Parse value based on operator
	if sc.Operator == OpIn || sc.Operator == OpNotIn {
		values, err := parseStructuredArrayValue(sc.Value)
		if err != nil {
			return nil, &ConstraintError{
				Reason: err.Error(),
				Hint:   "use an array value for 'in' and 'not-in' operators, e.g., [\"pdf\", \"png\"]",
			}
		}
		c.Values = values
	} else {
		val, err := parseStructuredScalarValue(sc.Value)
		if err != nil {
			return nil, &ConstraintError{
				Reason: err.Error(),
				Hint:   "use a string or boolean value for '==' and '!=' operators",
			}
		}
		c.Right = val
	}

	return c, nil
}

// parseStructuredArrayValue converts a value to []string for in/not-in operators.
func parseStructuredArrayValue(v any) ([]string, error) {
	switch val := v.(type) {
	case []any:
		if len(val) == 0 {
			return nil, fmt.Errorf("empty array not allowed for 'in'/'not-in' operator")
		}
		result := make([]string, 0, len(val))
		for i, item := range val {
			s, err := valueToString(item)
			if err != nil {
				return nil, fmt.Errorf("invalid value at index %d: %w", i, err)
			}
			result = append(result, s)
		}
		return result, nil
	case []string:
		if len(val) == 0 {
			return nil, fmt.Errorf("empty array not allowed for 'in'/'not-in' operator")
		}
		return val, nil
	default:
		return nil, fmt.Errorf("expected array value for 'in'/'not-in' operator, got %T", v)
	}
}

// parseStructuredScalarValue converts a value to string for ==/!= operators.
func parseStructuredScalarValue(v any) (string, error) {
	if v == nil {
		return "", fmt.Errorf("missing 'value' in structured constraint")
	}
	return valueToString(v)
}

// valueToString converts various types to their string representation.
func valueToString(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case bool:
		if val {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", val), nil
	case int:
		return fmt.Sprintf("%d", val), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

// Evaluate evaluates a single constraint against the provided context.
// Returns true if the constraint matches, false otherwise.
// Returns an error for invalid constraints or missing paths.
func (c *Constraint) Evaluate(ctx *ConstraintContext) (bool, error) {
	if ctx == nil {
		return false, &ConstraintError{
			Constraint: c.Raw,
			Reason:     "nil context provided",
			Hint:       "provide a valid ConstraintContext with labels, spec, and mode",
		}
	}

	var actual string

	switch {
	case c.Left == "mode":
		actual = string(ctx.Mode)

	case c.Left == "artefactKind":
		actual = ctx.ArtefactKind

	case strings.HasPrefix(c.Left, "labels."):
		key := strings.TrimPrefix(c.Left, "labels.")
		if ctx.Labels == nil {
			return false, &ConstraintError{
				Constraint: c.Raw,
				Reason:     fmt.Sprintf("label %q not found (artefact has no labels)", key),
				Hint:       "add 'metadata.labels' to the target ReportArtefact",
			}
		}
		val, ok := ctx.Labels[key]
		if !ok {
			return false, &ConstraintError{
				Constraint: c.Raw,
				Reason:     fmt.Sprintf("label %q not defined on artefact", key),
				Hint:       fmt.Sprintf("add 'labels.%s' to the ReportArtefact's metadata", key),
			}
		}
		actual = val

	case strings.HasPrefix(c.Left, "spec."):
		path := strings.TrimPrefix(c.Left, "spec.")
		val, err := lookupSpecPath(ctx.Spec, path)
		if err != nil {
			return false, &ConstraintError{
				Constraint: c.Raw,
				Reason:     err.Error(),
				Hint:       "check that the spec field exists on the ReportArtefact",
			}
		}
		actual = val

	default:
		return false, &ConstraintError{
			Constraint: c.Raw,
			Reason:     fmt.Sprintf("unsupported left operand %q", c.Left),
			Hint:       "left side must be 'mode', 'labels.<key>', or 'spec.<field>'",
		}
	}

	// Compare values
	switch c.Operator {
	case OpEquals:
		return actual == c.Right, nil
	case OpNotEquals:
		return actual != c.Right, nil
	case OpIn:
		for _, v := range c.Values {
			if actual == v {
				return true, nil
			}
		}
		return false, nil
	case OpNotIn:
		for _, v := range c.Values {
			if actual == v {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, &ConstraintError{
			Constraint: c.Raw,
			Reason:     fmt.Sprintf("unsupported operator %q", c.Operator),
			Hint:       "use '==', '!=', 'in', or 'not-in'",
		}
	}
}

// lookupSpecPath looks up a dot-separated path in the spec map.
// Returns the string representation of the value or an error if not found.
func lookupSpecPath(spec map[string]any, path string) (string, error) {
	if spec == nil {
		return "", fmt.Errorf("spec field %q not found (spec is empty)", path)
	}

	parts := strings.Split(path, ".")
	current := any(spec)

	for i, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				traversed := strings.Join(parts[:i+1], ".")
				return "", fmt.Errorf("spec field %q not found", traversed)
			}
			current = val
		default:
			traversed := strings.Join(parts[:i], ".")
			return "", fmt.Errorf("cannot traverse into %q: not an object", traversed)
		}
	}

	// Convert final value to string
	switch v := current.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", v), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// EvaluateConstraints evaluates a list of constraint expressions against the context.
// Returns true only if ALL constraints match.
// Returns an error if any constraint is invalid or evaluation fails.
// An empty constraint list returns true (no constraints = always included).
func EvaluateConstraints(constraints []string, ctx *ConstraintContext) (bool, error) {
	if len(constraints) == 0 {
		return true, nil
	}

	for _, expr := range constraints {
		c, err := ParseConstraint(expr)
		if err != nil {
			return false, err
		}

		match, err := c.Evaluate(ctx)
		if err != nil {
			return false, err
		}

		if !match {
			return false, nil
		}
	}

	return true, nil
}

// EvaluateConstraintsWithContext is like EvaluateConstraints but adds object context to errors.
func EvaluateConstraintsWithContext(constraints []string, ctx *ConstraintContext, kind, name string) (bool, error) {
	if len(constraints) == 0 {
		return true, nil
	}

	for _, expr := range constraints {
		c, err := ParseConstraint(expr)
		if err != nil {
			if ce, ok := err.(*ConstraintError); ok {
				ce.Kind = kind
				ce.Name = name
			}
			return false, err
		}

		match, err := c.Evaluate(ctx)
		if err != nil {
			if ce, ok := err.(*ConstraintError); ok {
				ce.Kind = kind
				ce.Name = name
			}
			return false, err
		}

		if !match {
			return false, nil
		}
	}

	return true, nil
}

// SpecToMap converts a JSON-encoded spec to a map for constraint evaluation.
func SpecToMap(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		Spec map[string]any `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse spec for constraints: %w", err)
	}
	return payload.Spec, nil
}

// LabelsFromRaw extracts labels from a JSON-encoded document.
func LabelsFromRaw(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse labels for constraints: %w", err)
	}
	return payload.Metadata.Labels, nil
}

// ConstraintsFromRaw extracts and parses constraints from a JSON-encoded document.
// It supports both string format and structured format constraints.
// String format: "labels.env == prod"
// Structured format: {"field": "labels.env", "operator": "==", "value": "prod"}
func ConstraintsFromRaw(raw json.RawMessage) ([]*Constraint, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		Metadata struct {
			Constraints []any `json:"constraints"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse constraints: %w", err)
	}

	return ParseMixedConstraints(payload.Metadata.Constraints)
}

// ParseMixedConstraints parses a list of constraints that may contain
// both string format and structured format constraints.
func ParseMixedConstraints(rawConstraints []any) ([]*Constraint, error) {
	if len(rawConstraints) == 0 {
		return nil, nil
	}

	constraints := make([]*Constraint, 0, len(rawConstraints))
	for i, raw := range rawConstraints {
		c, err := ParseConstraintFromAny(raw)
		if err != nil {
			return nil, fmt.Errorf("constraint[%d]: %w", i, err)
		}
		constraints = append(constraints, c)
	}
	return constraints, nil
}

// ParseConstraintFromAny parses a constraint from either string or map format.
func ParseConstraintFromAny(v any) (*Constraint, error) {
	switch val := v.(type) {
	case string:
		return ParseConstraint(val)
	case map[string]any:
		sc := &schema.StructuredConstraint{}
		if f, ok := val["field"].(string); ok {
			sc.Field = f
		}
		if o, ok := val["operator"].(string); ok {
			sc.Operator = o
		}
		sc.Value = val["value"]
		return ParseStructuredConstraint(sc)
	default:
		return nil, &ConstraintError{
			Reason: fmt.Sprintf("constraint must be string or object, got %T", v),
			Hint:   "use string format like 'mode==preview' or structured format with field/operator/value",
		}
	}
}

// ConstraintStringsFromRaw extracts constraints as strings from a JSON-encoded document.
// This is for backward compatibility with code that expects []string.
// It only works with string-format constraints; structured constraints will cause an error.
// Deprecated: Use ConstraintsFromRaw instead for full support of both formats.
func ConstraintStringsFromRaw(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		Metadata struct {
			Constraints []string `json:"constraints"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse constraints: %w", err)
	}
	return payload.Metadata.Constraints, nil
}

// EvaluateParsedConstraints evaluates a list of parsed constraints against the context.
// Returns true only if ALL constraints match.
// Returns an error if evaluation fails.
// An empty constraint list returns true (no constraints = always included).
func EvaluateParsedConstraints(constraints []*Constraint, ctx *ConstraintContext) (bool, error) {
	if len(constraints) == 0 {
		return true, nil
	}

	for _, c := range constraints {
		match, err := c.Evaluate(ctx)
		if err != nil {
			return false, err
		}

		if !match {
			return false, nil
		}
	}

	return true, nil
}

// EvaluateParsedConstraintsWithContext is like EvaluateParsedConstraints but adds object context to errors.
func EvaluateParsedConstraintsWithContext(constraints []*Constraint, ctx *ConstraintContext, kind, name string) (bool, error) {
	if len(constraints) == 0 {
		return true, nil
	}

	for _, c := range constraints {
		match, err := c.Evaluate(ctx)
		if err != nil {
			if ce, ok := err.(*ConstraintError); ok {
				ce.Kind = kind
				ce.Name = name
			}
			return false, err
		}

		if !match {
			return false, nil
		}
	}

	return true, nil
}
