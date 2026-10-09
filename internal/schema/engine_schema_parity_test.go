package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var dockerfileEnginePin = regexp.MustCompile(`(?m)^ARG ENGINE_VERSION=(\S+)`)

// The template engine publishes a JSON Schema for the content of a
// ComponentStyle and of a RuleSet. document.schema.json describes the same two
// objects by hand, so a key the engine adds, renames or drops went unnoticed
// here. This test compares every property name of the two descriptions.
//
// The engine schemas are a copy of the schemas/ directory of the engine
// release, kept under testdata/engine-schemas/<version>. The version is the
// engine pin of the Dockerfile, so moving the pin asks for a new copy.
func TestThemeAndRuleSetKeysMatchPinnedEngine(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	m := dockerfileEnginePin.FindSubmatch(dockerfile)
	if m == nil {
		t.Fatal("no ARG ENGINE_VERSION pin in Dockerfile")
	}
	pin := string(m[1])
	dir := filepath.Join("testdata", "engine-schemas", pin)

	var cli map[string]any
	if err := json.Unmarshal(DocumentSchemaBytes(), &cli); err != nil {
		t.Fatalf("parse document.schema.json: %v", err)
	}

	tests := []struct {
		file string // engine schema
		def  string // $defs entry of document.schema.json
	}{
		{file: "theme.schema.json", def: "styleContent"},
		{file: "ruleset.schema.json", def: "ruleSetContent"},
	}
	for _, tt := range tests {
		t.Run(tt.def, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatalf("no engine schema for the pinned engine %s: %v\n"+
					"copy the schemas/ directory of that engine release to %s", pin, err, dir)
			}
			var engine map[string]any
			if err := json.Unmarshal(raw, &engine); err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}

			want := map[string]bool{}
			collectKeyPaths(engine, engine, "", nil, want)
			got := map[string]bool{}
			collectKeyPaths(cli, map[string]any{"$ref": "#/$defs/" + tt.def}, "", nil, got)

			if len(want) == 0 {
				t.Fatalf("%s declares no properties", tt.file)
			}
			for _, key := range sortedKeys(want) {
				if !got[key] {
					t.Errorf("engine %s has %q, $defs.%s does not", pin, key, tt.def)
				}
			}
			for _, key := range sortedKeys(got) {
				if !want[key] {
					t.Errorf("$defs.%s has %q, engine %s does not", tt.def, key, pin)
				}
			}
		})
	}
}

// collectKeyPaths adds the dotted path of every property below node to out.
// It follows $ref inside root, and the schema of additionalProperties as "*".
func collectKeyPaths(root, node map[string]any, prefix string, refs []string, out map[string]bool) {
	if ref, ok := node["$ref"].(string); ok {
		for _, seen := range refs {
			if seen == ref {
				return // recursive definition
			}
		}
		target := resolveLocalRef(root, ref)
		if target == nil {
			out[prefix+"<unresolved "+ref+">"] = true
			return
		}
		collectKeyPaths(root, target, prefix, append(refs, ref), out)
		return
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for name, raw := range props {
			out[prefix+name] = true
			if child, ok := raw.(map[string]any); ok {
				collectKeyPaths(root, child, prefix+name+".", refs, out)
			}
		}
	}
	if extra, ok := node["additionalProperties"].(map[string]any); ok {
		collectKeyPaths(root, extra, prefix+"*.", refs, out)
	}
}

// resolveLocalRef resolves a "#/a/b" pointer inside root, or returns nil.
func resolveLocalRef(root map[string]any, ref string) map[string]any {
	path, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil
	}
	node := root
	for _, seg := range strings.Split(path, "/") {
		next, ok := node[seg].(map[string]any)
		if !ok {
			return nil
		}
		node = next
	}
	return node
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
