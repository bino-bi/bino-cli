package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
)

// overlayProject is a small project shaped like a real one: a CSV read by a
// relative path, a monthly and a regional DataSet, one page with a ChartTime
// and a Table, and the report.
var overlayProject = map[string]string{
	"bino.toml": "report-id = \"overlay-test\"\n",
	"data/sales.csv": `month,region,actual,plan
2025-01-31,North,1000,900
2025-01-31,South,500,600
2025-02-28,North,1100,1000
2025-02-28,South,700,650
`,
	"datasources/sales.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ../data/sales.csv
`,
	"datasets/sales.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_monthly
spec:
  dependencies: [sales_csv]
  query: |
    SELECT strftime(CAST(month AS DATE), '%Y-%m-%d') AS date, SUM(actual) AS ac1, SUM(plan) AS pl1
    FROM sales_csv GROUP BY 1 ORDER BY 1
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_region
spec:
  dependencies: [sales_csv]
  query: |
    SELECT region AS category, CASE region WHEN 'North' THEN 1 ELSE 2 END AS categoryIndex,
           SUM(actual) AS ac1, SUM(plan) AS pl1
    FROM sales_csv GROUP BY region ORDER BY categoryIndex
`,
	"pages/sales.yaml": overlayPage("sales_region"),
	"reports/report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Overlay test
  format: a4
  orientation: landscape
  layoutPages: [sales]
`,
}

// overlayPage is the project's page, with the Table reading tableDataset.
func overlayPage(tableDataset string) string {
	return `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: sales
spec:
  pageFormat: a4
  pageOrientation: landscape
  pageLayout: split-horizontal
  children:
    - kind: ChartTime
      metadata:
        name: monthly
      spec:
        dataset: sales_monthly
        dateInterval: month
        scenarios: [ac1, pl1]
        variances: [dac1_pl1_pos]
    - kind: Table
      metadata:
        name: regions
      spec:
        dataset: ` + tableDataset + `
        type: sum
        order: categoryindex
        scenarios: [pl1, ac1]
        variances: [dac1_pl1_pos, drac1_pl1_pos]
`
}

// overlayDraft replaces the page so the Table reads a DataSet that only the
// overlay has.
var overlayDraft = map[string]string{
	"pages/sales.yaml": overlayPage("draft_region"),
	"datasets/draft.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: draft_region
spec:
  dependencies: [sales_csv]
  query: |
    SELECT region AS category, CASE region WHEN 'North' THEN 1 ELSE 2 END AS categoryIndex,
           SUM(actual) * 2 AS ac1, SUM(plan) AS pl1
    FROM sales_csv GROUP BY region ORDER BY categoryIndex
`,
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// treeSnapshot lists every file and directory of dir with its content.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func loadOverlayManifests(t *testing.T, root string) *buildManifestData {
	t.Helper()
	out := NewOutput(OutputConfig{Stdout: io.Discard, Stderr: io.Discard, NoColor: true})
	manifests, err := loadBuildManifests(context.Background(), out, logx.Nop(), root, nil, nil, false, true, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manifests
}

func datasetRows(t *testing.T, result pipeline.RenderResult, name string) []map[string]any {
	t.Helper()
	for _, ds := range result.Datasets {
		if ds.Name == name {
			var rows []map[string]any
			if err := json.Unmarshal(ds.JSONRows, &rows); err != nil {
				t.Fatal(err)
			}
			return rows
		}
	}
	t.Fatalf("no dataset %s", name)
	return nil
}

func TestBuildOverlay_ReplacesPageAndAddsDataSet(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	// directories the loader skips are linked whole, never written through
	writeFiles(t, project, map[string]string{"node_modules/pkg/index.js": "x\n", "dist/old.pdf": "old\n"})
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, overlayDraft)
	before := treeSnapshot(t, project)

	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	manifests := loadOverlayManifests(t, overlay.Root)

	var page config.Document
	names := map[string]bool{}
	for _, doc := range manifests.Documents {
		names[doc.Kind+"/"+doc.Name] = true
		if doc.Kind == "LayoutPage" {
			page = doc
		}
	}
	if !names["DataSet/draft_region"] || !names["DataSet/sales_region"] {
		t.Fatalf("documents %v: want the project's DataSets and the overlay's", names)
	}
	if !strings.Contains(string(page.Raw), "draft_region") {
		t.Errorf("the page is the project's, not the overlay's: %s", page.Raw)
	}
	if got := overlay.ProjectPath(page.File); got != filepath.Join(project, "pages", "sales.yaml") {
		t.Errorf("ProjectPath(page) = %s", got)
	}
	if src, ok := overlay.FromOverlay(page.File); !ok || src != filepath.Join(overlayDir, "pages", "sales.yaml") {
		t.Errorf("FromOverlay(page) = %s, %v", src, ok)
	}

	// The new DataSet reads the project's CSV through the project's relative path.
	builder := &pipeline.Builder{Workdir: overlay.Root, Logger: logx.Nop()}
	result, err := builder.RenderArtefactHTML(context.Background(), manifests.Documents, manifests.Selected[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.HTML), "draft_region") {
		t.Error("the rendered page does not use the overlay's DataSet")
	}
	rows := datasetRows(t, result, "draft_region")
	if len(rows) != 2 || rows[0]["ac1"] != float64(4200) {
		t.Errorf("draft_region rows = %v, want North ac1 2 * 2100", rows)
	}

	if err := overlay.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlay.Root); !os.IsNotExist(err) {
		t.Errorf("temporary tree left behind: %v", err)
	}
	if after := treeSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestBuildOverlay_DataFileFromOverlay(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"data/sales.csv": "month,region,actual,plan\n2025-01-31,North,1,2\n"})

	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup
	manifests := loadOverlayManifests(t, overlay.Root)
	builder := &pipeline.Builder{Workdir: overlay.Root, Logger: logx.Nop()}
	result, err := builder.RenderArtefactHTML(context.Background(), manifests.Documents, manifests.Selected[0])
	if err != nil {
		t.Fatal(err)
	}
	if rows := datasetRows(t, result, "sales_region"); len(rows) != 1 || rows[0]["ac1"] != float64(1) {
		t.Errorf("sales_region rows = %v, want the overlay's CSV", rows)
	}
}

