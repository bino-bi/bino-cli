package lsp

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
)

const composeDoc = `version: "3"
services:
  web:
    image: nginx
    ports:
      - "80:80"
`

const workflowDoc = `name: CI
on:
  push:
jobs:
  test:
    runs-on: ubuntu-latest
`

func TestIsBinoBuffer(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", true},
		{"whitespace", "  \n\t\n", true},
		{"comment only", "# notes\n", true},
		{"kind being typed", "kind: ", true},
		{"key being typed", "ap", true},
		{"apiVersion being typed", "apiVersion: bin", true},
		{"bino apiVersion", "apiVersion: bino.bi/v1alpha1\nkind: Whatever\n", true},
		{"known kind without apiVersion", "kind: Table\nspec:\n  x: 1\n", true},
		{"bino doc after a foreign one", composeDoc + "---\napiVersion: bino.bi/v1alpha1\n", true},
		{"broken bino yaml", "apiVersion: bino.bi/v1alpha1\nspec: [\n", true},
		{"docker-compose", composeDoc, false},
		{"github workflow", workflowDoc, false},
		{"k8s", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n", false},
		{"root sequence", "- hosts: all\n", false},
	} {
		if got := isBinoBuffer(tt.text); got != tt.want {
			t.Errorf("%s: isBinoBuffer = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCompletion_NothingOutsideBino(t *testing.T) {
	s := newRealSchemaServer(t)
	for _, tt := range []struct {
		name       string
		doc        string
		line, char uint32
	}{
		{"compose after colon", composeDoc, 1, 9},
		{"compose nested key", composeDoc, 3, 4},
		{"compose new root key", composeDoc + "\n", 6, 0},
		{"workflow", workflowDoc, 5, 12},
	} {
		u := openDoc(t, s, tt.doc)
		res, err := s.Completion(context.Background(), completionParams(u, tt.line, tt.char))
		if err != nil {
			t.Fatal(err)
		}
		if labels := completionLabels(t, res); len(labels) != 0 {
			t.Errorf("%s: completion offered %v, want nothing", tt.name, labels)
		}
	}
}

func TestCompletion_BinoAndEmptyBuffers(t *testing.T) {
	s := newRealSchemaServer(t)
	u := openDoc(t, s, "apiVersion: bino.bi/v1alpha1\nkind: Table\nmetadata:\n  name: t\nspec:\n  \n")
	res, err := s.Completion(context.Background(), completionParams(u, 5, 2))
	if err != nil {
		t.Fatal(err)
	}
	if labels := completionLabels(t, res); !contains(labels, "dataset") {
		t.Errorf("bino doc: spec completion missing dataset (got %v)", labels)
	}
	for _, text := range []string{"", "   \n\n"} {
		u := openDoc(t, s, text)
		res, err := s.Completion(context.Background(), completionParams(u, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		if labels := completionLabels(t, res); !contains(labels, "Table manifest") || !contains(labels, "kind") {
			t.Errorf("empty buffer %q: want kind scaffolds and root keys (got %v)", text, labels)
		}
	}
}

func TestHover_NothingOutsideBino(t *testing.T) {
	s := newRealSchemaServer(t)
	// `metadata:` and `kind:` are bino keys with hover text; in a k8s file they must stay silent.
	u := openDoc(t, s, "apiVersion: apps/v1\nkind: Table\nmetadata:\n  name: web\n")
	for _, pos := range []protocol.Position{{Line: 1, Character: 7}, {Line: 2, Character: 2}} {
		h, err := s.Hover(context.Background(), hoverParams(u, pos.Line, pos.Character))
		if err != nil {
			t.Fatal(err)
		}
		if h != nil {
			t.Errorf("hover at %v in a k8s doc = %+v, want nil", pos, h.Contents)
		}
	}
	u = openDoc(t, s, "kind: Table\nmetadata:\n  name: t\n")
	if h, _ := s.Hover(context.Background(), hoverParams(u, 0, 7)); h == nil {
		t.Error("hover on a bino kind returned nil")
	}
}

func TestNavigation_NothingOutsideBino(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  dataset: sales\n")
	def, _ := s.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: 5, Character: 12},
		},
	})
	if def != nil {
		t.Errorf("definition in a k8s doc = %v, want nil", def)
	}
	syms, _ := s.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	})
	if syms != nil {
		t.Errorf("document symbols in a k8s doc = %v, want nil", syms)
	}
}
