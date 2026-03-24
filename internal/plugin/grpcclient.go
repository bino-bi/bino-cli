package plugin

import (
	"context"
	"errors"
	"io"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"
	goplugin "github.com/hashicorp/go-plugin"
)

// grpcClient implements the Plugin interface by delegating to the gRPC stub.
type grpcClient struct {
	client   pluginv1.BinoPluginClient
	manifest PluginManifest
	process  *goplugin.Client // for lifecycle management (kill)
}

func (g *grpcClient) Manifest() PluginManifest {
	return g.manifest
}

func (g *grpcClient) GetSchemas(ctx context.Context) (map[string][]byte, error) {
	resp, err := g.client.GetSchemas(ctx, &pluginv1.GetSchemasRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetSchemas(), nil
}

func (g *grpcClient) CollectDataSource(ctx context.Context, name string, rawSpec []byte, env map[string]string, projectRoot string) (*CollectResult, error) {
	resp, err := g.client.CollectDataSource(ctx, &pluginv1.CollectDataSourceRequest{
		Name:        name,
		RawSpec:     rawSpec,
		Env:         env,
		ProjectRoot: projectRoot,
	})
	if err != nil {
		return nil, err
	}
	return collectResultFromProto(resp), nil
}

func (g *grpcClient) Lint(ctx context.Context, docs []DocumentPayload, opts *LintOptions) ([]LintFinding, error) {
	req := &pluginv1.LintRequest{
		Documents: lintDocumentsToProto(docs),
	}
	if opts != nil {
		req.DatasetsAvailable = opts.DatasetsAvailable
		req.RenderedHtml = opts.RenderedHTML
		for _, ds := range opts.Datasets {
			req.Datasets = append(req.Datasets, &pluginv1.DatasetPayload{
				Name:     ds.Name,
				JsonRows: ds.JSONRows,
				Columns:  ds.Columns,
			})
		}
	}
	resp, err := g.client.Lint(ctx, req)
	if err != nil {
		return nil, err
	}
	return findingsFromProto(resp.GetFindings()), nil
}

func (g *grpcClient) GetAssets(ctx context.Context, renderMode string) (scripts []AssetFile, styles []AssetFile, err error) {
	resp, err := g.client.GetAssets(ctx, &pluginv1.GetAssetsRequest{
		RenderMode: renderMode,
	})
	if err != nil {
		return nil, nil, err
	}
	scripts, styles = assetsFromProto(resp)
	return scripts, styles, nil
}

func (g *grpcClient) ListCommands(ctx context.Context) ([]CommandDescriptor, error) {
	resp, err := g.client.ListCommands(ctx, &pluginv1.ListCommandsRequest{})
	if err != nil {
		return nil, err
	}
	return commandsFromProto(resp.GetCommands()), nil
}

func (g *grpcClient) ExecCommand(ctx context.Context, command string, args []string, flags map[string]string, workdir string, output func(stdout, stderr []byte)) (int, error) {
	stream, err := g.client.ExecCommand(ctx, &pluginv1.ExecCommandRequest{
		Command: command,
		Args:    args,
		Flags:   flags,
		Workdir: workdir,
	})
	if err != nil {
		return 1, err
	}
	var exitCode int32
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 1, err
		}
		if output != nil {
			output(msg.GetStdout(), msg.GetStderr())
		}
		if msg.GetIsFinal() {
			exitCode = msg.GetExitCode()
			break
		}
	}
	return int(exitCode), nil
}

func (g *grpcClient) OnHook(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error) {
	resp, err := g.client.OnHook(ctx, &pluginv1.HookRequest{
		Checkpoint: checkpoint,
		Payload:    hookPayloadToProto(payload),
	})
	if err != nil {
		return nil, err
	}
	return &HookResult{
		Modified:    resp.GetModified(),
		Payload:     hookPayloadFromProto(resp.GetPayload()),
		Diagnostics: diagnosticsFromProto(resp.GetDiagnostics()),
		Findings:    findingsFromProto(resp.GetFindings()),
	}, nil
}

func (g *grpcClient) RenderComponent(ctx context.Context, kind, name string, spec []byte, renderMode string) (string, error) {
	resp, err := g.client.RenderComponent(ctx, &pluginv1.RenderComponentRequest{
		Kind:       kind,
		Name:       name,
		Spec:       spec,
		RenderMode: renderMode,
	})
	if err != nil {
		return "", err
	}
	return resp.GetHtml(), nil
}

func (g *grpcClient) Shutdown(ctx context.Context) error {
	_, err := g.client.Shutdown(ctx, &pluginv1.ShutdownRequest{})
	// Kill the subprocess regardless of RPC result.
	g.process.Kill()
	return err
}
