package plugin

import (
	"context"
	"net"
	"reflect"
	"sync"
	"testing"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakePluginServer is a scriptable implementation of the plugin side of the
// BinoPlugin gRPC service. Tests connect the production grpcClient to it over
// bufconn, so every call crosses a real gRPC transport (marshaling, status
// codes, streaming) instead of an in-process mock.
type fakePluginServer struct {
	pluginv1.UnimplementedBinoPluginServer

	mu sync.Mutex

	// Scripted responses.
	initResp     *pluginv1.PluginManifest
	schemasResp  *pluginv1.GetSchemasResponse
	collectResp  *pluginv1.CollectDataSourceResponse
	lintResp     *pluginv1.LintResponse
	assetsResp   *pluginv1.GetAssetsResponse
	commandsResp *pluginv1.ListCommandsResponse
	execChunks   []*pluginv1.ExecCommandOutput
	hookResp     *pluginv1.HookResponse
	renderResp   *pluginv1.RenderComponentResponse

	// errs maps an RPC name to an error the fake returns instead of its
	// scripted response. For ExecCommand the error is returned after all
	// scripted chunks have been streamed.
	errs map[string]error

	// Captured requests.
	gotInit        *pluginv1.InitRequest
	gotCollect     *pluginv1.CollectDataSourceRequest
	gotLint        *pluginv1.LintRequest
	gotAssets      *pluginv1.GetAssetsRequest
	gotExec        *pluginv1.ExecCommandRequest
	gotHook        *pluginv1.HookRequest
	gotRender      *pluginv1.RenderComponentRequest
	shutdownCalled bool
}

func newFakePluginServer() *fakePluginServer {
	return &fakePluginServer{
		initResp: &pluginv1.PluginManifest{Name: "fake", Version: "1.0.0"},
		errs:     map[string]error{},
	}
}

func (f *fakePluginServer) Init(_ context.Context, req *pluginv1.InitRequest) (*pluginv1.PluginManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotInit = req
	if err := f.errs["Init"]; err != nil {
		return nil, err
	}
	return f.initResp, nil
}

func (f *fakePluginServer) GetSchemas(context.Context, *pluginv1.GetSchemasRequest) (*pluginv1.GetSchemasResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs["GetSchemas"]; err != nil {
		return nil, err
	}
	return f.schemasResp, nil
}

func (f *fakePluginServer) CollectDataSource(_ context.Context, req *pluginv1.CollectDataSourceRequest) (*pluginv1.CollectDataSourceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotCollect = req
	if err := f.errs["CollectDataSource"]; err != nil {
		return nil, err
	}
	return f.collectResp, nil
}

func (f *fakePluginServer) Lint(_ context.Context, req *pluginv1.LintRequest) (*pluginv1.LintResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotLint = req
	if err := f.errs["Lint"]; err != nil {
		return nil, err
	}
	return f.lintResp, nil
}

func (f *fakePluginServer) GetAssets(_ context.Context, req *pluginv1.GetAssetsRequest) (*pluginv1.GetAssetsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotAssets = req
	if err := f.errs["GetAssets"]; err != nil {
		return nil, err
	}
	return f.assetsResp, nil
}

func (f *fakePluginServer) ListCommands(context.Context, *pluginv1.ListCommandsRequest) (*pluginv1.ListCommandsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs["ListCommands"]; err != nil {
		return nil, err
	}
	return f.commandsResp, nil
}

func (f *fakePluginServer) ExecCommand(req *pluginv1.ExecCommandRequest, stream grpc.ServerStreamingServer[pluginv1.ExecCommandOutput]) error {
	f.mu.Lock()
	f.gotExec = req
	chunks := f.execChunks
	err := f.errs["ExecCommand"]
	f.mu.Unlock()

	for _, c := range chunks {
		if sendErr := stream.Send(c); sendErr != nil {
			return sendErr
		}
	}
	return err
}

func (f *fakePluginServer) OnHook(_ context.Context, req *pluginv1.HookRequest) (*pluginv1.HookResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotHook = req
	if err := f.errs["OnHook"]; err != nil {
		return nil, err
	}
	return f.hookResp, nil
}

func (f *fakePluginServer) RenderComponent(_ context.Context, req *pluginv1.RenderComponentRequest) (*pluginv1.RenderComponentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotRender = req
	if err := f.errs["RenderComponent"]; err != nil {
		return nil, err
	}
	return f.renderResp, nil
}

func (f *fakePluginServer) Shutdown(context.Context, *pluginv1.ShutdownRequest) (*pluginv1.ShutdownResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalled = true
	if err := f.errs["Shutdown"]; err != nil {
		return nil, err
	}
	return &pluginv1.ShutdownResponse{}, nil
}

// newWireClient serves fake over bufconn and builds the production grpcClient
// against it the same way Manager.loadPlugin does: dial, Init over the wire,
// convert the returned manifest via manifestFromProto. The go-plugin process
// handle is a never-started client, so Shutdown's Kill() is a harmless no-op.
func newWireClient(t *testing.T, fake *fakePluginServer) *grpcClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pluginv1.RegisterBinoPluginServer(srv, fake)
	go func() {
		_ = srv.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})

	stub := pluginv1.NewBinoPluginClient(conn)
	manifest, err := stub.Init(t.Context(), &pluginv1.InitRequest{BinoVersion: "0.0.0-test"})
	if err != nil {
		t.Fatalf("Init over bufconn: %v", err)
	}

	return &grpcClient{
		client:   stub,
		manifest: manifestFromProto(manifest),
		process:  goplugin.NewClient(&goplugin.ClientConfig{}),
	}
}

