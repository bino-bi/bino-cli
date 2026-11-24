package spec

import "testing"

const outlineSrc = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales-by-region
spec:
  prql: |-
    from sales
    ---
    select {region}

---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: folded
spec:
  query: >
    SELECT *
    FROM t
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: src
spec:
  type: csv
  query: SELECT 1
`

func TestOutline(t *testing.T) {
	docs := Outline(outlineSrc)
	if len(docs) != 3 {
		t.Fatalf("got %d docs, want 3: %+v", len(docs), docs)
	}
	d := docs[0]
	if d.Kind != "DataSet" || d.Name != "sales-by-region" || d.Position != 1 {
		t.Fatalf("doc 1 = %+v", d)
	}
	if d.Range.StartLine != 1 || d.Range.EndLine != 11 {
		t.Errorf("doc 1 range = %+v, want lines 1..11", d.Range)
	}
	if len(d.Blocks) != 1 {
		t.Fatalf("doc 1 blocks = %+v", d.Blocks)
	}
	// The indented --- is block content, and the trailing blank line is not.
	want := Range{StartLine: 7, StartCol: 5, EndLine: 9, EndCol: 20}
	if b := d.Blocks[0]; b.Field != "prql" || b.Content != want {
		t.Errorf("prql block = %+v, want %+v", b, want)
	}

	d = docs[1]
	if d.Name != "folded" || d.Position != 2 || d.Range.StartLine != 12 {
		t.Fatalf("doc 2 = %+v", d)
	}
	want = Range{StartLine: 18, StartCol: 5, EndLine: 19, EndCol: 11}
	if len(d.Blocks) != 1 || d.Blocks[0].Field != "query" || d.Blocks[0].Content != want {
		t.Errorf("folded query block = %+v, want %+v", d.Blocks, want)
	}

	// Only DataSet queries are blocks.
	if d = docs[2]; d.Kind != "DataSource" || d.Position != 3 || len(d.Blocks) != 0 {
		t.Errorf("doc 3 = %+v", d)
	}
}

func TestOutlineInlineQuery(t *testing.T) {
	docs := Outline("kind: DataSet\nmetadata:\n  name: a\nspec:\n  prql: from x\n")
	want := Range{StartLine: 5, StartCol: 9, EndLine: 5, EndCol: 15}
	if len(docs) != 1 || len(docs[0].Blocks) != 1 || docs[0].Blocks[0].Content != want {
		t.Fatalf("got %+v, want one block at %+v", docs, want)
	}
}

func TestOutlineSkipsEmptyDocsInPosition(t *testing.T) {
	docs := Outline("---\n# only a comment\n---\nkind: DataSet\nmetadata:\n  name: a\n")
	if len(docs) != 1 || docs[0].Position != 1 || docs[0].Range.StartLine != 4 {
		t.Fatalf("got %+v", docs)
	}
}

func TestOutlineBrokenDocumentKeepsOthers(t *testing.T) {
	src := "kind: DataSet\nmetadata:\n  name: a\n---\nkind: [unclosed\n---\nkind: DataSource\nmetadata:\n  name: b\n"
	docs := Outline(src)
	if len(docs) != 2 || docs[0].Name != "a" || docs[1].Name != "b" || docs[1].Range.StartLine != 7 {
		t.Fatalf("got %+v", docs)
	}
}
