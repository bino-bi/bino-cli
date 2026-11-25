package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/projectlayout"
)

// Every authoring tool refuses a path that leaves the project root and still
// accepts one inside it.
func TestAuthoringRefusesPathsOutsideRoot(t *testing.T) {
	cs, root := newAuthoringClient(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.yaml")
	text := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: ok\nspec:\n  value: hi\n"
	if err := os.WriteFile(victim, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := []string{victim, "../" + filepath.Base(outside) + "/victim.yaml", "escape/victim.yaml"}
	calls := map[string]func(file string) map[string]any{
		"create_manifest": func(f string) map[string]any {
			return map[string]any{"kind": "Text", "name": "n" + strings.ReplaceAll(filepath.Base(filepath.Dir(f)), "-", ""), "spec": map[string]any{"value": "x"}, "file": f}
		},
		"write_manifest": func(f string) map[string]any { return map[string]any{"file": f, "yaml": text} },
		"edit_manifest": func(f string) map[string]any {
			return map[string]any{"file": f, "patch": map[string]any{"spec.value": "pwned"}}
		},
		"remove_manifest_fields": func(f string) map[string]any {
			return map[string]any{"file": f, "paths": []string{"spec.value"}}
		},
		"reorder_manifest_sequence": func(f string) map[string]any {
			return map[string]any{"file": f, "path": "spec.items", "from": 0, "to": 1}
		},
		"init_bundle": func(f string) map[string]any { return map[string]any{"directory": filepath.Dir(f)} },
	}
	for tool, args := range calls {
		for _, f := range bad {
			res := callTool(t, cs, tool, args(f))
			if !res.IsError {
				t.Errorf("%s accepted %q", tool, f)
			}
		}
	}
	if data, _ := os.ReadFile(victim); string(data) != text { //nolint:gosec // test file
		t.Errorf("file outside the root changed:\n%s", data)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 1 {
		t.Errorf("files appeared outside the root: %v", entries)
	}

	// write_manifest refuses new files outside, not only existing ones.
	for _, f := range []string{filepath.Join(outside, "new.yaml"), "../" + filepath.Base(outside) + "/new.yaml", "escape/new.yaml"} {
		if res := callTool(t, cs, "write_manifest", map[string]any{"file": f, "yaml": text}); !res.IsError {
			t.Errorf("write_manifest accepted new file %q", f)
		}
	}

	// An auto-placed manifest whose kind directory is a symlink out is refused.
	if err := os.Symlink(outside, filepath.Join(root, projectlayout.DirForKind("Text"))); err != nil {
		t.Fatal(err)
	}
	if res := callTool(t, cs, "create_manifest", map[string]any{"kind": "Text", "name": "auto", "spec": map[string]any{"value": "x"}}); !res.IsError {
		t.Error("create_manifest wrote through a symlinked kind directory")
	}

	// init_bundle does not write through a symlink already in the target.
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "bino.toml"), filepath.Join(bundle, "bino.toml")); err != nil {
		t.Fatal(err)
	}
	if res := callTool(t, cs, "init_bundle", map[string]any{"directory": "bundle", "force": true}); !res.IsError {
		t.Error("init_bundle wrote through a dangling symlink")
	}

	if data, _ := os.ReadFile(victim); string(data) != text { //nolint:gosec // test file
		t.Errorf("file outside the root changed:\n%s", data)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 1 {
		t.Errorf("files appeared outside the root: %v", entries)
	}

	// Inside the root, relative and absolute paths still work.
	for _, f := range []string{"in.yaml", filepath.Join(root, "abs.yaml")} {
		if res := callTool(t, cs, "write_manifest", map[string]any{"file": f, "yaml": text}); res.IsError {
			t.Errorf("write_manifest %q: %+v", f, res.Content)
		}
	}
	if res := callTool(t, cs, "edit_manifest", map[string]any{"file": "in.yaml", "patch": map[string]any{"spec.value": "ok"}}); res.IsError {
		t.Errorf("edit_manifest inside root: %+v", res.Content)
	}
}

// The JSON listing names exactly the tools a served session sees, in both modes.
func TestListToolsJSONMatchesServer(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	for _, readOnly := range []bool{false, true} {
		var buf bytes.Buffer
		if err := printMCPTools(ctx, &buf, root, false, readOnly, true); err != nil {
			t.Fatal(err)
		}
		var listing struct {
			Tools []struct {
				Name        string         `json:"name"`
				Title       string         `json:"title"`
				InputSchema map[string]any `json:"inputSchema"`
				Annotations map[string]any `json:"annotations"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(buf.Bytes(), &listing); err != nil {
			t.Fatalf("readOnly=%v: not JSON: %v", readOnly, err)
		}
		project := newLazyProject(ctx, logx.Nop(), root)
		deps := newStandaloneDeps(project, false)
		deps.Preview = daemon.NewPreviewProcess(root, logx.Nop())
		deps.ReadOnly = readOnly
		served, err := mcp.ListTools(ctx, mcp.NewServer(deps))
		project.close()
		if err != nil {
			t.Fatal(err)
		}
		if len(served) != len(listing.Tools) {
			t.Fatalf("readOnly=%v: listing has %d tools, server %d", readOnly, len(listing.Tools), len(served))
		}
		for i, tool := range served {
			got := listing.Tools[i]
			if got.Name != tool.Name || got.Title == "" || got.InputSchema == nil || got.Annotations == nil {
				t.Errorf("readOnly=%v: listing entry %d = %+v, server %s", readOnly, i, got, tool.Name)
			}
		}
	}
}

func TestDaemonReadOnlyFlags(t *testing.T) {
	t.Setenv(daemonTokenEnv, "")
	cmd := newDaemonCommand()
	cmd.SetArgs([]string{"--read-only", "-w", t.TempDir()})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), daemonTokenEnv) {
		t.Errorf("--read-only without %s: err = %v", daemonTokenEnv, err)
	}

	cmd = newDaemonCommand()
	cmd.SetArgs([]string{"--read-only", "--mcp-allow-publish"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Error("--read-only with --mcp-allow-publish was accepted")
	}
}

// A symlink out of the root that no template file goes through does not stop
// init_bundle.
func TestInitBundleIgnoresUnrelatedSymlinks(t *testing.T) {
	cs, root := newAuthoringClient(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "venv")); err != nil {
		t.Fatal(err)
	}
	if res := callTool(t, cs, "init_bundle", map[string]any{}); res.IsError {
		t.Fatalf("init_bundle: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "bino.toml")); err != nil {
		t.Error(err)
	}
}
