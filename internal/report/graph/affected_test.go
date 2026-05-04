package graph

import (
	"slices"
	"testing"
)

// buildTestGraph hand-constructs a small dependency graph that mirrors the
// shape of a real bino bundle: DataSource → DataSet → Component → LayoutPage
// → ReportArtefact, plus a DocumentArtefact on a separate DataSet branch.
// Files are deliberately reused across multiple nodes so file-based lookups
// can be exercised.
//
//	/proj/sources.yaml           : DataSource ds1, DataSource ds2
//	/proj/datasets.yaml          : DataSet set1 (deps ds1), DataSet set2 (deps ds2)
//	/proj/components/chart.yaml  : Component chart1 (deps set1)
//	/proj/components/table.yaml  : Component table1 (deps set2)
//	/proj/pages/main.yaml        : LayoutPage page1 (deps chart1, table1)
//	/proj/artefacts.yaml         : ReportArtefact art1 (deps page1)
//	/proj/docs.yaml              : DocumentArtefact doc1 (deps set2)
func buildTestGraph() *Graph {
	nodes := map[string]*Node{
		"DataSource:ds1":   {ID: "DataSource:ds1", Kind: NodeDataSource, Name: "ds1", File: "/proj/sources.yaml"},
		"DataSource:ds2":   {ID: "DataSource:ds2", Kind: NodeDataSource, Name: "ds2", File: "/proj/sources.yaml"},
		"DataSet:set1":     {ID: "DataSet:set1", Kind: NodeDataSet, Name: "set1", File: "/proj/datasets.yaml", DependsOn: []string{"DataSource:ds1"}},
		"DataSet:set2":     {ID: "DataSet:set2", Kind: NodeDataSet, Name: "set2", File: "/proj/datasets.yaml", DependsOn: []string{"DataSource:ds2"}},
		"Component:chart1": {ID: "Component:chart1", Kind: NodeComponent, Name: "chart1", File: "/proj/components/chart.yaml", DependsOn: []string{"DataSet:set1"}},
		"Component:table1": {ID: "Component:table1", Kind: NodeComponent, Name: "table1", File: "/proj/components/table.yaml", DependsOn: []string{"DataSet:set2"}},
		"LayoutPage:page1": {ID: "LayoutPage:page1", Kind: NodeLayoutPage, Name: "page1", File: "/proj/pages/main.yaml", DependsOn: []string{"Component:chart1", "Component:table1"}},
		"ReportArtefact:art1": {
			ID: "ReportArtefact:art1", Kind: NodeReportArtefact, Name: "art1",
			File: "/proj/artefacts.yaml", DependsOn: []string{"LayoutPage:page1"},
		},
		"DocumentArtefact:doc1": {
			ID: "DocumentArtefact:doc1", Kind: NodeDocumentArtefact, Name: "doc1",
			File: "/proj/docs.yaml", DependsOn: []string{"DataSet:set2"},
		},
	}
	g := &Graph{
		Nodes:                 nodes,
		artefactIndex:         map[string]*Node{"art1": nodes["ReportArtefact:art1"]},
		documentArtefactIndex: map[string]*Node{"doc1": nodes["DocumentArtefact:doc1"]},
	}
	g.ReportArtefacts = []*Node{nodes["ReportArtefact:art1"]}
	g.DocumentArtefacts = []*Node{nodes["DocumentArtefact:doc1"]}
	return g
}

