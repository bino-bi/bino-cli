//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/numbervalues"
)

// The three flags on a real Chrome build: the overlay replaces the page and
// adds a DataSet, the values file has the table cells and the chart labels,
// and every shown number matches the DataSet rows. The project is read-only
// and stays as it was.
func TestBuildOverlayNumberValues(t *testing.T) {
	requireRenderStack(t)
	t.Setenv("CI", "1")

	// The data-point keys need a recent engine: build with the CLI's pin.
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	pin := dockerfileEnginePin.FindSubmatch(dockerfile)
	if pin == nil {
		t.Fatal("no ARG ENGINE_VERSION pin in Dockerfile")
	}
	project := t.TempDir()
	writeFiles(t, project, overlayProject)
	writeFiles(t, project, map[string]string{"bino.toml": overlayProject["bino.toml"] + "engine-version = \"" + string(pin[1]) + "\"\n"})
	overlayDir := t.TempDir()
	writeFiles(t, overlayDir, overlayDraft)
	outDir := t.TempDir()
	before := treeSnapshot(t, project)
	setReadOnly(t, project)

	root := newRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"build", "--work-dir", project, "--out-dir", outDir, "--overlay", overlayDir,
		"--verify-numbers", "--layout-state", "--build-log-format", "json", "--no-graph"})
	if err = root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	valuesPath := filepath.Join(outDir, "report.values.json")
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatal(err)
	}
	var file numbervalues.File
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	byKind := map[string]numbervalues.Component{}
	for _, c := range file.Components {
		byKind[c.Kind] = c
	}

	table := byKind["Table"]
	if !reflect.DeepEqual(table.Datasets, []string{"draft_region"}) {
		t.Errorf("table datasets = %v, want the overlay's draft_region", table.Datasets)
	}
	keys := map[string]bool{}
	for _, v := range table.Values {
		keys[v.Key] = true
		if v.Key == "ac1" && v.Point["category"] == "North" && v.Value != float64(4200) {
			t.Errorf("North ac1 = %v, want 4200 from the overlay's DataSet", v.Value)
		}
	}
	for _, key := range []string{"ac1", "pl1", "dac1_pl1_pos", "drac1_pl1_pos"} {
		if !keys[key] {
			t.Errorf("table has no %s values: %s", key, data)
		}
	}

	chart := byKind["ChartTime"]
	labels := 0
	for _, v := range chart.Values {
		if v.Text != nil && v.Point["date"] != "" {
			labels++
		}
	}
	if labels == 0 {
		t.Errorf("chart has no labels with a date: %s", data)
	}

	if file.Check == nil || file.Differences == nil {
		t.Fatalf("no check in the values file: %s", data)
	}
	if file.Check.Differences != 0 || file.Check.Unmatched != 0 || file.Check.Matched != file.Check.Values || file.Check.Values == 0 {
		t.Errorf("check = %+v, differences = %+v", *file.Check, *file.Differences)
	}

	logs, err := filepath.Glob(filepath.Join(outDir, "bino-build-*.json"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("JSON build logs %v: %v", logs, err)
	}
	logData, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	var log buildlog.JSONBuildLog
	if err := json.Unmarshal(logData, &log); err != nil {
		t.Fatal(err)
	}
	entry := log.Artifacts[0]
	if entry.Values != valuesPath || entry.LayoutState != filepath.Join(outDir, "report.layout.json") {
		t.Errorf("artefact entry = %+v", entry)
	}
	if entry.NumberCheck == nil || entry.NumberCheck.Values != file.Check.Values || entry.NumberCheck.Differences != 0 {
		t.Errorf("number_check = %+v", entry.NumberCheck)
	}
	for _, doc := range log.Documents {
		if filepath.Dir(filepath.Dir(doc.File)) != project {
			t.Errorf("build log names %s, want a path in the project", doc.File)
		}
	}

	if after := treeSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed:\nbefore %v\nafter  %v", before, after)
	}
}

// setReadOnly makes dir and everything in it read-only until the test ends.
func setReadOnly(t *testing.T, dir string) {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		paths = append(paths, path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chmod(paths[i], 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, path := range paths {
			_ = os.Chmod(path, 0o755)
		}
	})
}