func TestGRPCClient_ManifestOverWire(t *testing.T) {
	fake := newFakePluginServer()
	fake.initResp = &pluginv1.PluginManifest{
		Name:             "salesforce",
		Version:          "1.2.3",
		Description:      "SOQL data sources",
		DuckdbExtensions: []string{"httpfs", "spatial"},
		ProvidesLinter:   true,
		ProvidesAssets:   true,
		Hooks:            []string{"post-load", "pre-render-html"},
		Kinds: []*pluginv1.KindRegistration{
			{KindName: "SfDataSource", Category: pluginv1.KindCategory_KIND_DATASOURCE, DatasourceType: "sf_soql"},
			{KindName: "SfChart", Category: pluginv1.KindCategory_KIND_COMPONENT},
			{KindName: "SfConfig", Category: pluginv1.KindCategory_KIND_CONFIG},
			{KindName: "SfExport", Category: pluginv1.KindCategory_KIND_ARTIFACT},
		},
		Commands: []*pluginv1.CommandDescriptor{{
			Name:  "sf:auth",
			Short: "Authenticate",
			Long:  "Authenticate against Salesforce.",
			Usage: "sf:auth [flags]",
			Flags: []*pluginv1.FlagDescriptor{{
				Name:         "profile",
				Shorthand:    "p",
				Description:  "auth profile",
				DefaultValue: "default",
				Type:         "string",
				Required:     true,
			}},
		}},
	}

	client := newWireClient(t, fake)

	want := PluginManifest{
		Name:             "salesforce",
		Version:          "1.2.3",
		Description:      "SOQL data sources",
		DuckDBExtensions: []string{"httpfs", "spatial"},
		ProvidesLinter:   true,
		ProvidesAssets:   true,
		Hooks:            []string{"post-load", "pre-render-html"},
		Kinds: []KindRegistration{
			{KindName: "SfDataSource", Category: KindCategoryDataSource, DataSourceType: "sf_soql"},
			{KindName: "SfChart", Category: KindCategoryComponent},
			{KindName: "SfConfig", Category: KindCategoryConfig},
			{KindName: "SfExport", Category: KindCategoryArtifact},
		},
		Commands: []CommandDescriptor{{
			Name:  "sf:auth",
			Short: "Authenticate",
			Long:  "Authenticate against Salesforce.",
			Usage: "sf:auth [flags]",
			Flags: []FlagDescriptor{{
				Name:         "profile",
				Shorthand:    "p",
				Description:  "auth profile",
				DefaultValue: "default",
				Type:         "string",
				Required:     true,
			}},
		}},
	}
	if got := client.Manifest(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest() mismatch:\n got %+v\nwant %+v", got, want)
	}

	fake.mu.Lock()
	gotInit := fake.gotInit
	fake.mu.Unlock()
	if gotInit.GetBinoVersion() != "0.0.0-test" {
		t.Fatalf("Init request BinoVersion = %q, want %q", gotInit.GetBinoVersion(), "0.0.0-test")
	}
}

