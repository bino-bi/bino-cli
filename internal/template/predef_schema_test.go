package template

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlPkg "gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

// TestPredefRendersValidManifests mirrors TestStandardRendersValidManifests for
// the predef scaffold. It is also the acceptance test for the widened
// metadata.name pattern: "@acme/<name>/revenue_table" only validates once the
// schema accepts the '@scope/package/definition' form.
func TestPredefRendersValidManifests(t *testing.T) {
	created, dest := renderPredef(t)

	seen := 0
	for _, rel := range created {
		if filepath.Ext(rel) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		dec := yamlPkg.NewDecoder(bytes.NewReader(body))
		idx := 0
		for {
			var doc any
			derr := dec.Decode(&doc)
			if errors.Is(derr, io.EOF) {
				break
			}
			if derr != nil {
				t.Errorf("%s: decode document %d: %v", rel, idx, derr)
				break
			}
			idx++
			if doc == nil {
				continue // empty document between separators
			}
			docBytes, merr := yamlPkg.Marshal(doc)
			if merr != nil {
				t.Errorf("%s: marshal document %d: %v", rel, idx, merr)
				continue
			}
			seen++
			if verr := schema.Validate(docBytes); verr != nil {
				t.Errorf("%s document %d failed validation:\n%v", rel, idx, verr)
			}
		}
	}

	if seen == 0 {
		t.Fatal("no manifests validated; the predef template rendered no YAML")
	}
}

// TestPredefReferencesResolve checks that every name the scaffold points at is a
// name the scaffold also declares. The packaged Table binds its dataset through
// the ${DATASET} param, which resolves at consumption time, so placeholder
// values are skipped.
func TestPredefReferencesResolve(t *testing.T) {
	created, dest := renderPredef(t)

	names := map[string]bool{} // metadata.name of every declared document
	var refs []struct{ kind, name, from string }

	for _, rel := range created {
		if filepath.Ext(rel) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		dec := yamlPkg.NewDecoder(bytes.NewReader(body))
		for {
			var doc map[string]any
			derr := dec.Decode(&doc)
			if errors.Is(derr, io.EOF) {
				break
			}
			if derr != nil || doc == nil {
				break
			}
			if meta, ok := doc["metadata"].(map[string]any); ok {
				if name, ok := meta["name"].(string); ok {
					names[name] = true
				}
			}
			collectPredefRefs(doc, rel, &refs)
		}
	}

	for _, ref := range refs {
		if strings.Contains(ref.name, "${") {
			continue // param placeholder, bound by the consumer
		}
		if !names[ref.name] {
			t.Errorf("%s: %s reference %q resolves to no declared document", ref.from, ref.kind, ref.name)
		}
	}
}

// collectPredefRefs walks a decoded manifest and records the cross-document
// references the predef scaffold relies on. It is the standard test's collector
// plus "ref", the layout-child form the preview page uses.
func collectPredefRefs(node any, from string, out *[]struct{ kind, name, from string }) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "dataset", "selectedStyle", "page", "ref":
				if name, ok := child.(string); ok && name != "" {
					*out = append(*out, struct{ kind, name, from string }{key, name, from})
				}
			case "dependencies":
				if items, ok := child.([]any); ok {
					for _, item := range items {
						if name, ok := item.(string); ok && name != "" {
							*out = append(*out, struct{ kind, name, from string }{key, name, from})
						}
					}
				}
			}
			collectPredefRefs(child, from, out)
		}
	case []any:
		for _, item := range value {
			collectPredefRefs(item, from, out)
		}
	}
}
