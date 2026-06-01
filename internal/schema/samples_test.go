package schema

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	yamlPkg "gopkg.in/yaml.v3"
)

// TestValidate_SampleBundle is a regression gate: every document in the shipped
// sample bundle must validate against the embedded schema. Each YAML file holds
// multiple documents separated by '---'; each is validated independently.
func TestValidate_SampleBundle(t *testing.T) {
	files, err := filepath.Glob("../../docs/samples/sales-dashboard/*.yaml")
	if err != nil {
		t.Fatalf("glob samples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no sample files found")
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		dec := yamlPkg.NewDecoder(bytes.NewReader(data))
		idx := 0
		for {
			var doc any
			derr := dec.Decode(&doc)
			if errors.Is(derr, io.EOF) {
				break
			}
			if derr != nil {
				t.Errorf("%s: decode document %d: %v", filepath.Base(file), idx, derr)
				break
			}
			idx++
			if doc == nil {
				continue // empty document between separators
			}

			docBytes, merr := yamlPkg.Marshal(doc)
			if merr != nil {
				t.Errorf("%s: marshal document %d: %v", filepath.Base(file), idx, merr)
				continue
			}

			name := filepath.Base(file)
			docIdx := idx
			t.Run(name, func(t *testing.T) {
				if verr := Validate(docBytes); verr != nil {
					t.Errorf("%s document %d failed validation:\n%v", name, docIdx, verr)
				}
			})
		}
	}
}
