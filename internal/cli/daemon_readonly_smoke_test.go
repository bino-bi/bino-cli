package cli

import (
	"bufio"
	"context"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
)

// TestDaemonReadOnlySmoke starts the real `bino daemon --read-only` on a tree
// without write permission, and on a writable one, and checks it serves the
// read tools and writes nothing. Skipped unless BINO_MCP_SMOKE points at a
// built binary.
func TestDaemonReadOnlySmoke(t *testing.T) {
	bin := os.Getenv("BINO_MCP_SMOKE")
	if bin == "" {
		t.Skip("set BINO_MCP_SMOKE to the bino binary path to run the daemon smoke test")
	}
	for _, writable := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only tree", true: "writable tree"}[writable], func(t *testing.T) {
			daemonReadOnlySmoke(t, bin, writable)
		})
	}
}

func daemonReadOnlySmoke(t *testing.T, bin string, writable bool) {
	src, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	copyTree(t, src, root)
	before := treeListing(t, root)
	if !writable {
		setTreeWritable(t, root, false)
		t.Cleanup(func() { setTreeWritable(t, root, true) })
	}

	port := freePort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const token = "smoke-token"
	cmd := exec.CommandContext(ctx, bin, "daemon", "-w", root, "--read-only", "--port", strconv.Itoa(port)) //nolint:gosec // test-controlled binary path
	cmd.Env = append(os.Environ(), daemonTokenEnv+"="+token, "TMPDIR="+t.TempDir())
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			t.Log(sc.Text())
		}
	}()

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitHealthy(ctx, t, base, token)

	// Another token is refused.
	if code := httpStatus(ctx, t, "GET", base+"/health", "other"); code != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", code)
	}
	// Write routes are refused.
	for _, route := range []string{"POST /build", "POST /preview/start", "POST /introspect-draft", "POST /preview-dataset"} {
		method, path, _ := strings.Cut(route, " ")
		if code := httpStatus(ctx, t, method, base+path, token); code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", route, code)
		}
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "smoke", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: daemon.TokenClient(token)}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = cs.Close() }()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only daemon serves %s, which is not read-only", tool.Name)
		}
	}

	vd := smokeCall(ctx, t, cs, "validate_draft", map[string]any{"yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: broken\n"})
	if !strings.Contains(vd, `"valid":false`) {
		t.Errorf("validate_draft should be invalid: %s", vd)
	}
	if res := smokeCallResult(ctx, t, cs, "validate_project", map[string]any{"execute_queries": true}); res.IsError {
		t.Errorf("validate_project: %s", smokeText(res))
	}
	project := smokeCall(ctx, t, cs, "describe_project", map[string]any{})
	if !strings.Contains(project, "DataSet") {
		t.Fatalf("describe_project found no DataSet: %s", project)
	}
	cols := smokeCallResult(ctx, t, cs, "get_columns", map[string]any{"name": firstDataSet(t, root)})
	t.Logf("get_columns: %s", smokeText(cols))
	if cols.IsError {
		t.Errorf("get_columns failed: %s", smokeText(cols))
	}

	if after := treeListing(t, root); after != before {
		t.Errorf("the tree changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func treeListing(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(rel + " " + strconv.FormatInt(info.Size(), 10) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func setTreeWritable(t *testing.T, root string, writable bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o444)
		if d.IsDir() {
			mode = 0o555
		}
		if writable {
			mode |= 0o200
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func httpStatus(ctx context.Context, t *testing.T, method, url, token string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func waitHealthy(ctx context.Context, t *testing.T, base, token string) {
	t.Helper()
	for ctx.Err() == nil {
		if httpStatus(ctx, t, "GET", base+"/health", token) == http.StatusOK {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("daemon did not become healthy")
}

// firstDataSet returns the name of a DataSet declared in the sample.
func firstDataSet(t *testing.T, root string) string {
	t.Helper()
	var name string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || name != "" || !strings.HasSuffix(path, ".yaml") {
			return nil //nolint:nilerr // best-effort scan
		}
		data, _ := os.ReadFile(path) //nolint:gosec // test file
		for _, doc := range strings.Split(string(data), "\n---") {
			if !strings.Contains(doc, "kind: DataSet") {
				continue
			}
			for _, line := range strings.Split(doc, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "name: "); ok {
					name = strings.Trim(v, `"'`)
					return nil
				}
			}
		}
		return nil
	})
	if name == "" {
		t.Fatal("no DataSet in the sample")
	}
	return name
}
