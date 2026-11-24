package lsp

import (
	"context"
	"errors"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func documentsRequest(t *testing.T, s *Server) DocumentsResult {
	t.Helper()
	res, err := s.Request(context.Background(), methodDocuments,
		protocol.LSPAny(`{"textDocument":{"uri":"file:///proj/report.yaml"}}`))
	if err != nil {
		t.Fatalf("bino/documents: %v", err)
	}
	return res.(DocumentsResult)
}

func TestDocumentsRequestUsesBuffer(t *testing.T) {
	s := newTestServer()
	openDoc(t, s, `# header comment
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales-by-region
spec:
  prql: |-
    from sales
    ---
    select {region}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: foreign
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: my-table
spec:
  dataset: sales-by-region
`)
	res := documentsRequest(t, s)
	if !res.IsBino || res.Version != 1 {
		t.Fatalf("isBino/version = %v/%d", res.IsBino, res.Version)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("got %d documents, want 2 (the ConfigMap is not bino): %+v", len(res.Documents), res.Documents)
	}
	ds := res.Documents[0]
	if ds.Kind != "DataSet" || ds.Name != "sales-by-region" || ds.Position != 1 || ds.Range.Start.Line != 1 {
		t.Errorf("dataset = %+v", ds)
	}
	if len(ds.Blocks) != 1 {
		t.Fatalf("blocks = %+v", ds.Blocks)
	}
	b := ds.Blocks[0]
	wantRange := protocol.Range{Start: protocol.Position{Line: 7, Character: 4}, End: protocol.Position{Line: 9, Character: 19}}
	if b.Field != "prql" || b.Language != "prql" || b.Range != wantRange {
		t.Errorf("block = %+v, want prql at %+v", b, wantRange)
	}
	// The ConfigMap is document 2 of the file, so the Table is 3.
	tbl := res.Documents[1]
	if tbl.Kind != "Table" || tbl.Name != "my-table" || tbl.Position != 3 || tbl.Range.Start.Line != 16 {
		t.Errorf("table = %+v", tbl)
	}
}

func TestDocumentsRequestForeignBuffer(t *testing.T) {
	s := newTestServer()
	openDoc(t, s, "services:\n  web:\n    image: nginx\n")
	res := documentsRequest(t, s)
	if res.IsBino || len(res.Documents) != 0 {
		t.Fatalf("foreign buffer = %+v", res)
	}
}

func TestDocumentsRequestNeedsTextDocument(t *testing.T) {
	s := newTestServer()
	if _, err := s.Request(context.Background(), methodDocuments, protocol.LSPAny(`{}`)); err == nil {
		t.Fatal("want an error without textDocument")
	}
}

func TestIndexRequest(t *testing.T) {
	s := newTestServer()
	res, err := s.Request(context.Background(), methodIndex, protocol.LSPAny(`null`))
	if err != nil {
		t.Fatal(err)
	}
	idx := res.(IndexResult)
	if len(idx.Documents) != 2 || idx.Documents[0] != (IndexEntry{Kind: "DataSet", Name: "sales", File: "data.yaml", Position: 1}) {
		t.Fatalf("index = %+v", idx)
	}
}

func TestUnknownRequest(t *testing.T) {
	s := newTestServer()
	if _, err := s.Request(context.Background(), "bino/nope", nil); !errors.Is(err, jsonrpc2.ErrMethodNotFound) {
		t.Fatalf("err = %v, want method not found", err)
	}
}

func TestWantsIndexNotifications(t *testing.T) {
	for in, want := range map[string]bool{
		``:                      false,
		`null`:                  false,
		`{}`:                    false,
		`{"indexChanged":true}`: true,
		`"x"`:                   false,
	} {
		if got := wantsIndexNotifications(protocol.LSPAny(in)); got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}
