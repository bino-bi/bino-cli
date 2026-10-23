package cli

import (
	"io"
	"strings"
	"testing"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
)

// TestBuildAllArtefacts_ClearsPluginDatasets checks that each artefact starts
// with no host datasets, so a plugin never sees the previous artefact's data.
// Duplicate DataSet names make each artefact fail right after that point.
func TestBuildAllArtefacts_ClearsPluginDatasets(t *testing.T) {
	dupDocs := []config.Document{{Kind: "DataSet", Name: "dup"}, {Kind: "DataSet", Name: "dup"}}
	tests := []struct {
		name      string
		manifests *buildManifestData
	}{
		{"report artefact", &buildManifestData{
			Documents: dupDocs,
			Selected:  []config.Artifact{{Document: config.Document{Kind: "ReportArtefact", Name: "second"}}},
		}},
		{"screenshot artefact", &buildManifestData{
			Documents:           dupDocs,
			SelectedScreenshots: []config.ScreenshotArtefact{{Document: config.Document{Kind: "ScreenshotArtefact", Name: "second"}}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := plugin.NewBinoHostServer()
			host.SetDatasets([]plugin.DatasetPayload{{Name: "first", JSONRows: []byte(`[{"a":1}]`)}})

			out := NewOutput(OutputConfig{Stdout: io.Discard, Stderr: io.Discard, NoColor: true})
			_, err := buildAllArtefacts(t.Context(), out, tt.manifests, buildExecutionConfig{
				Builder:     &pipeline.Builder{Workdir: t.TempDir()},
				Logger:      logx.Nop(),
				OutputDir:   t.TempDir(),
				Stdout:      io.Discard,
				NoColor:     true,
				HostService: host,
			})
			if err == nil {
				t.Fatal("expected the artefact to fail on the duplicate name")
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("failed for another reason: %v", err)
			}

			resp, err := host.GetDatasetResult(t.Context(), &pluginv1.GetDatasetResultRequest{Name: "first"})
			if err != nil {
				t.Fatalf("GetDatasetResult: %v", err)
			}
			if resp.GetFound() {
				t.Fatal("the previous artefact's dataset is still visible")
			}
		})
	}
}
