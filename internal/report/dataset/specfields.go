package dataset

import (
	"sort"
	"sync"

	"bino.bi/bino/internal/schema"
	"bino.bi/bino/internal/schema/walk"
)

// KindTokens maps the lowercase kind token of a `_spec_<token>_<field>`
// column (and of constants.spec.<token>) to the manifest kind whose spec it
// targets: the seven kinds that bind a dataset.
var KindTokens = map[string]string{
	"text":           "Text",
	"table":          "Table",
	"chartstructure": "ChartStructure",
	"charttime":      "ChartTime",
	"chartscatter":   "ChartScatter",
	"chartbubble":    "ChartBubble",
	"chartbullet":    "ChartBullet",
}

// AnyToken is the reserved kind token meaning every kind that has the field.
const AnyToken = "any"

// SortedKindTokens returns the kind tokens in sorted order.
func SortedKindTokens() []string {
	tokens := make([]string, 0, len(KindTokens))
	for t := range KindTokens {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	return tokens
}

// FieldType classifies a spec field by the shape a dataset default cell must
// take, derived from the embedded JSON schema.
type FieldType string

const (
	// FieldString covers strings, enums and string-or-number unions.
	FieldString FieldType = "string"
	FieldNumber FieldType = "number"
	FieldBool   FieldType = "boolean"
	// FieldList is a list of strings (also accepted as a comma string).
	FieldList FieldType = "list"
	// FieldObject is an object or a list of objects, filled from a JSON cell,
	// index columns or a row marker (see defaults.go).
	FieldObject FieldType = "object"
)

var (
	specFieldsOnce sync.Once
	specModel      *walk.Model
	specFieldTypes map[string]map[string]FieldType // kind -> field -> type
)

func specFields() map[string]map[string]FieldType {
	specFieldsOnce.Do(func() {
		specModel = walk.Parse(schema.DocumentSchemaBytes())
		specFieldTypes = make(map[string]map[string]FieldType, len(KindTokens))
		for _, kind := range KindTokens {
			fields := map[string]FieldType{}
			for _, p := range specModel.ResolveAt([]string{"spec"}, kindsOf(kind)).Props() {
				fields[p.Name] = classifyField(kind, []string{p.Name}, p)
			}
			specFieldTypes[kind] = fields
		}
	})
	return specFieldTypes
}

func kindsOf(kind string) map[string]string { return map[string]string{"": kind} }

// classifyField classifies the property p found at path (its last segment is
// p.Name) in kind's spec.
func classifyField(kind string, path []string, p walk.PropInfo) FieldType {
	has := func(t string) bool {
		for _, x := range p.Types {
			if x == t {
				return true
			}
		}
		return false
	}
	switch {
	case has("array"):
		item := append(append([]string{"spec"}, path...), "0")
		if specModel.ResolveAt(item, kindsOf(kind)).IsObject() {
			return FieldObject
		}
		return FieldList
	case has("object"):
		return FieldObject
	case has("string") || len(p.Enum) > 0:
		return FieldString
	case has("number") || has("integer"):
		return FieldNumber
	case has("boolean"):
		return FieldBool
	default:
		return FieldString
	}
}

// propAt resolves the property named by the last segment of path inside
// kind's spec; an index segment ("0") walks into a list's items.
func propAt(kind string, path []string) (walk.PropInfo, bool) {
	specFields()
	if len(path) == 0 {
		return walk.PropInfo{}, false
	}
	parent := append([]string{"spec"}, path[:len(path)-1]...)
	return specModel.ResolveAt(parent, kindsOf(kind)).Prop(path[len(path)-1])
}

// FieldTypeAt reports the type of a nested spec field, e.g. Table
// ["columnthereof", "0", "subGroups"] -> list.
func FieldTypeAt(kind string, path []string) (FieldType, bool) {
	p, ok := propAt(kind, path)
	if !ok {
		return "", false
	}
	return classifyField(kind, path, p), true
}

// ObjectKeys returns the property names of the object at path inside kind's
// spec (for a list of objects pass the field and "0"), and the required ones.
func ObjectKeys(kind string, path []string) (keys, required []string) {
	specFields()
	node := specModel.ResolveAt(append([]string{"spec"}, path...), kindsOf(kind))
	for _, p := range node.Props() {
		keys = append(keys, p.Name)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	sort.Strings(keys)
	sort.Strings(required)
	return keys, required
}

// IsListOfObjects reports whether kind's field is a list of objects.
func IsListOfObjects(kind, field string) bool {
	p, ok := propAt(kind, []string{field})
	if !ok {
		return false
	}
	for _, t := range p.Types {
		if t == "array" {
			return specModel.ResolveAt([]string{"spec", field, "0"}, kindsOf(kind)).IsObject()
		}
	}
	return false
}

// ObjectAcceptsString reports whether kind's object field also allows a bare
// string (a measure token such as scatter `x`), so a non-JSON cell is a value.
func ObjectAcceptsString(kind, field string) bool {
	if t, ok := SpecField(kind, field); !ok || t != FieldObject || IsListOfObjects(kind, field) {
		return false
	}
	p, _ := propAt(kind, []string{field})
	for _, t := range p.Types {
		if t == "string" {
			return true
		}
	}
	return false
}

// SpecField reports the type of a kind's spec field, and whether the kind
// (a manifest kind such as "Table") has it.
func SpecField(kind, field string) (FieldType, bool) {
	t, ok := specFields()[kind][field]
	return t, ok
}

// AnyKindWithField returns the kind that types an `any` column: the first
// dataset-bound kind in token order that has the field.
func AnyKindWithField(field string) (string, bool) {
	for _, token := range SortedKindTokens() {
		if _, ok := SpecField(KindTokens[token], field); ok {
			return KindTokens[token], true
		}
	}
	return "", false
}

// AnyKindHasField reports whether any dataset-bound kind has the field,
// typed by the first such kind in token order.
func AnyKindHasField(field string) (FieldType, bool) {
	kind, ok := AnyKindWithField(field)
	if !ok {
		return "", false
	}
	return SpecField(kind, field)
}
