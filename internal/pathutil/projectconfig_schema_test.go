package pathutil_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/pathutil"
)

type schemaNode struct {
	Ref                  string                 `json:"$ref"`
	Type                 string                 `json:"type"`
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties json.RawMessage        `json:"additionalProperties"`
	Items                *schemaNode            `json:"items"`
	Definitions          map[string]*schemaNode `json:"definitions"`
}

func loadSchema(t *testing.T) *schemaNode {
	t.Helper()
	var root schemaNode
	if err := json.Unmarshal(pathutil.ProjectConfigSchema, &root); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return &root
}

func (r *schemaNode) deref(t *testing.T, root *schemaNode) *schemaNode {
	t.Helper()
	if r.Ref == "" {
		return r
	}
	name, ok := strings.CutPrefix(r.Ref, "#/definitions/")
	def := root.Definitions[name]
	if !ok || def == nil {
		t.Fatalf("unresolved $ref %q", r.Ref)
	}
	return def.deref(t, root)
}

// valueSchema returns the schema of a map's values (additionalProperties).
func (r *schemaNode) valueSchema(t *testing.T) *schemaNode {
	t.Helper()
	var n schemaNode
	if json.Unmarshal(r.AdditionalProperties, &n) != nil {
		return nil
	}
	return &n
}

// TestProjectConfigSchemaCoversStruct walks ProjectConfig and checks that the
// schema has a property of the right type for every toml field, and no
// property the struct does not read.
func TestProjectConfigSchemaCoversStruct(t *testing.T) {
	root := loadSchema(t)
	checkType(t, root, root, reflect.TypeOf(pathutil.ProjectConfig{}), "bino.toml")
}

func checkType(t *testing.T, root, s *schemaNode, typ reflect.Type, path string) {
	t.Helper()
	s = s.deref(t, root)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(pathutil.HooksConfig{}) {
		checkHooks(t, root, s, path)
		return
	}
	want := map[reflect.Kind]string{
		reflect.String: "string", reflect.Slice: "array",
		reflect.Map: "object", reflect.Struct: "object",
	}[typ.Kind()]
	if want != "" && s.Type != want {
		t.Errorf("%s: schema type %q, struct wants %q", path, s.Type, want)
	}
	if typ == reflect.TypeOf(pathutil.CommandEnv{}) {
		// Variables may also sit directly under [<cmd>.env] (see ParseProjectConfig).
		if v := s.valueSchema(t); v == nil || v.Type != "string" {
			t.Errorf("%s: env needs string additionalProperties", path)
		}
	}
	switch typ.Kind() {
	case reflect.Struct:
		seen := make(map[string]bool)
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
			if name == "" || name == "-" {
				continue
			}
			seen[name] = true
			prop := s.Properties[name]
			if prop == nil {
				t.Errorf("%s: schema has no property %q (field %s)", path, name, f.Name)
				continue
			}
			checkType(t, root, prop, f.Type, path+"."+name)
		}
		for name := range s.Properties {
			if !seen[name] {
				t.Errorf("%s: schema property %q is not a field of %s", path, name, typ.Name())
			}
		}
	case reflect.Map:
		if typ.Elem().Kind() == reflect.Interface {
			return // free-form, e.g. [build.args]
		}
		v := s.valueSchema(t)
		if v == nil {
			t.Errorf("%s: map needs an additionalProperties schema", path)
			return
		}
		checkType(t, root, v, typ.Elem(), path+".*")
	case reflect.Slice:
		if s.Items == nil {
			t.Errorf("%s: array needs items", path)
			return
		}
		checkType(t, root, s.Items, typ.Elem(), path+"[]")
	default:
	}
}

// checkHooks: HooksConfig is a map, but only the known checkpoints run.
func checkHooks(t *testing.T, root, s *schemaNode, path string) {
	t.Helper()
	got := make([]string, 0, len(s.Properties))
	for name, p := range s.Properties {
		got = append(got, name)
		checkType(t, root, p, reflect.TypeOf([]string{}), path+"."+name)
	}
	want := make([]string, 0, len(hooks.ValidCheckpoints))
	for name := range hooks.ValidCheckpoints {
		want = append(want, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: schema checkpoints %v, want %v", path, got, want)
	}
}

// TestProjectConfigSchemaCopies keeps the copies shipped with the VS Code
// extension and served on the docs site in sync with the embedded schema.
func TestProjectConfigSchemaCopies(t *testing.T) {
	for _, p := range []string{
		"../../vscode-bino/schema/bino-toml.schema.json",
		"../../docs/public/schemas/bino-toml.schema.json",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if !bytes.Equal(b, pathutil.ProjectConfigSchema) {
			t.Errorf("%s differs from internal/pathutil/bino-toml.schema.json: copy it over", p)
		}
	}
}
