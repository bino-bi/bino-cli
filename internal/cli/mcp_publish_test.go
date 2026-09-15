package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

// TestMain lets this test binary stand in for bino: registry_publish shells
// out to os.Executable(), which under `go test` is this binary, so a child
// started as `<test binary> publish ...` must run the real command the way
// cmd/bino/main.go does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "publish" {
		os.Exit(runAsBino())
	}
	os.Exit(m.Run())
}

func runAsBino() int {
	ctx := context.Background()
	if err := New().Execute(ctx); err != nil {
		msg, code := FormatError(ctx, err)
		fmt.Fprintf(os.Stderr, "bino: %s\n", msg)
		return code
	}
	return 0
}

// publishToolOutput is the registry_publish structured output; result reuses
// the CLI's own --json shape.
type publishToolOutput struct {
	Success  bool            `json:"success"`
	ExitCode int             `json:"exitCode"`
	Output   string          `json:"output"`
	Result   *publishOutcome `json:"result"`
}

// newPublishClient builds an MCP server with registry_publish enabled over a
// fresh predef project and returns a connected in-memory client. The test
// process is moved away from the project afterwards, so a publish that finds
// the project proves the tool selects it via cmd.Dir rather than the cwd.
func newPublishClient(t *testing.T, registryURL string, extra map[string]string) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	root := newPredefTestProject(t, registryURL, extra)
	t.Chdir(t.TempDir())
	state := newTestState(t, root)
	server := mcp.NewServer(mcp.Deps{State: state, AllowPublish: true})
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callPublish(t *testing.T, cs *mcpsdk.ClientSession, args map[string]any) (*mcpsdk.CallToolResult, publishToolOutput) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "registry_publish", Arguments: args})
	if err != nil {
		t.Fatalf("call registry_publish: %v", err)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out publishToolOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode structured content %s: %v", raw, err)
	}
	return res, out
}

// Without --bump the CLI refuses before any project or network work, so the
// registry must never see a request.
func TestRegistryPublishRequiresBumpWithoutDryRun(t *testing.T) {
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string { return `{}` })
	cs := newPublishClient(t, srv.URL, nil)

	res, out := callPublish(t, cs, map[string]any{})
	if !res.IsError {
		t.Fatalf("expected an error result, got %+v", out)
	}
	if text := toolText(t, res); !strings.Contains(text, "--bump") {
		t.Errorf("error text does not name --bump:\n%s", text)
	}
	if out.ExitCode != 1 {
		t.Errorf("exitCode = %d, want 1", out.ExitCode)
	}
	if capture.calls != 0 {
		t.Errorf("registry contacted %d time(s), want 0", capture.calls)
	}
}

func TestRegistryPublishDryRunMintsNothing(t *testing.T) {
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"dryRun":true,"digest":"sha256:d","version":"1.0.0","files":[{"path":"components/table.yaml","type":"document","digest":"sha256:a"}],"warnings":[]}`
	})
	cs := newPublishClient(t, srv.URL, nil)

	res, out := callPublish(t, cs, map[string]any{"dry_run": true})
	if res.IsError {
		t.Fatalf("registry_publish failed: %s", toolText(t, res))
	}
	if !out.Success || out.Result == nil || !out.Result.DryRun || out.Result.Version != "1.0.0" {
		t.Errorf("output = %+v", out)
	}
	if !capture.manifest.DryRun {
		t.Error("the registry was not asked for a dry run")
	}
}

// The registry's gate is the authority; its findings, as `bino publish`
// renders them, must reach the agent so it can fix the actual reason.
func TestRegistryPublishSurfacesGateFindings(t *testing.T) {
	srv, _ := fakePublishServer(t, http.StatusUnprocessableEntity, func(registry.PublishManifest) string {
		return `{"code":"schema_invalid","message":"document failed schema validation","details":[{"bino":"0.92.5","engine":"1.0.0","findings":[{"severity":"error","rule":"schema","message":"metadata.name is not allowed","file":"components/table.yaml","line":4}]}]}`
	})
	cs := newPublishClient(t, srv.URL, nil)

	res, out := callPublish(t, cs, map[string]any{"bump": "patch"})
	if !res.IsError {
		t.Fatalf("expected an error result, got %+v", out)
	}
	if out.ExitCode != 3 || out.Result != nil {
		t.Errorf("output = %+v, want exitCode 3 and no result", out)
	}
	text := toolText(t, res)
	for _, want := range []string{"metadata.name is not allowed", "components/table.yaml:4", "0.92.5"} {
		if !strings.Contains(text, want) {
			t.Errorf("error text does not mention %q:\n%s", want, text)
		}
		if !strings.Contains(out.Output, want) {
			t.Errorf("output does not mention %q:\n%s", want, out.Output)
		}
	}
}

func TestRegistryPublishUnchangedIsSuccess(t *testing.T) {
	srv, _ := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"package":"@acme/kit","version":"1.0.0","digest":"sha256:d","tag":"latest","kinds":["Table"],"unchanged":true,"files":[],"warnings":[]}`
	})
	cs := newPublishClient(t, srv.URL, nil)

	res, out := callPublish(t, cs, map[string]any{"bump": "patch"})
	if res.IsError {
		t.Fatalf("registry_publish failed: %s", toolText(t, res))
	}
	if !out.Success || out.ExitCode != 0 || out.Result == nil || !out.Result.Unchanged || out.Result.Version != "1.0.0" {
		t.Errorf("output = %+v", out)
	}
}

func TestRegistryPublishWithoutPackageTableIsActionable(t *testing.T) {
	cs := newPublishClient(t, "http://example.invalid", map[string]string{"bino.toml": "report-id = \"test\"\n"})

	res, out := callPublish(t, cs, map[string]any{"bump": "patch"})
	if !res.IsError {
		t.Fatalf("expected an error result, got %+v", out)
	}
	if out.ExitCode != 1 {
		t.Errorf("exitCode = %d, want 1", out.ExitCode)
	}
	text := toolText(t, res)
	for _, want := range []string{"[package]", "bino init predef"} {
		if !strings.Contains(text, want) {
			t.Errorf("error text does not mention %q:\n%s", want, text)
		}
	}
}