func TestBuildOverlay_ErrorNamesProjectPath(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"pages/sales.yaml": "apiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: [broken\n"})

	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup
	out := NewOutput(OutputConfig{Stdout: io.Discard, Stderr: io.Discard, NoColor: true})
	_, loadErr := loadBuildManifests(context.Background(), out, logx.Nop(), overlay.Root, nil, nil, false, true, false, nil, nil)
	if loadErr == nil {
		t.Fatal("a broken overlay file must fail the build")
	}
	err = overlay.WrapError(loadErr)

	projectPage := filepath.Join(project, "pages", "sales.yaml")
	if msg := err.Error(); !strings.Contains(msg, projectPage) || strings.Contains(msg, overlay.Root) || !strings.Contains(msg, "line 3") {
		t.Errorf("message %q: want the project path and the line, not the temporary one", msg)
	}
	if file, _ := errorSource(err); file != projectPage {
		t.Errorf("error source = %s, want %s", file, projectPage)
	}
	var oe *overlayError
	if !errors.As(err, &oe) || !strings.Contains(oe.Note(), filepath.Join(overlayDir, "pages", "sales.yaml")) {
		t.Errorf("the error does not say the content came from the overlay: %v", err)
	}
	if src, ok := overlaySource(err, projectPage); !ok || src != filepath.Join(overlayDir, "pages", "sales.yaml") {
		t.Errorf("overlaySource = %s, %v", src, ok)
	}
}

func TestBuildOverlay_Rejects(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)

	withToml := t.TempDir()
	writeFiles(t, withToml, map[string]string{"bino.toml": "report-id = \"x\"\n"})
	if _, err := newBuildOverlay(project, withToml); err == nil || !strings.Contains(err.Error(), "bino.toml") {
		t.Errorf("an overlay bino.toml: err = %v", err)
	}
	if _, err := newBuildOverlay(project, filepath.Join(project, "missing")); err == nil {
		t.Error("a missing overlay directory must fail")
	}
	if _, err := newBuildOverlay(filepath.Join(project, "pages"), project); err == nil {
		t.Error("an overlay that contains the project must fail")
	}
}

// An overlay file below a linked directory of the project would be written
// through the link, so the build refuses it.
func TestBuildOverlay_RejectsFileBelowProjectLink(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	shared := t.TempDir()
	if err := os.Symlink(shared, filepath.Join(project, "shared")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"shared/x.csv": "a\n1\n"})

	if _, err := newBuildOverlay(project, overlayDir); err == nil || !strings.Contains(err.Error(), "shared is a link") {
		t.Errorf("err = %v", err)
	}
	if entries, _ := os.ReadDir(shared); len(entries) != 0 {
		t.Errorf("the overlay wrote through the link: %v", entries)
	}
}

// An overlay kept inside the project is not read as project files.
func TestBuildOverlay_InsideProject(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := filepath.Join(project, ".drafts")
	writeFiles(t, overlayDir, overlayDraft)

	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup
	pages := 0
	for _, doc := range loadOverlayManifests(t, overlay.Root).Documents {
		if doc.Kind == "LayoutPage" {
			pages++
		}
	}
	if pages != 1 {
		t.Errorf("%d LayoutPages, want the overlay's one", pages)
	}
}

