package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// makeDocAt is makeDoc with an explicit manifest file path — DocumentArtefact
// tests need a real directory because markdown sources resolve relative to it.
func makeDocAt(kind, name, file string, raw json.RawMessage) config.Document {
	doc := makeDoc(kind, name, raw)
	doc.File = file
	return doc
}

// docArtefactDoc builds a DocumentArtefact manifest document rooted at dir.
func docArtefactDoc(t *testing.T, name, dir string, sources []string) config.Document {
	t.Helper()
	srcJSON, err := json.Marshal(sources)
	if err != nil {
		t.Fatalf("marshal sources: %v", err)
	}
	raw := fmt.Sprintf(`{
		"apiVersion": "bino.bi/v1",
		"kind": "DocumentArtefact",
		"metadata": {"name": %q},
		"spec": {"format": "a4", "sources": %s}
	}`, name, srcJSON)
	return makeDocAt("DocumentArtefact", name, filepath.Join(dir, name+".yaml"), json.RawMessage(raw))
}

// tempDirResolved returns a symlink-resolved temp dir so node File values
// compare exactly against watcher-style absolute paths (macOS /tmp symlink).
func tempDirResolved(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestBuildDocumentArtefactSources proves markdown sources resolve to
// concrete file nodes whose File values match watcher paths exactly.
// Before the fix the builder stored the raw source string, so NodesByFile
// never matched and every markdown edit forced a full rebuild.
func TestBuildDocumentArtefactSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("plain relative source resolves to absolute file node", func(t *testing.T) {
		t.Parallel()
		dir := tempDirResolved(t)
		mdPath := filepath.Join(dir, "notes.md")
		writeFile(t, mdPath, "# One\n")

		g, err := Build(ctx, []config.Document{docArtefactDoc(t, "handbook", dir, []string{"notes.md"})})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}

		nodes := g.NodesByFile(map[string]struct{}{mdPath: {}})
		if len(nodes) != 1 {
			t.Fatalf("NodesByFile(%s) = %d nodes, want 1", mdPath, len(nodes))
		}
		if nodes[0].Kind != NodeMarkdownFile {
			t.Errorf("node kind = %s, want MarkdownFile", nodes[0].Kind)
		}
		docNode, ok := g.NodeByID(makeNodeID(NodeDocumentArtefact, "handbook"))
		if !ok {
			t.Fatal("document artefact node missing")
		}
		if !slices.Contains(docNode.DependsOn, nodes[0].ID) {
			t.Errorf("doc node deps %v missing markdown node %s", docNode.DependsOn, nodes[0].ID)
		}
	})

	t.Run("glob source resolves to one node per matched file", func(t *testing.T) {
		t.Parallel()
		dir := tempDirResolved(t)
		writeFile(t, filepath.Join(dir, "docs", "01_intro.md"), "# Intro\n")
		writeFile(t, filepath.Join(dir, "docs", "02_body.md"), "# Body\n")

		g, err := Build(ctx, []config.Document{docArtefactDoc(t, "handbook", dir, []string{"docs/*.md"})})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}

		docNode, _ := g.NodeByID(makeNodeID(NodeDocumentArtefact, "handbook"))
		if len(docNode.DependsOn) != 2 {
			t.Fatalf("doc deps = %v, want 2 markdown file nodes", docNode.DependsOn)
		}
		for _, f := range []string{filepath.Join(dir, "docs", "01_intro.md"), filepath.Join(dir, "docs", "02_body.md")} {
			if len(g.NodesByFile(map[string]struct{}{f: {}})) != 1 {
				t.Errorf("no node matches resolved file %s", f)
			}
		}
	})

	t.Run("unresolvable source degrades to fallback node without Build error", func(t *testing.T) {
		t.Parallel()
		dir := tempDirResolved(t)

		g, err := Build(ctx, []config.Document{docArtefactDoc(t, "handbook", dir, []string{"missing/*.md"})})
		if err != nil {
			t.Fatalf("Build must not fail on unresolvable sources, got: %v", err)
		}

		id := makeNodeID(NodeMarkdownFile, filepath.Join(dir, "missing/*.md"))
		node, ok := g.NodeByID(id)
		if !ok {
			t.Fatalf("fallback node %s missing", id)
		}
		if node.Attributes["unresolved"] != "true" {
			t.Errorf("fallback node not marked unresolved: %v", node.Attributes)
		}
	})

	t.Run("same source string in sibling dirs stays distinct", func(t *testing.T) {
		t.Parallel()
		dir := tempDirResolved(t)
		dirA := filepath.Join(dir, "a")
		dirB := filepath.Join(dir, "b")
		writeFile(t, filepath.Join(dirA, "notes.md"), "# A\n")
		writeFile(t, filepath.Join(dirB, "notes.md"), "# B\n")

		g, err := Build(ctx, []config.Document{
			docArtefactDoc(t, "doc_a", dirA, []string{"notes.md"}),
			docArtefactDoc(t, "doc_b", dirB, []string{"notes.md"}),
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}

		// Editing a/notes.md must affect only doc_a. Before the fix both
		// artefacts collapsed onto one "notes.md" node.
		seeds := g.NodesByFile(map[string]struct{}{filepath.Join(dirA, "notes.md"): {}})
		if len(seeds) != 1 {
			t.Fatalf("expected 1 seed for a/notes.md, got %d", len(seeds))
		}
		_, docs := g.AffectedArtefacts(seeds)
		if !slices.Equal(docs, []string{"doc_a"}) {
			t.Errorf("affected docs = %v, want [doc_a]", docs)
		}
	})

	t.Run("shared file is one node depended on by both artefacts", func(t *testing.T) {
		t.Parallel()
		dir := tempDirResolved(t)
		writeFile(t, filepath.Join(dir, "shared.md"), "# Shared\n")

		g, err := Build(ctx, []config.Document{
			docArtefactDoc(t, "doc_a", dir, []string{"shared.md"}),
			docArtefactDoc(t, "doc_b", dir, []string{"shared.md"}),
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}

		seeds := g.NodesByFile(map[string]struct{}{filepath.Join(dir, "shared.md"): {}})
		if len(seeds) != 1 {
			t.Fatalf("expected exactly 1 shared node, got %d", len(seeds))
		}
		_, docs := g.AffectedArtefacts(seeds)
		if !slices.Equal(docs, []string{"doc_a", "doc_b"}) {
			t.Errorf("affected docs = %v, want both artefacts", docs)
		}
	})
}

// TestBuildDocumentArtefactRefEdges proves :ref[Kind:name] references in
// markdown sources become dependencies of the markdown file node — the edge
// that lets component and data edits propagate to embedding documents.
func TestBuildDocumentArtefactRefEdges(t *testing.T) {
	t.Parallel()
	dir := tempDirResolved(t)
	writeFile(t, filepath.Join(dir, "notes.md"),
		":ref[ChartTime:chart]\n:ref[Grid:kpis]{caption=\"KPIs\"}\n:ref[LayoutCard:card]\n:ref[Table:missing]\n")

	docs := []config.Document{
		makeDoc("ChartTime", "chart", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"chart"},"spec":{"dataset":"sales"}}`)),
		makeDoc("Grid", "kpis", json.RawMessage(`{"kind":"Grid","metadata":{"name":"kpis"},"spec":{}}`)),
		makeDoc("LayoutCard", "card", json.RawMessage(`{"kind":"LayoutCard","metadata":{"name":"card"},"spec":{"children":[]}}`)),
		docArtefactDoc(t, "handbook", dir, []string{"notes.md"}),
	}

	g, err := Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mdNodes := g.NodesByFile(map[string]struct{}{filepath.Join(dir, "notes.md"): {}})
	if len(mdNodes) != 1 {
		t.Fatalf("markdown node not found")
	}
	deps := mdNodes[0].DependsOn

	for _, want := range []string{
		makeNodeID(NodeComponent, "chart"),
		makeNodeID(NodeComponent, "kpis"),
		makeNodeID(NodeLayoutCard, "card"),
	} {
		if !slices.Contains(deps, want) {
			t.Errorf("markdown node deps %v missing %s", deps, want)
		}
	}
	if slices.Contains(deps, makeNodeID(NodeComponent, "missing")) {
		t.Errorf("unresolvable ref must not create an edge: %v", deps)
	}
}

// TestAffectedArtefactsFromMarkdownAndComponentEdits walks the full chain a
// preview refresh relies on: an edit to a dataset feeding an embedded chart
// must mark the embedding document affected (dataset → chart → markdown →
// document), and a markdown edit must mark only its document.
func TestAffectedArtefactsFromMarkdownAndComponentEdits(t *testing.T) {
	t.Parallel()
	dir := tempDirResolved(t)
	mdPath := filepath.Join(dir, "notes.md")
	writeFile(t, mdPath, "# Doc\n\n:ref[ChartTime:chart]\n")

	docs := []config.Document{
		makeDoc("DataSource", "ppl", json.RawMessage(`{"kind":"DataSource","metadata":{"name":"ppl"},"spec":{"type":"inline","content":[{"a":1}]}}`)),
		makeDoc("DataSet", "sales", json.RawMessage(`{"kind":"DataSet","metadata":{"name":"sales"},"spec":{"query":"SELECT * FROM ppl","dependencies":["ppl"]}}`)),
		makeDoc("ChartTime", "chart", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"chart"},"spec":{"dataset":"sales"}}`)),
		docArtefactDoc(t, "handbook", dir, []string{"notes.md"}),
	}

	g, err := Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tests := []struct {
		name     string
		seedID   string
		wantDocs []string
	}{
		{"markdown edit affects its document", makeNodeID(NodeMarkdownFile, mdPath), []string{"handbook"}},
		{"dataset edit reaches the document through the embedded chart", makeNodeID(NodeDataSet, "sales"), []string{"handbook"}},
		{"datasource edit reaches the document too", makeNodeID(NodeDataSource, "ppl"), []string{"handbook"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, ok := g.NodeByID(tt.seedID)
			if !ok {
				t.Fatalf("seed node %s not found", tt.seedID)
			}
			_, docsAffected := g.AffectedArtefacts([]*Node{node})
			if !slices.Equal(docsAffected, tt.wantDocs) {
				t.Errorf("affected docs = %v, want %v", docsAffected, tt.wantDocs)
			}
		})
	}
}