func TestNodesByFile(t *testing.T) {
	t.Parallel()
	g := buildTestGraph()

	t.Run("matches single file with multiple docs", func(t *testing.T) {
		got := g.NodesByFile(map[string]struct{}{"/proj/sources.yaml": {}})
		names := nodeNames(got)
		want := []string{"ds1", "ds2"}
		slices.Sort(names)
		slices.Sort(want)
		if !slices.Equal(names, want) {
			t.Errorf("got %v, want %v", names, want)
		}
	})

	t.Run("matches multiple files", func(t *testing.T) {
		got := g.NodesByFile(map[string]struct{}{
			"/proj/components/chart.yaml": {},
			"/proj/components/table.yaml": {},
		})
		if len(got) != 2 {
			t.Errorf("got %d nodes, want 2", len(got))
		}
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		got := g.NodesByFile(map[string]struct{}{"/proj/nonexistent.yaml": {}})
		if len(got) != 0 {
			t.Errorf("got %d nodes, want 0", len(got))
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		got := g.NodesByFile(nil)
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("nil graph returns nil", func(t *testing.T) {
		var g *Graph
		if got := g.NodesByFile(map[string]struct{}{"/x": {}}); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestAffectedArtefacts(t *testing.T) {
	t.Parallel()
	g := buildTestGraph()

	tests := []struct {
		name        string
		seedFiles   []string
		wantReports []string
		wantDocs    []string
	}{
		{
			name:        "leaf component change affects only its artefact",
			seedFiles:   []string{"/proj/components/chart.yaml"},
			wantReports: []string{"art1"},
			wantDocs:    nil,
		},
		{
			name:        "datasource shared across branches reaches all dependents",
			seedFiles:   []string{"/proj/sources.yaml"},
			wantReports: []string{"art1"},
			wantDocs:    []string{"doc1"},
		},
		{
			name:        "dataset on the document branch reaches only the document",
			seedFiles:   []string{"/proj/datasets.yaml"},
			wantReports: []string{"art1"},
			wantDocs:    []string{"doc1"},
		},
		{
			name:        "layout page change reaches its artefact",
			seedFiles:   []string{"/proj/pages/main.yaml"},
			wantReports: []string{"art1"},
			wantDocs:    nil,
		},
		{
			name:        "artefact file change includes the artefact itself",
			seedFiles:   []string{"/proj/artefacts.yaml"},
			wantReports: []string{"art1"},
			wantDocs:    nil,
		},
		{
			name:        "document artefact file change includes the doc itself",
			seedFiles:   []string{"/proj/docs.yaml"},
			wantReports: nil,
			wantDocs:    []string{"doc1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileSet := map[string]struct{}{}
			for _, f := range tt.seedFiles {
				fileSet[f] = struct{}{}
			}
			seeds := g.NodesByFile(fileSet)
			reports, docs := g.AffectedArtefacts(seeds)
			if !slices.Equal(reports, tt.wantReports) {
				t.Errorf("reports = %v, want %v", reports, tt.wantReports)
			}
			if !slices.Equal(docs, tt.wantDocs) {
				t.Errorf("docs = %v, want %v", docs, tt.wantDocs)
			}
		})
	}
}

func TestAffectedArtefacts_EdgeCases(t *testing.T) {
	t.Parallel()
	g := buildTestGraph()

	t.Run("empty seeds returns nil", func(t *testing.T) {
		reports, docs := g.AffectedArtefacts(nil)
		if reports != nil || docs != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", reports, docs)
		}
	})

	t.Run("nil seeds in slice are skipped", func(t *testing.T) {
		seed := g.Nodes["DataSource:ds2"]
		reports, docs := g.AffectedArtefacts([]*Node{nil, seed, nil})
		if !slices.Equal(reports, []string{"art1"}) {
			t.Errorf("reports = %v, want [art1]", reports)
		}
		if !slices.Equal(docs, []string{"doc1"}) {
			t.Errorf("docs = %v, want [doc1]", docs)
		}
	})

	t.Run("nil graph returns nil", func(t *testing.T) {
		var g *Graph
		seed := &Node{ID: "x"}
		reports, docs := g.AffectedArtefacts([]*Node{seed})
		if reports != nil || docs != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", reports, docs)
		}
	})

	t.Run("dedupes when multiple seeds reach the same artefact", func(t *testing.T) {
		seeds := []*Node{
			g.Nodes["DataSource:ds1"],
			g.Nodes["Component:chart1"],
			g.Nodes["LayoutPage:page1"],
		}
		reports, _ := g.AffectedArtefacts(seeds)
		if !slices.Equal(reports, []string{"art1"}) {
			t.Errorf("reports = %v, want [art1] (deduped)", reports)
		}
	})
}

func nodeNames(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}
