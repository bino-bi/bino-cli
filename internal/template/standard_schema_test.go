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

// TestStandardRendersValidManifests is the regression gate for the scaffold's
// content: every YAML document the standard template produces must validate
// against the embedded schema. Rendering first is the point — the sources hold
// template actions, so only the output can be validated, and that is also what
// the user actually gets.
//
// It mirrors TestValidate_SampleBundle in internal/schema, which gates the
// shipped sample bundle the same way.
func TestStandardRendersValidManifests(t *testing.T) {
	created, dest := renderStandard(t)

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
		t.Fatal("no manifests validated; the standard template rendered no YAML")
	}
}

// TestStandardReferencesResolve checks the wiring the schema cannot: that every
// name the scaffold points at is a name the scaffold also declares. A dangling
// dataset or page reference renders a blank report rather than an error, so it
// has to be caught here.
func TestStandardReferencesResolve(t *testing.T) {
	created, dest := renderStandard(t)

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
			collectRefs(doc, rel, &refs)
		}
	}

	for _, ref := range refs {
		// A "$name" binding targets a DataSource; the bare form targets a DataSet.
		// Both resolve against the same declared-name set here.
		target := strings.TrimPrefix(ref.name, "$")
		if !names[target] {
			t.Errorf("%s: %s reference %q resolves to no declared document", ref.from, ref.kind, ref.name)
		}
	}
}

// collectRefs walks a decoded manifest and records the cross-document references
// the scaffold relies on.
func collectRefs(node any, from string, out *[]struct{ kind, name, from string }) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "dataset", "selectedStyle", "artefact":
				if name, ok := child.(string); ok && name != "" {
					*out = append(*out, struct{ kind, name, from string }{key, name, from})
				}
			case "page":
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
			collectRefs(child, from, out)
		}
	case []any:
		for _, item := range value {
			collectRefs(item, from, out)
		}
	}
}
