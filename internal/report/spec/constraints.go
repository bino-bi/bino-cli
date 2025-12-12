package spec

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Mode represents the execution mode for constraint evaluation.
type Mode string

const (
	// ModeBuild is used when building artefacts for production output.
	ModeBuild Mode = "build"
	// ModePreview is used when previewing artefacts during development.
	ModePreview Mode = "preview"
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

// Constraint represents a parsed constraint expression.
type Constraint struct {
	Raw      string // Original expression string
	Left     string // Left operand (e.g., "spec.format", "labels.preview", "mode")
	Operator string // "==" or "!="
	Right    string // Right operand value (literal)
}

// ParseConstraint parses a constraint expression string into its components.
// Supported expressions:
//   - spec.<field> == <value>
//   - spec.<field> != <value>
//   - labels.<key> == <value>
//   - labels.<key> != <value>
//   - mode == <value>
//   - mode != <value>
func ParseConstraint(expr string) (*Constraint, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "empty constraint expression",
			Hint:       "provide a constraint like 'mode==preview' or 'labels.env==prod'",
		}
	}

	// Detect operator (check != first since it's longer)
	var operator string
	var opIdx int
	if idx := strings.Index(expr, "!="); idx > 0 {
		operator = "!="
		opIdx = idx
	} else if idx := strings.Index(expr, "=="); idx > 0 {
		operator = "=="
		opIdx = idx
	} else {
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     "missing operator",
			Hint:       "use '==' or '!=' to compare values, e.g., 'mode==preview'",
		}
	}

	left := strings.TrimSpace(expr[:opIdx])
	right := strings.TrimSpace(expr[opIdx+2:])

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

	// Validate left operand root
	root := left
	if idx := strings.Index(left, "."); idx > 0 {
		root = left[:idx]
	}

	switch root {
	case "spec", "labels", "mode":
		// valid roots
	default:
		return nil, &ConstraintError{
			Constraint: expr,
			Reason:     fmt.Sprintf("unknown root %q in left operand", root),
			Hint:       "left side must start with 'spec.', 'labels.', or be 'mode'",
		}
	}

	// For spec and labels, require a path after the dot
	if root == "spec" || root == "labels" {
		if !strings.Contains(left, ".") {
			return nil, &ConstraintError{
				Constraint: expr,
				Reason:     fmt.Sprintf("%s requires a field path", root),
				Hint:       fmt.Sprintf("use '%s.<fieldname>' to access a specific field", root),
			}
		}
	}

	return &Constraint{
		Raw:      expr,
		Left:     left,
		Operator: operator,
		Right:    right,
	}, nil
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
	case "==":
		return actual == c.Right, nil
	case "!=":
		return actual != c.Right, nil
	default:
		return false, &ConstraintError{
			Constraint: c.Raw,
			Reason:     fmt.Sprintf("unsupported operator %q", c.Operator),
			Hint:       "use '==' or '!='",
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

// ConstraintsFromRaw extracts constraints from a JSON-encoded document.
func ConstraintsFromRaw(raw json.RawMessage) ([]string, error) {
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
