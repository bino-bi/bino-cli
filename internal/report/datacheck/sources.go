package datacheck

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
)

// sources gives findings their place in the YAML: it reads each manifest
// file once and resolves dotted paths to lines.
type sources struct {
	byKey   map[string][]*config.Document
	text    map[string]string
	nodes   map[string][]*yaml.Node
	decoded map[*config.Document]map[string]any
}

func newSources(docs []config.Document) *sources {
	s := &sources{
		byKey:   map[string][]*config.Document{},
		text:    map[string]string{},
		nodes:   map[string][]*yaml.Node{},
		decoded: map[*config.Document]map[string]any{},
	}
	for i := range docs {
		d := &docs[i]
		s.byKey[d.Kind+":"+d.Name] = append(s.byKey[d.Kind+":"+d.Name], d)
	}
	return s
}

// doc returns the first document of a kind and name, or nil.
func (s *sources) doc(kind, name string) *config.Document {
	if list := s.byKey[kind+":"+name]; len(list) > 0 {
		return list[0]
	}
	return nil
}

func (s *sources) content(file string) string {
	if t, ok := s.text[file]; ok {
		return t
	}
	b, err := os.ReadFile(file) //nolint:gosec // G304: a manifest file the loader already read
	s.text[file] = string(b)
	if err != nil {
		s.text[file] = ""
	}
	return s.text[file]
}

// root returns the YAML node of a document, or nil when the file no longer
// matches what the loader read.
func (s *sources) root(doc *config.Document) *yaml.Node {
	nodes, ok := s.nodes[doc.File]
	if !ok {
		parsed, _ := spec.ParseYAMLNodes(s.content(doc.File)) //nolint:errcheck // a broken file yields no nodes
		nodes = spec.ManifestNodes(parsed)
		s.nodes[doc.File] = nodes
	}
	if doc.Position < 1 || doc.Position > len(nodes) {
		return nil
	}
	return nodes[doc.Position-1]
}

// json returns the decoded payload of a document.
func (s *sources) json(doc *config.Document) map[string]any {
	if m, ok := s.decoded[doc]; ok {
		return m
	}
	var m map[string]any
	_ = json.Unmarshal(doc.Raw, &m) //nolint:errcheck // a payload that does not decode has no fields
	s.decoded[doc] = m
	return m
}

// position resolves a dotted path in a document to its line and column.
// An unresolved path falls back to the first line of the document.
func (s *sources) position(doc *config.Document, path string) (line, col int) {
	root := s.root(doc)
	if root == nil {
		return 1, 1
	}
	if l, c, ok := spec.ResolvePathPosition(root, path); ok {
		return l, c
	}
	return root.Line, root.Column
}

// finding builds a finding located at path in doc.
func (s *sources) finding(doc *config.Document, rule, severity, path, msg string) lint.Finding {
	line, col := s.position(doc, path)
	return lint.Finding{
		RuleID:   rule,
		Severity: severity,
		Message:  msg,
		File:     doc.File,
		DocIdx:   doc.Position,
		Path:     path,
		Line:     line,
		Column:   col,
	}
}

// queryErrorFinding places a DataSet query error. An inline query maps
// DuckDB's "LINE n" to its YAML line like the language server does; other
// queries ($file, prql, source) point at their field.
func (s *sources) queryErrorFinding(doc config.Document, err error) lint.Finding {
	d := s.doc(doc.Kind, doc.Name)
	for _, cand := range s.byKey[doc.Kind+":"+doc.Name] {
		if cand.File == doc.File && cand.Position == doc.Position {
			d = cand
		}
	}
	msg, n, ok := spec.ParseDuckDBError(err.Error())
	if !ok {
		msg = strings.TrimSpace(err.Error())
	}
	msg = fmt.Sprintf("DataSet %q: %s", doc.Name, oneLine(msg))

	for _, od := range spec.Outline(s.content(d.File)) {
		if od.Position != d.Position || od.Kind != "DataSet" {
			continue
		}
		for _, b := range od.Blocks {
			if b.Field != "query" {
				continue
			}
			line := spec.QueryErrorLine(b, n)
			col := b.Content.StartCol
			if line != b.Content.StartLine {
				col = indentColumn(s.content(d.File), line)
			}
			return lint.Finding{
				RuleID: lint.RuleDatasetSQL, Severity: "error", Message: msg,
				File: d.File, DocIdx: d.Position, Path: "spec.query", Line: line, Column: col,
			}
		}
	}

	path := "spec"
	if sp, ok := s.json(d)["spec"].(map[string]any); ok {
		for _, field := range []string{"query", "prql", "source"} {
			if _, has := sp[field]; has {
				path = "spec." + field
				break
			}
		}
	}
	return s.finding(d, lint.RuleDatasetSQL, "error", path, msg)
}

// csvFinding places a CSV issue on the spec option it concerns, or on the
// path when the option is not written.
func (s *sources) csvFinding(doc config.Document, option, msg string) lint.Finding {
	d := s.doc(doc.Kind, doc.Name)
	for _, cand := range s.byKey[doc.Kind+":"+doc.Name] {
		if cand.File == doc.File && cand.Position == doc.Position {
			d = cand
		}
	}
	path := "spec.path"
	if sp, ok := s.json(d)["spec"].(map[string]any); ok && option != "" {
		if _, has := sp[option]; has {
			path = "spec." + option
		}
	}
	return s.finding(d, lint.RuleCSVFormat, "warning", path, fmt.Sprintf("DataSource %q: %s", doc.Name, msg))
}

// oneLine joins the lines of a DuckDB error, so a finding stays on one line.
func oneLine(s string) string {
	var parts []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " ")
}

// indentColumn returns the 1-based column of the first non-blank character of a line.
func indentColumn(text string, line int) int {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return 1
	}
	l := lines[line-1]
	return len([]rune(l)) - len([]rune(strings.TrimLeft(l, " \t"))) + 1
}