// The overlay comes from untrusted callers: only regular files are read, and
// a size limit holds.
func TestBuildOverlay_OnlyRegularFiles(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	secret := filepath.Join(t.TempDir(), "secret")
	writeFiles(t, filepath.Dir(secret), map[string]string{"secret": "token\n"})

	withLink := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withLink, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(withLink, "pages", "x.yaml")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if _, err := newBuildOverlay(project, withLink); err == nil || !strings.Contains(err.Error(), "pages/x.yaml is not a regular file") {
		t.Errorf("a link in the overlay: err = %v", err)
	}

	withFifo := t.TempDir()
	if err := mkfifo(filepath.Join(withFifo, "data.csv")); err == nil {
		if _, err := newBuildOverlay(project, withFifo); err == nil || !strings.Contains(err.Error(), "data.csv is not a regular file") {
			t.Errorf("a pipe in the overlay: err = %v", err)
		}
	}

	tooBig := t.TempDir()
	writeFiles(t, tooBig, map[string]string{"data/big.csv": strings.Repeat("x", maxOverlayBytes+1)})
	if _, err := newBuildOverlay(project, tooBig); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Errorf("a too large overlay: err = %v", err)
	}
}

// An overlay file is never written through a link of the tree: not into a
// linked directory, and not over a linked project file whose name differs
// only in case on a case-insensitive file system.
func TestBuildOverlay_CopyNeverWritesThroughLinks(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{"pages/sales.yaml": "original\n"})
	outside := t.TempDir()
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"linked/x.yaml": "draft\n", "pages/sales.yaml": "draft\n"})

	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	// what Sync leaves for a project file the overlay names in another case
	if err := os.Symlink(filepath.Join(project, "pages", "sales.yaml"), filepath.Join(root, "pages", "sales.yaml")); err != nil {
		t.Fatal(err)
	}
	o, src := treeOverlay(t, project, overlayDir, root)
	budget := int64(maxOverlayBytes)

	if err := o.copyOverlayFile(src, "linked/x.yaml", &budget); err == nil {
		t.Error("a file below a linked directory must be refused")
	}
	if err := o.copyOverlayFile(src, "linked/deeper/x.yaml", &budget); err == nil {
		t.Error("a directory below a linked directory must not be created")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("written through the link: %v", entries)
	}
	if err := o.copyOverlayFile(src, "pages/sales.yaml", &budget); err == nil {
		t.Error("an existing linked file must be refused")
	}
	if data, _ := os.ReadFile(filepath.Join(project, "pages", "sales.yaml")); string(data) != "original\n" {
		t.Errorf("the project file was overwritten: %q", data)
	}
}

// treeOverlay is a buildOverlay over an existing tree, for tests of single steps.
func treeOverlay(t *testing.T, project, overlayDir, root string) (*buildOverlay, *os.Root) {
	t.Helper()
	tree, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.OpenRoot(overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tree.Close(); src.Close() })
	return &buildOverlay{ProjectRoot: project, Dir: overlayDir, Root: root, tree: tree, files: map[string]struct{}{}, lower: map[string]string{}}, src
}

// The byte budget holds while copying, also when a file grew after the walk.
func TestBuildOverlay_CopyBudget(t *testing.T) {
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"data/a.csv": "0123456789"})
	o, src := treeOverlay(t, t.TempDir(), overlayDir, t.TempDir())
	budget := int64(4)
	if err := o.copyOverlayFile(src, "data/a.csv", &budget); !errors.Is(err, errOverlayTooLarge) {
		t.Errorf("err = %v, want the size limit", err)
	}
}

// Directories count against the entry limit, not only files.
func TestBuildOverlay_EntryLimitCountsDirectories(t *testing.T) {
	overlayDir := t.TempDir()
	for i := 0; i <= maxOverlayEntries; i++ {
		if err := os.Mkdir(filepath.Join(overlayDir, fmt.Sprintf("d%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	if _, err := newBuildOverlay(project, overlayDir); err == nil || !strings.Contains(err.Error(), "files and directories") {
		t.Errorf("err = %v", err)
	}
}

// A project file whose name differs only in case from an overlay file is
// refused, also when a hook writes it later: on a case-insensitive file
// system the two would be one file.
func TestBuildOverlay_SyncRefusesCaseClash(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, overlayDraft)
	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup

	writeFiles(t, project, map[string]string{"Datasets/Draft.yaml": "x\n"})
	if err := overlay.Sync(); err == nil || !strings.Contains(err.Error(), "differs only in case") {
		t.Errorf("err = %v", err)
	}
}

// A file a hook writes into the project after the tree was built is seen
// once the tree is synced, as the build does after its hooks.
func TestBuildOverlay_SyncSeesNewProjectFiles(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, overlayDraft)
	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup

	writeFiles(t, project, map[string]string{"data/fresh.csv": "a\n1\n", "pages/sales.yaml": overlayPage("changed")})
	if err := overlay.Sync(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(overlay.Root, "data", "fresh.csv")); err != nil || string(data) != "a\n1\n" {
		t.Errorf("new project file: %q, %v", data, err)
	}
	if data, _ := os.ReadFile(filepath.Join(overlay.Root, "pages", "sales.yaml")); !strings.Contains(string(data), "draft_region") {
		t.Error("Sync replaced the overlay's file")
	}
}

func TestBuildStatelessRefusesDraftFlags(t *testing.T) {
	t.Setenv("CI", "1") // no update check in the background
	for _, flag := range [][]string{{"--overlay", t.TempDir()}, {"--number-values"}, {"--verify-numbers"}} {
		root := newRootCommand()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(append([]string{"build", "--stateless"}, flag...))
		if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot be combined with --stateless") {
			t.Errorf("%v: err = %v", flag, err)
		}
	}
}

func TestCheckOutputPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	for _, name := range []string{"report.pdf", "sub/report.pdf"} {
		if err := checkOutputPath(true, out, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"../pages/sales.yaml", "/etc/passwd", "a/../../x.pdf", "."} {
		if err := checkOutputPath(true, out, name); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if err := checkOutputPath(false, out, "../x.pdf"); err != nil {
		t.Errorf("without --overlay nothing is refused: %v", err)
	}
}

// An overlay's ReportArtefact cannot write its PDF over a project file.
func TestBuildOverlay_ArtefactFilenameStaysInOutDir(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, map[string]string{"reports/report.yaml": strings.Replace(overlayProject["reports/report.yaml"],
		"filename: report.pdf", "filename: ../pages/sales.yaml", 1)})
	before := treeSnapshot(t, project)
	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	defer overlay.Close() //nolint:errcheck // test cleanup
	manifests := loadOverlayManifests(t, overlay.Root)

	_, err = buildArtefact(context.Background(), buildArtefactConfig{
		Builder:       &pipeline.Builder{Workdir: overlay.Root, Logger: logx.Nop()},
		Logger:        logx.Nop(),
		Docs:          manifests.Documents,
		Artifact:      manifests.Selected[0],
		OutputDir:     filepath.Join(project, "dist"),
		ConfineOutput: true,
	})
	if err == nil || !strings.Contains(err.Error(), "must stay inside the output directory") {
		t.Errorf("err = %v", err)
	}
	if after := treeSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Error("the project changed")
	}
}

// The copy that stands in for a link (Windows without link rights) follows a
// project link to its file, as the build does without --overlay.
func TestBuildOverlay_CopyProjectFileFollowsLinks(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, map[string]string{"data/real.csv": "a\n1\n"})
	link := filepath.Join(project, "data", "alias.csv")
	if err := os.Symlink(filepath.Join(project, "data", "real.csv"), link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	root := t.TempDir()
	o, _ := treeOverlay(t, project, t.TempDir(), root)
	if err := o.copyProjectFile(link, "alias.csv"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "alias.csv")); err != nil || string(data) != "a\n1\n" {
		t.Errorf("copy = %q, %v", data, err)
	}
}

// The build writes caches below .bino. With --overlay they land in the tree:
// the project's .bino, registry and old cache included, stays byte-identical,
// and installed registry packages are still read.
func TestBuildOverlay_BinoCacheStaysInTree(t *testing.T) {
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	writeFiles(t, project, map[string]string{
		".bino/registry/@acme/vendor/kit.yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: kit_values\nspec:\n  query: SELECT 1 AS ac1\n",
		".bino/cache/datasets/old.json":        "{\"old\":true}\n",
		".bino/cache/datasources/keep.csv":     "a\n1\n",
	})
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, overlayDraft)
	before := treeSnapshot(t, project)

	overlay, err := newBuildOverlay(project, overlayDir)
	if err != nil {
		t.Fatal(err)
	}
	manifests := loadOverlayManifests(t, overlay.Root)
	found := false
	for _, doc := range manifests.Documents {
		found = found || doc.Name == "kit_values"
	}
	if !found {
		t.Error("the registry package below .bino/registry is not loaded")
	}
	builder := &pipeline.Builder{Workdir: overlay.Root, Logger: logx.Nop()}
	if _, err := builder.RenderArtefactHTML(context.Background(), manifests.Documents, manifests.Selected[0]); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(overlay.Root, ".bino", "cache")); err != nil || !info.IsDir() {
		t.Errorf("the cache is not a directory of the tree: %v, %v", info, err)
	}
	if err := overlay.Close(); err != nil {
		t.Fatal(err)
	}
	if after := treeSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed:\nbefore %v\nafter  %v", before, after)
	}
}
