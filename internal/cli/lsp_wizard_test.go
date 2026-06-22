package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/schema"
)

func newWizardProject(t *testing.T) (dir, csv string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("name = \"test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	csv = filepath.Join(dir, "data", "sales.csv")
	if err := os.MkdirAll(filepath.Dir(csv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csv, []byte("id,Full Name,amount\n1,Ada,42.5\n2,Nik,37.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, csv
}

func TestRunLSPIntrospectDraftCSV(t *testing.T) {
	dir, csv := newWizardProject(t)
	spec := fmt.Sprintf(`{"type":"csv","path":%q}`, csv)

	var buf bytes.Buffer
	if err := runLSPIntrospectDraft(context.Background(), dir, "", []byte(spec), "", 50, &buf); err != nil {
		t.Fatalf("introspect: %v", err)
	}

	var res lspIntrospectResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if res.Error != "" {
		t.Fatalf("error: %s", res.Error)
	}
	if res.Version == "" {
		t.Error("missing version")
	}
	want := []string{"id", "Full Name", "amount"}
	if len(res.Columns) != len(want) {
		t.Fatalf("columns = %+v, want %v", res.Columns, want)
	}
	for i, c := range res.Columns {
		if c.Name != want[i] {
			t.Errorf("col[%d] = %q, want %q", i, c.Name, want[i])
		}
	}
	if res.DetectedCSV == nil || res.DetectedCSV.Delimiter != "," {
		t.Errorf("detectedCsv = %+v, want delimiter ','", res.DetectedCSV)
	}
}

func TestRunLSPScaffoldCreatesValidManifests(t *testing.T) {
	dir, csv := newWizardProject(t)
	payload := fmt.Sprintf(`{
		"dataSource": {"name":"sales_src","type":"csv","path":%q,"delimiter":";"},
		"dataSet": {"name":"sales","pretty":true,"columns":[
			{"name":"id","type":"BIGINT"},
			{"name":"Full Name","type":"VARCHAR"},
			{"name":"amount","type":"VARCHAR","targetType":"DECIMAL(18,2)"}
		]}
	}`, csv)

	var buf bytes.Buffer
	if err := runLSPScaffold(context.Background(), dir, []byte(payload), &buf); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	var res lspScaffoldResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if !res.OK || res.Error != "" {
		t.Fatalf("scaffold failed: ok=%v error=%s", res.OK, res.Error)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want 2 (datasource + dataset)", res.Files)
	}

	for _, f := range res.Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatalf("read %s: %v", f.Path, err)
		}
		if err := schema.Validate(b); err != nil {
			t.Fatalf("file %s failed schema.Validate:\n%s\nerror: %v", f.Path, b, err)
		}
	}

	// The DataSet manifest should carry the generated typed SELECT.
	dsetBytes, err := os.ReadFile(filepath.Join(dir, res.Files[1].Path))
	if err != nil {
		t.Fatal(err)
	}
	dset := string(dsetBytes)
	if !bytes.Contains(dsetBytes, []byte("AS full_name")) {
		t.Errorf("dataset missing typed SELECT alias:\n%s", dset)
	}

	// The DataSource path should be relative to its manifest, not absolute.
	dsBytes, err := os.ReadFile(filepath.Join(dir, res.Files[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dsBytes, []byte(csv)) {
		t.Errorf("datasource path should be relative, not absolute:\n%s", dsBytes)
	}
}

// seedEditProject writes a project root with a single Text manifest carrying a
// comment, and returns the project dir and the manifest's absolute path.
func seedEditProject(t *testing.T) (dir, manifest string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("name = \"test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(dir, "note.yaml")
	original := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: note # keep me\nspec:\n  value: old\n"
	if err := os.WriteFile(manifest, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}

func runEdit(t *testing.T, dir string, payload string) lspEditResult {
	t.Helper()
	var buf bytes.Buffer
	if err := runLSPEdit(context.Background(), dir, []byte(payload), &buf); err != nil {
		t.Fatalf("edit: %v", err)
	}
	var res lspEditResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	return res
}

// compute mode returns the rewritten text but must not touch the file on disk.
func TestRunLSPEditComputeDoesNotWrite(t *testing.T) {
	dir, manifest := seedEditProject(t)
	before, _ := os.ReadFile(manifest)

	res := runEdit(t, dir, `{"file":"note.yaml","patch":{"spec.value":"new"},"mode":"compute"}`)
	if !res.OK || res.Error != "" {
		t.Fatalf("compute failed: ok=%v error=%s", res.OK, res.Error)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("compute reported diagnostics for a valid edit: %+v", res.Diagnostics)
	}
	if res.Full == "" {
		t.Fatal("compute returned empty full text")
	}
	if !bytes.Contains([]byte(res.Full), []byte("value: new")) || !bytes.Contains([]byte(res.Full), []byte("# keep me")) {
		t.Errorf("compute full did not apply the edit or dropped the comment:\n%s", res.Full)
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) {
		t.Errorf("compute wrote to disk:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// When the IDE supplies the open buffer's `content` (unsaved/dirty edits),
// compute must derive the rewritten file from that buffer, not from the stale
// on-disk copy — otherwise a Design edit would clobber the user's unsaved work.
func TestRunLSPEditComputeUsesSuppliedContent(t *testing.T) {
	dir, manifest := seedEditProject(t)
	diskBefore, _ := os.ReadFile(manifest)

	// Live buffer: spec.value is dirty (not yet saved) and carries an unsaved
	// comment that does not exist on disk. The patch touches an unrelated key.
	buffer := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: note # keep me\nspec:\n  value: dirty-unsaved # not on disk\n"
	payload, err := json.Marshal(map[string]any{
		"file":    "note.yaml",
		"mode":    "compute",
		"content": buffer,
		"patch":   map[string]any{"metadata.name": "renamed"},
	})
	if err != nil {
		t.Fatal(err)
	}

	res := runEdit(t, dir, string(payload))
	if !res.OK || res.Error != "" {
		t.Fatalf("compute failed: ok=%v error=%s", res.OK, res.Error)
	}
	// The unrelated patch applied...
	if !bytes.Contains([]byte(res.Full), []byte("name: renamed")) {
		t.Errorf("compute did not apply the patch:\n%s", res.Full)
	}
	// ...on top of the BUFFER (dirty value + unsaved comment preserved),
	// proving disk was not the compute source (no-clobber).
	if !bytes.Contains([]byte(res.Full), []byte("value: dirty-unsaved")) {
		t.Errorf("compute clobbered the dirty buffer value with stale disk:\n%s", res.Full)
	}
	if !bytes.Contains([]byte(res.Full), []byte("# not on disk")) {
		t.Errorf("compute dropped the unsaved buffer comment:\n%s", res.Full)
	}
	if bytes.Contains([]byte(res.Full), []byte("value: old")) {
		t.Errorf("compute used the stale on-disk value instead of the buffer:\n%s", res.Full)
	}
	// And disk is still untouched (compute never writes).
	diskAfter, _ := os.ReadFile(manifest)
	if !bytes.Equal(diskBefore, diskAfter) {
		t.Errorf("compute wrote to disk:\nbefore:\n%s\nafter:\n%s", diskBefore, diskAfter)
	}
}

// write mode lands exactly the bytes compute returned, and matches the
// fidelity-preserving engine output.
func TestRunLSPEditWriteMatchesCompute(t *testing.T) {
	dir, manifest := seedEditProject(t)
	payload := `{"file":"note.yaml","patch":{"spec.value":"new"},"mode":%q}`

	computed := runEdit(t, dir, fmt.Sprintf(payload, "compute"))
	if !computed.OK {
		t.Fatalf("compute failed: %+v", computed)
	}

	written := runEdit(t, dir, fmt.Sprintf(payload, "write"))
	if !written.OK || written.Error != "" {
		t.Fatalf("write failed: ok=%v error=%s", written.OK, written.Error)
	}
	if written.File != "note.yaml" {
		t.Errorf("write file = %q, want note.yaml", written.File)
	}

	onDisk, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != computed.Full {
		t.Errorf("write bytes != compute full:\non disk:\n%s\ncompute:\n%s", onDisk, computed.Full)
	}
}

// An invalid patch yields diagnostics and writes nothing.
func TestRunLSPEditInvalidPatchBlocksWrite(t *testing.T) {
	dir, manifest := seedEditProject(t)
	before, _ := os.ReadFile(manifest)

	// spec.value must be a string; a mapping makes the document invalid.
	res := runEdit(t, dir, `{"file":"note.yaml","patch":{"spec.value":{"not":"a string"}},"mode":"write"}`)
	if res.OK {
		t.Fatal("invalid edit should not be ok")
	}
	if len(res.Diagnostics) == 0 {
		t.Fatalf("invalid edit produced no diagnostics: %+v", res)
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) {
		t.Errorf("invalid edit wrote to disk:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// lsp-helper edit (write) and the MCP edit_manifest path must land identical bytes.
func TestRunLSPEditParityWithEditManifest(t *testing.T) {
	// lsp-helper edit --write
	dirA, manA := seedEditProject(t)
	resA := runEdit(t, dirA, `{"file":"note.yaml","patch":{"spec.value":"new"},"mode":"write"}`)
	if !resA.OK {
		t.Fatalf("lsp-helper edit write failed: %+v", resA)
	}
	gotA, _ := os.ReadFile(manA)

	// MCP EditManifest (writes to disk)
	dirB, manB := seedEditProject(t)
	a := newCLIAuthoring(dirB)
	wr, err := a.EditManifest(context.Background(), mcp.EditManifestInput{
		File: "note.yaml", Patch: map[string]any{"spec.value": "new"},
	})
	if err != nil {
		t.Fatalf("EditManifest: %v", err)
	}
	if wr.Action != "edited" || wr.Content != "" {
		t.Errorf("EditManifest write result = %+v, want action=edited, empty content", wr)
	}
	gotB, _ := os.ReadFile(manB)

	if string(gotA) != string(gotB) {
		t.Errorf("lsp-helper edit and edit_manifest disagree:\nlsp-helper:\n%s\nedit_manifest:\n%s", gotA, gotB)
	}

	// And the MCP dry-run content must equal the bytes the write path lands.
	dirC, _ := seedEditProject(t)
	c := newCLIAuthoring(dirC)
	dry, err := c.EditManifest(context.Background(), mcp.EditManifestInput{
		File: "note.yaml", Patch: map[string]any{"spec.value": "new"}, DryRun: true,
	})
	if err != nil {
		t.Fatalf("EditManifest dry run: %v", err)
	}
	if dry.Action != "computed" {
		t.Errorf("dry-run action = %q, want computed", dry.Action)
	}
	if dry.Content != string(gotB) {
		t.Errorf("dry-run content != written bytes:\ndry:\n%s\nwritten:\n%s", dry.Content, gotB)
	}
}

// seedSeqProject writes a project root with a DataSet whose spec.dependencies is
// a reorderable/removable sequence of scalar references (schema-valid as-is).
func seedSeqProject(t *testing.T) (dir, manifest string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("name = \"test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(dir, "ds.yaml")
	original := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: ds # keep me\nspec:\n  query: SELECT 1\n  dependencies:\n    - $a\n    - $b\n    - $c\n"
	if err := os.WriteFile(manifest, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}

// op=remove: compute returns the rewritten text without writing; write lands the
// same bytes; and the MCP remove_manifest_fields path agrees.
func TestRunLSPEditRemoveParity(t *testing.T) {
	dir, manifest := seedSeqProject(t)
	before, _ := os.ReadFile(manifest)

	computed := runEdit(t, dir, `{"file":"ds.yaml","op":"remove","paths":["spec.dependencies[1]"],"mode":"compute"}`)
	if !computed.OK || computed.Error != "" {
		t.Fatalf("remove compute failed: ok=%v error=%s diags=%+v", computed.OK, computed.Error, computed.Diagnostics)
	}
	if bytes.Contains([]byte(computed.Full), []byte("$b")) {
		t.Errorf("remove compute kept the deleted element:\n%s", computed.Full)
	}
	if after, _ := os.ReadFile(manifest); !bytes.Equal(before, after) {
		t.Errorf("remove compute wrote to disk")
	}

	written := runEdit(t, dir, `{"file":"ds.yaml","op":"remove","paths":["spec.dependencies[1]"],"mode":"write"}`)
	if !written.OK {
		t.Fatalf("remove write failed: %+v", written)
	}
	onDisk, _ := os.ReadFile(manifest)
	if string(onDisk) != computed.Full {
		t.Errorf("remove write bytes != compute full:\non disk:\n%s\ncompute:\n%s", onDisk, computed.Full)
	}

	// MCP parity: RemoveManifestPaths must land identical bytes.
	dirB, manB := seedSeqProject(t)
	a := newCLIAuthoring(dirB)
	wr, err := a.RemoveManifestPaths(context.Background(), mcp.RemoveManifestPathsInput{
		File: "ds.yaml", Paths: []string{"spec.dependencies[1]"},
	})
	if err != nil {
		t.Fatalf("RemoveManifestPaths: %v", err)
	}
	if wr.Action != "edited" {
		t.Errorf("RemoveManifestPaths action = %q, want edited", wr.Action)
	}
	if gotB, _ := os.ReadFile(manB); string(gotB) != string(onDisk) {
		t.Errorf("lsp-helper remove and remove_manifest_fields disagree:\nlsp:\n%s\nmcp:\n%s", onDisk, gotB)
	}

	// MCP dry-run content equals the written bytes.
	dirC, _ := seedSeqProject(t)
	dry, err := newCLIAuthoring(dirC).RemoveManifestPaths(context.Background(), mcp.RemoveManifestPathsInput{
		File: "ds.yaml", Paths: []string{"spec.dependencies[1]"}, DryRun: true,
	})
	if err != nil {
		t.Fatalf("RemoveManifestPaths dry run: %v", err)
	}
	if dry.Action != "computed" || dry.Content != string(onDisk) {
		t.Errorf("remove dry-run = %+v, want computed content == written bytes", dry)
	}
}

// op=reorder: compute≡write, MCP parity, and a no-op is byte-identical.
func TestRunLSPEditReorderParity(t *testing.T) {
	dir, manifest := seedSeqProject(t)

	computed := runEdit(t, dir, `{"file":"ds.yaml","op":"reorder","path":"spec.dependencies","from":0,"to":2,"mode":"compute"}`)
	if !computed.OK || computed.Error != "" {
		t.Fatalf("reorder compute failed: ok=%v error=%s diags=%+v", computed.OK, computed.Error, computed.Diagnostics)
	}

	written := runEdit(t, dir, `{"file":"ds.yaml","op":"reorder","path":"spec.dependencies","from":0,"to":2,"mode":"write"}`)
	if !written.OK {
		t.Fatalf("reorder write failed: %+v", written)
	}
	onDisk, _ := os.ReadFile(manifest)
	if string(onDisk) != computed.Full {
		t.Errorf("reorder write bytes != compute full:\non disk:\n%s\ncompute:\n%s", onDisk, computed.Full)
	}

	// MCP parity against a fresh copy.
	dirB, manB := seedSeqProject(t)
	if _, err := newCLIAuthoring(dirB).ReorderManifestSequence(context.Background(), mcp.ReorderManifestSequenceInput{
		File: "ds.yaml", Path: "spec.dependencies", From: 0, To: 2,
	}); err != nil {
		t.Fatalf("ReorderManifestSequence: %v", err)
	}
	if gotB, _ := os.ReadFile(manB); string(gotB) != string(onDisk) {
		t.Errorf("lsp-helper reorder and reorder_manifest_sequence disagree:\nlsp:\n%s\nmcp:\n%s", onDisk, gotB)
	}

	// No-op reorder is byte-identical to the canonical re-encoding.
	dirC, _ := seedSeqProject(t)
	noop := runEdit(t, dirC, `{"file":"ds.yaml","op":"reorder","path":"spec.dependencies","from":1,"to":1,"mode":"compute"}`)
	if !noop.OK {
		t.Fatalf("no-op reorder failed: %+v", noop)
	}
	canon := runEdit(t, dirC, `{"file":"ds.yaml","op":"reorder","path":"spec.dependencies","from":0,"to":0,"mode":"compute"}`)
	if noop.Full != canon.Full {
		t.Errorf("no-op reorder not byte-identical:\nfrom1to1:\n%s\nfrom0to0:\n%s", noop.Full, canon.Full)
	}
}

// A removal that makes the document schema-invalid returns diagnostics and
// writes nothing (the transport's validation gate applies to every op).
func TestRunLSPEditRemoveInvalidBlocksWrite(t *testing.T) {
	dir, manifest := seedEditProject(t)
	before, _ := os.ReadFile(manifest)

	// spec.value is required for a Text document; removing it is invalid.
	res := runEdit(t, dir, `{"file":"note.yaml","op":"remove","paths":["spec.value"],"mode":"write"}`)
	if res.OK {
		t.Fatal("invalid removal should not be ok")
	}
	if len(res.Diagnostics) == 0 {
		t.Fatalf("invalid removal produced no diagnostics: %+v", res)
	}
	if after, _ := os.ReadFile(manifest); !bytes.Equal(before, after) {
		t.Errorf("invalid removal wrote to disk")
	}
}

// Per-op required fields and unknown ops are reported as errors.
func TestRunLSPEditOpValidation(t *testing.T) {
	dir, _ := seedSeqProject(t)
	cases := map[string]string{
		"remove without paths": `{"file":"ds.yaml","op":"remove","mode":"compute"}`,
		"reorder without path": `{"file":"ds.yaml","op":"reorder","from":0,"to":1,"mode":"compute"}`,
		"unknown op":           `{"file":"ds.yaml","op":"frobnicate","mode":"compute"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			res := runEdit(t, dir, payload)
			if res.OK || res.Error == "" {
				t.Errorf("expected an error result, got %+v", res)
			}
		})
	}
}

// When the wizard sends an edited SQL, the DataSet must carry it verbatim
// instead of regenerating a typed SELECT from the columns.
func TestRunLSPScaffoldUsesEditedSQL(t *testing.T) {
	dir, csv := newWizardProject(t)
	payload := fmt.Sprintf(`{
		"dataSource": {"name":"sales_src","type":"csv","path":%q,"delimiter":";"},
		"dataSet": {"name":"sales","pretty":true,"sql":"SELECT 42 AS sentinel_xyz FROM sales_src","columns":[
			{"name":"id","type":"BIGINT"}
		]}
	}`, csv)

	var buf bytes.Buffer
	if err := runLSPScaffold(context.Background(), dir, []byte(payload), &buf); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	var res lspScaffoldResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if !res.OK || res.Error != "" {
		t.Fatalf("scaffold failed: ok=%v error=%s", res.OK, res.Error)
	}

	dsetBytes, err := os.ReadFile(filepath.Join(dir, res.Files[1].Path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dsetBytes, []byte("sentinel_xyz")) {
		t.Errorf("dataset should use the edited SQL verbatim:\n%s", dsetBytes)
	}
	if bytes.Contains(dsetBytes, []byte(`"id"`)) {
		t.Errorf("dataset should not contain a regenerated typed SELECT:\n%s", dsetBytes)
	}
	if err := schema.Validate(dsetBytes); err != nil {
		t.Fatalf("edited-SQL dataset failed schema.Validate:\n%s\nerror: %v", dsetBytes, err)
	}
}