func TestGRPCClient_GetSchemas(t *testing.T) {
	fake := newFakePluginServer()
	fake.schemasResp = &pluginv1.GetSchemasResponse{
		Schemas: map[string][]byte{
			"SfDataSource": []byte(`{"type":"object"}`),
			"SfChart":      []byte(`{"type":"object","required":["dataset"]}`),
		},
	}
	client := newWireClient(t, fake)

	got, err := client.GetSchemas(t.Context())
	if err != nil {
		t.Fatalf("GetSchemas: %v", err)
	}
	want := map[string][]byte{
		"SfDataSource": []byte(`{"type":"object"}`),
		"SfChart":      []byte(`{"type":"object","required":["dataset"]}`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSchemas() = %v, want %v", got, want)
	}
}

func TestGRPCClient_CollectDataSource(t *testing.T) {
	fake := newFakePluginServer()
	fake.collectResp = &pluginv1.CollectDataSourceResponse{
		JsonRows:    []byte(`[{"id":"001","amount":42}]`),
		ColumnTypes: map[string]string{"id": "VARCHAR", "amount": "INTEGER"},
		Ephemeral:   true,
		Diagnostics: []*pluginv1.Diagnostic{{
			Source:   "salesforce",
			Stage:    "collect",
			Message:  "API quota at 80%",
			Severity: pluginv1.Severity_INFO,
		}},
		DuckdbExpression: "read_parquet('cache.parquet')",
	}
	client := newWireClient(t, fake)

	got, err := client.CollectDataSource(t.Context(), "opps", []byte(`{"type":"sf_soql"}`), map[string]string{"SF_TOKEN": "xyz"}, "/proj")
	if err != nil {
		t.Fatalf("CollectDataSource: %v", err)
	}
	want := &CollectResult{
		JSONRows:         []byte(`[{"id":"001","amount":42}]`),
		ColumnTypes:      map[string]string{"id": "VARCHAR", "amount": "INTEGER"},
		Ephemeral:        true,
		Diagnostics:      []Diagnostic{{Source: "salesforce", Stage: "collect", Message: "API quota at 80%", Severity: SeverityInfo}},
		DuckDBExpression: "read_parquet('cache.parquet')",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectDataSource() mismatch:\n got %+v\nwant %+v", got, want)
	}

	fake.mu.Lock()
	req := fake.gotCollect
	fake.mu.Unlock()
	if req.GetName() != "opps" {
		t.Fatalf("request name = %q, want %q", req.GetName(), "opps")
	}
	if string(req.GetRawSpec()) != `{"type":"sf_soql"}` {
		t.Fatalf("request raw spec = %s", req.GetRawSpec())
	}
	if !reflect.DeepEqual(req.GetEnv(), map[string]string{"SF_TOKEN": "xyz"}) {
		t.Fatalf("request env = %v", req.GetEnv())
	}
	if req.GetProjectRoot() != "/proj" {
		t.Fatalf("request project root = %q, want %q", req.GetProjectRoot(), "/proj")
	}
}

func TestGRPCClient_Lint(t *testing.T) {
	docs := []DocumentPayload{
		{File: "a.yaml", Position: 1, Kind: "DataSet", Name: "rev", Raw: []byte(`{"kind":"DataSet"}`)},
		{File: "b.yaml", Position: 3, Kind: "Table", Name: "tbl", Raw: []byte(`{"kind":"Table"}`)},
	}

	t.Run("documents and options cross the wire", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.lintResp = &pluginv1.LintResponse{
			Findings: []*pluginv1.LintFinding{
				{RuleId: "sf/field-access", Message: "unknown field", File: "a.yaml", DocIdx: 1, Path: "spec.query", Line: 12, Column: 5, Severity: pluginv1.Severity_ERROR},
				{RuleId: "sf/naming", Message: "prefer lowercase", File: "b.yaml", DocIdx: 3, Path: "metadata.name", Line: 2, Column: 9, Severity: pluginv1.Severity_WARNING},
			},
		}
		client := newWireClient(t, fake)

		opts := &LintOptions{
			Datasets:          []DatasetPayload{{Name: "rev", JSONRows: []byte(`[{"a":1}]`), Columns: []string{"a"}}},
			DatasetsAvailable: true,
			RenderedHTML:      []byte("<html></html>"),
		}
		got, err := client.Lint(t.Context(), docs, opts)
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		want := []LintFinding{
			{RuleID: "sf/field-access", Message: "unknown field", File: "a.yaml", DocIdx: 1, Path: "spec.query", Line: 12, Column: 5, Severity: SeverityError},
			{RuleID: "sf/naming", Message: "prefer lowercase", File: "b.yaml", DocIdx: 3, Path: "metadata.name", Line: 2, Column: 9, Severity: SeverityWarning},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Lint() mismatch:\n got %+v\nwant %+v", got, want)
		}

		fake.mu.Lock()
		req := fake.gotLint
		fake.mu.Unlock()
		if len(req.GetDocuments()) != 2 {
			t.Fatalf("expected 2 documents on the wire, got %d", len(req.GetDocuments()))
		}
		d := req.GetDocuments()[0]
		if d.GetFile() != "a.yaml" || d.GetPosition() != 1 || d.GetKind() != "DataSet" || d.GetName() != "rev" || string(d.GetRaw()) != `{"kind":"DataSet"}` {
			t.Fatalf("document 0 mangled in transit: %+v", d)
		}
		if !req.GetDatasetsAvailable() {
			t.Fatal("DatasetsAvailable flag lost in transit")
		}
		if len(req.GetDatasets()) != 1 || req.GetDatasets()[0].GetName() != "rev" || string(req.GetDatasets()[0].GetJsonRows()) != `[{"a":1}]` {
			t.Fatalf("datasets mangled in transit: %+v", req.GetDatasets())
		}
		if string(req.GetRenderedHtml()) != "<html></html>" {
			t.Fatalf("rendered HTML mangled in transit: %s", req.GetRenderedHtml())
		}
	})

	t.Run("nil options send an empty request tail", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.lintResp = &pluginv1.LintResponse{}
		client := newWireClient(t, fake)

		got, err := client.Lint(t.Context(), docs, nil)
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected no findings, got %+v", got)
		}

		fake.mu.Lock()
		req := fake.gotLint
		fake.mu.Unlock()
		if req.GetDatasetsAvailable() || len(req.GetDatasets()) != 0 || len(req.GetRenderedHtml()) != 0 {
			t.Fatalf("nil options should leave the optional fields empty, got %+v", req)
		}
	})
}

