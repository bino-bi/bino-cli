package lsp

import (
	"context"
	"encoding/json"
	"os"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
)

// paramsForTarget returns the metadata.params declared by the (kind, name)
// document, resolved through the name index so unsaved buffers win. Targets
// that are not param-capable or fail to decode (schema-invalid) yield ok=false
// so completion stays silent and the ref-params lint remains the signal.
func (s *Server) paramsForTarget(ctx context.Context, kind, name string) ([]config.LayoutPageParamSpec, bool) {
	if kind == "" || name == "" {
		return nil, false
	}
	if _, capable := config.ParamCapableKinds[kind]; !capable {
		return nil, false
	}
	def, ok := s.getNameIndex(ctx).Definition(kind, name)
	if !ok {
		return nil, false
	}
	content, ok := s.fileContent(def.File)
	if !ok {
		return nil, false
	}
	nodes, err := reportspec.ParseYAMLNodes(content)
	if err != nil || def.DocIndex >= len(nodes) || nodes[def.DocIndex] == nil {
		return nil, false
	}
	return decodeDocParams(nodes[def.DocIndex])
}

// fileContent returns a file's content, preferring an open buffer over disk.
func (s *Server) fileContent(path string) (string, bool) {
	for _, d := range s.docs.All() {
		if d.Path == path {
			return d.Text, true
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// decodeDocParams extracts metadata.params from a parsed document via the same
// YAML→JSON round-trip the loader uses, so type handling stays identical.
func decodeDocParams(root *yaml.Node) ([]config.LayoutPageParamSpec, bool) {
	var raw any
	if err := root.Decode(&raw); err != nil {
		return nil, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var doc struct {
		Metadata struct {
			Params []config.LayoutPageParamSpec `json:"params"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	return doc.Metadata.Params, true
}
