package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	reportgraph "bino.bi/bino/internal/report/graph"
)

// columnsGraph builds a report whose page holds a Table bound to two
// datasets and a Text that reads a dataset only through its template.
func columnsGraph(t *testing.T) (*reportgraph.Graph, *reportgraph.Node) {
	t.Helper()
	doc := func(kind, name, spec string) config.Document {
		return config.Document{Kind: kind, Name: name, File: "report.yaml", Raw: []byte(
			`{"apiVersion":"bino.bi/v1","kind":"` + kind + `","metadata":{"name":"` + name + `"},"spec":` + spec + `}`)}
	}
	docs := []config.Document{
		doc("DataSet", "sales", `{"query":"SELECT 1"}`),
		doc("DataSet", "plan", `{"query":"SELECT 1"}`),
		doc("LayoutPage", "page", `{"children":[
			{"kind":"Table","spec":{"dataset":["sales","plan"],"scenarios":["ac1","pp1"],"filter":"region = 'EMEA'"}},
			{"kind":"Text","spec":{"value":"Revenue ${data.kpi[0].ac1}"}}
		]}`),
		doc("ReportArtefact", "report", `{"layoutPages":["page"]}`),
	}
	g, err := reportgraph.Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	root, ok := g.ReportArtefactByName("report")
	if !ok {
		t.Fatal("report artefact node missing")
	}
	return g, root
}

func TestPrintGraphFlatColumns(t *testing.T) {
	g, root := columnsGraph(t)
	var buf bytes.Buffer
	printGraphFlat(&buf, g, []*reportgraph.Node{root}, "")
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	if got := strings.Join(strings.Fields(lines[0]), " "); got != "KIND NAME HASH DEPENDS ON DETAILS" {
		t.Fatalf("header changed: %q", lines[0])
	}
	table := findLine(t, lines, "Table page#0")
	want := ", columns[sales]=scenario:ac1,pp1 filter:region implicit:category,categoryIndex,operation" +
		", columns[plan]=scenario:ac1,pp1 filter:region implicit:category,categoryIndex,operation"
	if !strings.HasSuffix(table, want) {
		t.Errorf("table row does not end with the columns in binding order:\n%s", table)
	}
	if line := findLine(t, lines, "DataSet "); strings.Contains(line, "columns[") {
		t.Errorf("dataset row has columns: %s", line)
	}
}

func TestPrintGraphTreeColumns(t *testing.T) {
	g, root := columnsGraph(t)
	var buf bytes.Buffer
	printGraphTree(&buf, g, []*reportgraph.Node{root}, "")
	lines := strings.Split(buf.String(), "\n")

	tableAt := indexOfLine(t, lines, "Table page#0")
	if got, want := lines[tableAt+1], "    │   │   columns[sales]=scenario:ac1,pp1 filter:region implicit:category,categoryIndex,operation"; got != want {
		t.Errorf("table columns line:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(lines[tableAt+3], "[DataSet]") {
		t.Errorf("dataset child should follow the column lines, got %q", lines[tableAt+3])
	}
	textAt := indexOfLine(t, lines, "Text page#1")
	if got, want := lines[textAt+1], "            columns[kpi]=template:ac1"; got != want {
		t.Errorf("text columns line (node without children):\n got %q\nwant %q", got, want)
	}
}

func findLine(t *testing.T, lines []string, substr string) string {
	t.Helper()
	return lines[indexOfLine(t, lines, substr)]
}

func indexOfLine(t *testing.T, lines []string, substr string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i
		}
	}
	t.Fatalf("no line contains %q in:\n%s", substr, strings.Join(lines, "\n"))
	return -1
}