func TestGRPCClient_GetAssets(t *testing.T) {
	fake := newFakePluginServer()
	fake.assetsResp = &pluginv1.GetAssetsResponse{
		Scripts: []*pluginv1.AssetFile{{
			UrlPath:   "/plugins/sf/chart.js",
			Content:   []byte("export default 1;"),
			MediaType: "text/javascript",
			IsModule:  true,
		}},
		Styles: []*pluginv1.AssetFile{{
			UrlPath:  "/plugins/sf/chart.css",
			FilePath: "/opt/sf/chart.css",
		}},
	}
	client := newWireClient(t, fake)

	scripts, styles, err := client.GetAssets(t.Context(), "preview")
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	wantScripts := []AssetFile{{
		URLPath:   "/plugins/sf/chart.js",
		Content:   []byte("export default 1;"),
		MediaType: "text/javascript",
		IsModule:  true,
	}}
	wantStyles := []AssetFile{{
		URLPath:  "/plugins/sf/chart.css",
		FilePath: "/opt/sf/chart.css",
	}}
	if !reflect.DeepEqual(scripts, wantScripts) {
		t.Fatalf("scripts mismatch:\n got %+v\nwant %+v", scripts, wantScripts)
	}
	if !reflect.DeepEqual(styles, wantStyles) {
		t.Fatalf("styles mismatch:\n got %+v\nwant %+v", styles, wantStyles)
	}

	fake.mu.Lock()
	req := fake.gotAssets
	fake.mu.Unlock()
	if req.GetRenderMode() != "preview" {
		t.Fatalf("render mode = %q, want %q", req.GetRenderMode(), "preview")
	}
}