// TestStandaloneComponentDatasetEdge proves a standalone component document
// depends on the dataset its spec binds. extractDatasets only understood the
// bare spec fragment layout children pass, so standalone nodes (built from
// the full manifest) carried no data edges — the link markdown :ref edges
// rely on to propagate data edits into documents.
func TestStandaloneComponentDatasetEdge(t *testing.T) {
	t.Parallel()

	docs := []config.Document{
		makeDoc("DataSet", "sales", json.RawMessage(`{"kind":"DataSet","metadata":{"name":"sales"},"spec":{"query":"SELECT 1"}}`)),
		makeDoc("ChartTime", "chart", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"chart"},"spec":{"dataset":"sales"}}`)),
	}

	g, err := Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node, ok := g.NodeByID(makeNodeID(NodeComponent, "chart"))
	if !ok {
		t.Fatal("standalone component node missing")
	}
	if !slices.Contains(node.DependsOn, makeNodeID(NodeDataSet, "sales")) {
		t.Errorf("standalone component deps = %v, want DataSet:sales edge", node.DependsOn)
	}
}

// TestDataSetSourceAliasEdge proves a spec.source pass-through dataset
// depends on its aliased DataSource. Before the fix the alias was recorded
// as attributes only, so DataSource edits never reached dependent artefacts.
func TestDataSetSourceAliasEdge(t *testing.T) {
	t.Parallel()

	docs := []config.Document{
		makeDoc("DataSource", "ppl", json.RawMessage(`{"kind":"DataSource","metadata":{"name":"ppl"},"spec":{"type":"inline","content":[{"a":1}]}}`)),
		makeDoc("DataSet", "alias_ds", json.RawMessage(`{"kind":"DataSet","metadata":{"name":"alias_ds"},"spec":{"source":"ppl"}}`)),
	}

	g, err := Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node, ok := g.NodeByID(makeNodeID(NodeDataSet, "alias_ds"))
	if !ok {
		t.Fatal("alias dataset node missing")
	}
	if !slices.Contains(node.DependsOn, makeNodeID(NodeDataSource, "ppl")) {
		t.Errorf("alias dataset deps = %v, want DataSource:ppl edge", node.DependsOn)
	}
}
