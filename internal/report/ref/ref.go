// Package ref implements component-reference resolution: the one rule for
// how a {kind, ref, params, spec} child resolves against the document index.
// The renderer's layout children, tree nodes, and grid children and the graph
// builder's layout children all share this implementation — it existed four
// times before and the copies had diverged (nil-context guards, LayoutPage
// misuse detection, constraint-filter handling, and error wrapping each
// differed by call site).
package ref

import (
	"encoding/json"
	"fmt"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

// Ref describes one component reference to resolve.
type Ref struct {
	// Kind is the referenced manifest kind (e.g. "Table").
	Kind string
	// Name is the ref target (metadata.name). Empty means the child is
	// inline and Spec is the complete spec.
	Name string
	// Spec is the inline spec (no Name) or the spec overrides (with Name).
	Spec json.RawMessage
	// Optional makes a missing ref a graceful skip instead of an error.
	Optional bool
	// Params are the caller-supplied parameter values expanded into the
	// referenced document (against its declared metadata.params).
	Params map[string]string
}

// Options carries the resolution context.
type Options struct {
	// Index maps "Kind:Name" to the (possibly constraint-filtered) documents
	// refs resolve against. Required for ref children.
	Index map[string]config.Document
	// GlobalIndex maps "Kind:Name" over the unfiltered document set. A ref
	// present here but absent from Index was filtered by constraints and is
	// skipped gracefully. Callers without constraint filtering pass Index.
	GlobalIndex map[string]config.Document
	// IsPage reports whether name identifies a LayoutPage — referencing one
	// as a child is explicitly disallowed and gets a dedicated error.
	IsPage func(name string) bool
	// Log receives the skip messages (Debug level). Nil means logx.Nop.
	Log logx.Logger
	// Where prefixes error messages with the resolution site, e.g.
	// `layout child in "main_page"`. Optional.
	Where string
}

// Result is the outcome of a resolution.
type Result struct {
	// Spec is the effective spec (inline, referenced, or referenced with
	// overrides deep-merged). Nil when Skipped.
	Spec json.RawMessage
	// Doc is the referenced document (zero value for inline children).
	Doc config.Document
	// Skipped reports the graceful-skip outcomes: the ref was
	// constraint-filtered, or optional and missing.
	Skipped bool
}

// Resolve resolves one reference. Inline children pass through; ref children
// look up Options.Index, honor constraint filtering and Optional, expand
// params, and deep-merge overrides.
func Resolve(r Ref, opt Options) (Result, error) {
	if r.Name == "" {
		return Result{Spec: r.Spec}, nil
	}

	if opt.Index == nil {
		return Result{}, opt.errorf("ref %q cannot be resolved without a document index", r.Name)
	}
	log := opt.Log
	if log == nil {
		log = logx.Nop()
	}

	key := r.Kind + ":" + r.Name
	refDoc, found := opt.Index[key]
	if !found {
		// Referencing a LayoutPage is explicitly disallowed, in every context.
		if opt.IsPage != nil && opt.IsPage(r.Name) {
			return Result{}, opt.errorf("ref %q points to LayoutPage which cannot be referenced; only Text, Table, ChartStructure, ChartTime, ChartScatter, ChartBubble, ChartBullet, Tree, Grid, LayoutCard, and Image can be referenced", r.Name)
		}

		// Present in the unfiltered set but not in the filtered one: the ref
		// was filtered by constraints — skip gracefully.
		if opt.GlobalIndex != nil {
			if _, existsGlobally := opt.GlobalIndex[key]; existsGlobally {
				log.Debugf("%sref %q of kind %q filtered by constraints, skipping", opt.prefix(), r.Name, r.Kind)
				return Result{Skipped: true}, nil
			}
		}

		if r.Optional {
			log.Debugf("%soptional ref %q of kind %q not found, skipping", opt.prefix(), r.Name, r.Kind)
			return Result{Skipped: true}, nil
		}

		return Result{}, opt.errorf("required reference %q of kind %q not found (use optional: true to allow missing refs)", r.Name, r.Kind)
	}

	// Expand params into the referenced document before extracting its spec.
	refRaw := refDoc.Raw
	if len(r.Params) > 0 || len(refDoc.Params) > 0 {
		refRaw, _ = config.ExpandDocParams(refDoc.Raw, refDoc.Params, r.Params)
	}

	var refPayload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(refRaw, &refPayload); err != nil {
		return Result{}, opt.errorf("failed to parse ref %q spec: %w", r.Name, err)
	}

	if len(r.Spec) == 0 || string(r.Spec) == "null" {
		return Result{Spec: refPayload.Spec, Doc: refDoc}, nil
	}

	merged, err := MergeSpec(refPayload.Spec, r.Spec)
	if err != nil {
		return Result{}, opt.errorf("failed to merge ref %q with overrides: %w", r.Name, err)
	}
	return Result{Spec: merged, Doc: refDoc}, nil
}

func (o Options) prefix() string {
	if o.Where == "" {
		return ""
	}
	return o.Where + ": "
}

func (o Options) errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if o.Where == "" {
		return err
	}
	return fmt.Errorf("%s: %w", o.Where, err)
}

// MergeSpec performs a deep merge of two JSON objects. Values from override
// replace values in base; objects merge recursively; arrays are replaced
// entirely (not merged element-by-element).
func MergeSpec(base, override json.RawMessage) (json.RawMessage, error) {
	var baseMap map[string]json.RawMessage
	var overrideMap map[string]json.RawMessage

	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, fmt.Errorf("base is not a JSON object: %w", err)
	}
	if err := json.Unmarshal(override, &overrideMap); err != nil {
		return nil, fmt.Errorf("override is not a JSON object: %w", err)
	}

	result := make(map[string]json.RawMessage, len(baseMap))
	for k, v := range baseMap {
		result[k] = v
	}

	for k, overrideVal := range overrideMap {
		baseVal, hasBase := result[k]
		if !hasBase {
			result[k] = overrideVal
			continue
		}

		var baseObj map[string]json.RawMessage
		var overrideObj map[string]json.RawMessage
		baseIsObj := json.Unmarshal(baseVal, &baseObj) == nil && baseObj != nil
		overrideIsObj := json.Unmarshal(overrideVal, &overrideObj) == nil && overrideObj != nil

		if baseIsObj && overrideIsObj {
			merged, err := MergeSpec(baseVal, overrideVal)
			if err != nil {
				return nil, err
			}
			result[k] = merged
		} else {
			// Override replaces base (including arrays).
			result[k] = overrideVal
		}
	}

	return json.Marshal(result)
}