func TestGRPCClient_ListCommands(t *testing.T) {
	fake := newFakePluginServer()
	fake.commandsResp = &pluginv1.ListCommandsResponse{
		Commands: []*pluginv1.CommandDescriptor{{
			Name:  "sf:export",
			Short: "Export data",
			Usage: "sf:export [flags]",
			Flags: []*pluginv1.FlagDescriptor{{Name: "format", Type: "string", DefaultValue: "csv"}},
		}},
	}
	client := newWireClient(t, fake)

	got, err := client.ListCommands(t.Context())
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	want := []CommandDescriptor{{
		Name:  "sf:export",
		Short: "Export data",
		Usage: "sf:export [flags]",
		Flags: []FlagDescriptor{{Name: "format", Type: "string", DefaultValue: "csv"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCommands() mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestGRPCClient_ExecCommand(t *testing.T) {
	type outputCall struct {
		stdout string
		stderr string
	}
	collect := func(calls *[]outputCall) func(stdout, stderr []byte) {
		return func(stdout, stderr []byte) {
			*calls = append(*calls, outputCall{stdout: string(stdout), stderr: string(stderr)})
		}
	}

	t.Run("streams chunks in order and returns the final exit code", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.execChunks = []*pluginv1.ExecCommandOutput{
			{Stdout: []byte("building...\n")},
			{Stderr: []byte("warn: slow\n")},
			{IsFinal: true, ExitCode: 3},
		}
		client := newWireClient(t, fake)

		var calls []outputCall
		code, err := client.ExecCommand(t.Context(), "sf:export", []string{"opps"}, map[string]string{"format": "csv"}, "/work", collect(&calls))
		if err != nil {
			t.Fatalf("ExecCommand: %v", err)
		}
		if code != 3 {
			t.Fatalf("exit code = %d, want 3", code)
		}
		wantCalls := []outputCall{
			{stdout: "building...\n"},
			{stderr: "warn: slow\n"},
			{}, // final message still reaches the callback, with empty chunks
		}
		if !reflect.DeepEqual(calls, wantCalls) {
			t.Fatalf("output calls mismatch:\n got %+v\nwant %+v", calls, wantCalls)
		}

		fake.mu.Lock()
		req := fake.gotExec
		fake.mu.Unlock()
		if req.GetCommand() != "sf:export" || !reflect.DeepEqual(req.GetArgs(), []string{"opps"}) || req.GetWorkdir() != "/work" {
			t.Fatalf("request mangled in transit: %+v", req)
		}
		if !reflect.DeepEqual(req.GetFlags(), map[string]string{"format": "csv"}) {
			t.Fatalf("flags mangled in transit: %v", req.GetFlags())
		}
	})

	t.Run("stream ending without a final message yields exit code 0", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.execChunks = []*pluginv1.ExecCommandOutput{{Stdout: []byte("done\n")}}
		client := newWireClient(t, fake)

		var calls []outputCall
		code, err := client.ExecCommand(t.Context(), "sf:export", nil, nil, "", collect(&calls))
		if err != nil {
			t.Fatalf("ExecCommand: %v", err)
		}
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if len(calls) != 1 || calls[0].stdout != "done\n" {
			t.Fatalf("output calls = %+v", calls)
		}
	})

	t.Run("server error mid-stream propagates after partial output", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.execChunks = []*pluginv1.ExecCommandOutput{{Stdout: []byte("partial")}}
		fake.errs["ExecCommand"] = status.Error(codes.Internal, "command crashed")
		client := newWireClient(t, fake)

		var calls []outputCall
		code, err := client.ExecCommand(t.Context(), "sf:export", nil, nil, "", collect(&calls))
		if err == nil {
			t.Fatal("expected the stream error to propagate")
		}
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if len(calls) != 1 || calls[0].stdout != "partial" {
			t.Fatalf("expected the partial chunk before the error, got %+v", calls)
		}
	})

	t.Run("nil output callback is tolerated", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.execChunks = []*pluginv1.ExecCommandOutput{
			{Stdout: []byte("ignored")},
			{IsFinal: true, ExitCode: 2},
		}
		client := newWireClient(t, fake)

		code, err := client.ExecCommand(t.Context(), "sf:export", nil, nil, "", nil)
		if err != nil {
			t.Fatalf("ExecCommand: %v", err)
		}
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})
}

func TestGRPCClient_OnHook(t *testing.T) {
	t.Run("payload round-trips and the response converts", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.hookResp = &pluginv1.HookResponse{
			Modified: true,
			Payload: &pluginv1.HookPayload{
				Html:     []byte("<html>modified</html>"),
				Metadata: map[string]string{"touched": "yes"},
			},
			Diagnostics: []*pluginv1.Diagnostic{{
				Source: "sf", Stage: "hook", Message: "rewrote html", Severity: pluginv1.Severity_INFO,
			}},
			Findings: []*pluginv1.LintFinding{{
				RuleId: "sf/hook", Message: "flagged", Severity: pluginv1.Severity_ERROR,
			}},
		}
		client := newWireClient(t, fake)

		payload := &HookPayload{
			Documents: []DocumentPayload{{File: "a.yaml", Position: 2, Kind: "DataSet", Name: "rev", Raw: []byte(`{"x":1}`)}},
			HTML:      []byte("<html>original</html>"),
			PDFPath:   "/out/report.pdf",
			Datasets:  []DatasetPayload{{Name: "rev", JSONRows: []byte(`[{"x":1}]`), Columns: []string{"x"}}},
			Metadata:  map[string]string{"artefact": "sales"},
		}
		got, err := client.OnHook(t.Context(), "pre-render-html", payload)
		if err != nil {
			t.Fatalf("OnHook: %v", err)
		}
		want := &HookResult{
			Modified: true,
			Payload: &HookPayload{
				HTML:     []byte("<html>modified</html>"),
				Metadata: map[string]string{"touched": "yes"},
			},
			Diagnostics: []Diagnostic{{Source: "sf", Stage: "hook", Message: "rewrote html", Severity: SeverityInfo}},
			Findings:    []LintFinding{{RuleID: "sf/hook", Message: "flagged", Severity: SeverityError}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("OnHook() mismatch:\n got %+v\nwant %+v", got, want)
		}

		fake.mu.Lock()
		req := fake.gotHook
		fake.mu.Unlock()
		if req.GetCheckpoint() != "pre-render-html" {
			t.Fatalf("checkpoint = %q, want %q", req.GetCheckpoint(), "pre-render-html")
		}
		pb := req.GetPayload()
		if len(pb.GetDocuments()) != 1 || pb.GetDocuments()[0].GetName() != "rev" || pb.GetDocuments()[0].GetPosition() != 2 {
			t.Fatalf("documents mangled in transit: %+v", pb.GetDocuments())
		}
		if string(pb.GetHtml()) != "<html>original</html>" || pb.GetPdfPath() != "/out/report.pdf" {
			t.Fatalf("html/pdf mangled in transit: %+v", pb)
		}
		if len(pb.GetDatasets()) != 1 || !reflect.DeepEqual(pb.GetDatasets()[0].GetColumns(), []string{"x"}) {
			t.Fatalf("datasets mangled in transit: %+v", pb.GetDatasets())
		}
		if !reflect.DeepEqual(pb.GetMetadata(), map[string]string{"artefact": "sales"}) {
			t.Fatalf("metadata mangled in transit: %v", pb.GetMetadata())
		}
	})

	t.Run("nil payload stays nil in both directions", func(t *testing.T) {
		fake := newFakePluginServer()
		fake.hookResp = &pluginv1.HookResponse{Modified: false}
		client := newWireClient(t, fake)

		got, err := client.OnHook(t.Context(), "post-load", nil)
		if err != nil {
			t.Fatalf("OnHook: %v", err)
		}
		if got.Modified || got.Payload != nil {
			t.Fatalf("expected unmodified nil payload, got %+v", got)
		}

		fake.mu.Lock()
		req := fake.gotHook
		fake.mu.Unlock()
		if req.GetPayload() != nil {
			t.Fatalf("expected nil payload on the wire, got %+v", req.GetPayload())
		}
	})
}

func TestGRPCClient_RenderComponent(t *testing.T) {
	fake := newFakePluginServer()
	fake.renderResp = &pluginv1.RenderComponentResponse{
		Html: `<bn-example-chart data="[1,2]"></bn-example-chart>`,
	}
	client := newWireClient(t, fake)

	got, err := client.RenderComponent(t.Context(), "ExampleChart", "sales", []byte(`{"dataset":"rev"}`), "build")
	if err != nil {
		t.Fatalf("RenderComponent: %v", err)
	}
	if want := `<bn-example-chart data="[1,2]"></bn-example-chart>`; got != want {
		t.Fatalf("RenderComponent() = %q, want %q", got, want)
	}

	fake.mu.Lock()
	req := fake.gotRender
	fake.mu.Unlock()
	if req.GetKind() != "ExampleChart" || req.GetName() != "sales" || req.GetRenderMode() != "build" {
		t.Fatalf("request mangled in transit: %+v", req)
	}
	if string(req.GetSpec()) != `{"dataset":"rev"}` {
		t.Fatalf("spec mangled in transit: %s", req.GetSpec())
	}
}

func TestGRPCClient_Shutdown(t *testing.T) {
	fake := newFakePluginServer()
	client := newWireClient(t, fake)

	if err := client.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	fake.mu.Lock()
	called := fake.shutdownCalled
	fake.mu.Unlock()
	if !called {
		t.Fatal("Shutdown RPC never reached the plugin")
	}
}

// TestGRPCClient_ErrorPropagation drives every RPC against a server that
// fails it and asserts the gRPC status error reaches the caller instead of
// being swallowed into a zero-value result.
func TestGRPCClient_ErrorPropagation(t *testing.T) {
	tests := []struct {
		rpc  string
		call func(ctx context.Context, c *grpcClient) error
	}{
		{"GetSchemas", func(ctx context.Context, c *grpcClient) error {
			_, err := c.GetSchemas(ctx)
			return err
		}},
		{"CollectDataSource", func(ctx context.Context, c *grpcClient) error {
			_, err := c.CollectDataSource(ctx, "n", nil, nil, "")
			return err
		}},
		{"Lint", func(ctx context.Context, c *grpcClient) error {
			_, err := c.Lint(ctx, nil, nil)
			return err
		}},
		{"GetAssets", func(ctx context.Context, c *grpcClient) error {
			_, _, err := c.GetAssets(ctx, "build")
			return err
		}},
		{"ListCommands", func(ctx context.Context, c *grpcClient) error {
			_, err := c.ListCommands(ctx)
			return err
		}},
		{"ExecCommand", func(ctx context.Context, c *grpcClient) error {
			_, err := c.ExecCommand(ctx, "x", nil, nil, "", nil)
			return err
		}},
		{"OnHook", func(ctx context.Context, c *grpcClient) error {
			_, err := c.OnHook(ctx, "post-load", nil)
			return err
		}},
		{"RenderComponent", func(ctx context.Context, c *grpcClient) error {
			_, err := c.RenderComponent(ctx, "K", "n", nil, "build")
			return err
		}},
		{"Shutdown", func(ctx context.Context, c *grpcClient) error {
			return c.Shutdown(ctx)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.rpc, func(t *testing.T) {
			fake := newFakePluginServer()
			fake.errs[tt.rpc] = status.Error(codes.Internal, "plugin exploded")
			client := newWireClient(t, fake)

			err := tt.call(t.Context(), client)
			if err == nil {
				t.Fatalf("%s: expected the server error to propagate, got nil", tt.rpc)
			}
			if got := status.Code(err); got != codes.Internal {
				t.Fatalf("%s: status code = %v, want %v", tt.rpc, got, codes.Internal)
			}
		})
	}
}
