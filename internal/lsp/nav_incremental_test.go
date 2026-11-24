package lsp

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

// navSnapshot renders what navigation sees, independent of map order.
func navSnapshot(s *Server, names []string) string {
	ni := s.getNameIndex(context.Background())
	var lines []string
	for _, d := range ni.Defs() {
		lines = append(lines, fmt.Sprintf("def %+v", d))
	}
	for _, n := range names {
		for _, r := range ni.References("", n) {
			lines = append(lines, fmt.Sprintf("ref %+v", r))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestNameIndexIncrementalMatchesRebuild: after a buffer edit and a close the
// incrementally updated index equals a full rebuild from disk plus buffers.
func TestNameIndexIncrementalMatchesRebuild(t *testing.T) {
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "data.yaml")
	reportFile := filepath.Join(dir, "report.yaml")
	for f, c := range map[string]string{dataFile: dataDoc, reportFile: reportDoc} {
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	be := &fakeBackend{index: []IndexDoc{
		{Kind: "DataSet", Name: "sales", File: dataFile, Position: 1},
		{Kind: "Table", Name: "rev_table", File: reportFile, Position: 1},
	}}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(be, log, true, dir)
	ctx := context.Background()
	names := []string{"sales", "revenue", "rev_table", "t2", "sales_src"}
	_ = s.getNameIndex(ctx)

	check := func(step string) {
		t.Helper()
		incremental := navSnapshot(s, names)
		s.mu.Lock()
		s.nameIdx = nil
		s.mu.Unlock()
		if full := navSnapshot(s, names); incremental != full {
			t.Fatalf("%s: incremental index differs from a rebuild\n got:\n%s\nwant:\n%s", step, incremental, full)
		}
	}

	u := uri.File(reportFile)
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t2\nspec:\n  dataset: revenue\n---\nkind: DataSet\nmetadata:\n  name: revenue\nspec:\n  source: sales_src\n", 2)
	s.updateNav(ctx, reportFile)
	if s.nameIdx == nil {
		t.Fatal("an edit must update the index, not drop it")
	}
	check("edit")

	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t2\nspec:\n  dataset: sales\n", 3)
	s.updateNav(ctx, reportFile)
	s.docs.Remove(u)
	s.updateNav(ctx, reportFile)
	check("close")
}
