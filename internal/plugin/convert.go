package plugin

import (
	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"

	"bino.bi/bino/internal/report/config"
)

// manifestFromProto converts a protobuf PluginManifest to the Go type.
func manifestFromProto(pb *pluginv1.PluginManifest) PluginManifest {
	m := PluginManifest{
		Name:             pb.GetName(),
		Version:          pb.GetVersion(),
		Description:      pb.GetDescription(),
		DuckDBExtensions: pb.GetDuckdbExtensions(),
		ProvidesLinter:   pb.GetProvidesLinter(),
		ProvidesAssets:   pb.GetProvidesAssets(),
		Hooks:            pb.GetHooks(),
	}

	for _, k := range pb.GetKinds() {
		m.Kinds = append(m.Kinds, KindRegistration{
			KindName:       k.GetKindName(),
			Category:       kindCategoryFromProto(k.GetCategory()),
			DataSourceType: k.GetDatasourceType(),
		})
	}

	for _, c := range pb.GetCommands() {
		m.Commands = append(m.Commands, commandFromProto(c))
	}

	return m
}

// kindCategoryFromProto converts a protobuf KindCategory enum.
func kindCategoryFromProto(pb pluginv1.KindCategory) KindCategory {
	switch pb {
	case pluginv1.KindCategory_KIND_DATASOURCE:
		return KindCategoryDataSource
	case pluginv1.KindCategory_KIND_COMPONENT:
		return KindCategoryComponent
	case pluginv1.KindCategory_KIND_CONFIG:
		return KindCategoryConfig
	case pluginv1.KindCategory_KIND_ARTIFACT:
		return KindCategoryArtifact
	default:
		return KindCategoryComponent
	}
}

// severityFromProto converts a protobuf Severity enum.
func severityFromProto(pb pluginv1.Severity) Severity {
	switch pb {
	case pluginv1.Severity_WARNING:
		return SeverityWarning
	case pluginv1.Severity_ERROR:
		return SeverityError
	case pluginv1.Severity_INFO:
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// hookPayloadToProto converts a Go HookPayload to protobuf.
func hookPayloadToProto(hp *HookPayload) *pluginv1.HookPayload {
	if hp == nil {
		return nil
	}
	pb := &pluginv1.HookPayload{
		Html:     hp.HTML,
		PdfPath:  hp.PDFPath,
		Metadata: hp.Metadata,
	}
	for _, d := range hp.Documents {
		pb.Documents = append(pb.Documents, &pluginv1.DocumentPayload{
			File:     d.File,
			Position: int32(d.Position), //nolint:gosec // G115: document position is always small
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		})
	}
	for _, ds := range hp.Datasets {
		pb.Datasets = append(pb.Datasets, &pluginv1.DatasetPayload{
			Name:     ds.Name,
			JsonRows: ds.JSONRows,
			Columns:  ds.Columns,
		})
	}
	return pb
}

// hookPayloadFromProto converts a protobuf HookPayload to Go.
func hookPayloadFromProto(pb *pluginv1.HookPayload) *HookPayload {
	if pb == nil {
		return nil
	}
	hp := &HookPayload{
		HTML:     pb.GetHtml(),
		PDFPath:  pb.GetPdfPath(),
		Metadata: pb.GetMetadata(),
	}
	for _, d := range pb.GetDocuments() {
		hp.Documents = append(hp.Documents, DocumentPayload{
			File:     d.GetFile(),
			Position: int(d.GetPosition()),
			Kind:     d.GetKind(),
			Name:     d.GetName(),
			Raw:      d.GetRaw(),
		})
	}
	for _, ds := range pb.GetDatasets() {
		hp.Datasets = append(hp.Datasets, DatasetPayload{
			Name:     ds.GetName(),
			JSONRows: ds.GetJsonRows(),
			Columns:  ds.GetColumns(),
		})
	}
	return hp
}

// lintDocumentsToProto converts Go DocumentPayload slice to proto LintDocument slice.
func lintDocumentsToProto(docs []DocumentPayload) []*pluginv1.LintDocument {
	pbs := make([]*pluginv1.LintDocument, len(docs))
	for i, d := range docs {
		pbs[i] = &pluginv1.LintDocument{
			File:     d.File,
			Position: int32(d.Position), //nolint:gosec // G115: document position is always small
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		}
	}
	return pbs
}

// findingsFromProto converts proto LintFinding slice to Go.
func findingsFromProto(pbs []*pluginv1.LintFinding) []LintFinding {
	findings := make([]LintFinding, len(pbs))
	for i, pb := range pbs {
		findings[i] = LintFinding{
			RuleID:   pb.GetRuleId(),
			Message:  pb.GetMessage(),
			File:     pb.GetFile(),
			DocIdx:   int(pb.GetDocIdx()),
			Path:     pb.GetPath(),
			Line:     int(pb.GetLine()),
			Column:   int(pb.GetColumn()),
			Severity: severityFromProto(pb.GetSeverity()),
		}
	}
	return findings
}

// collectResultFromProto converts proto CollectDataSourceResponse to Go.
func collectResultFromProto(pb *pluginv1.CollectDataSourceResponse) *CollectResult {
	cr := &CollectResult{
		JSONRows:         pb.GetJsonRows(),
		ColumnTypes:      pb.GetColumnTypes(),
		Ephemeral:        pb.GetEphemeral(),
		DuckDBExpression: pb.GetDuckdbExpression(),
	}
	for _, d := range pb.GetDiagnostics() {
		cr.Diagnostics = append(cr.Diagnostics, diagnosticFromProto(d))
	}
	return cr
}

// assetsFromProto converts proto GetAssetsResponse to Go AssetFile slices.
func assetsFromProto(pb *pluginv1.GetAssetsResponse) (scripts, styles []AssetFile) {
	for _, s := range pb.GetScripts() {
		scripts = append(scripts, assetFromProto(s))
	}
	for _, s := range pb.GetStyles() {
		styles = append(styles, assetFromProto(s))
	}
	return scripts, styles
}

func assetFromProto(pb *pluginv1.AssetFile) AssetFile {
	return AssetFile{
		URLPath:   pb.GetUrlPath(),
		Content:   pb.GetContent(),
		FilePath:  pb.GetFilePath(),
		MediaType: pb.GetMediaType(),
		IsModule:  pb.GetIsModule(),
	}
}

func commandsFromProto(pbs []*pluginv1.CommandDescriptor) []CommandDescriptor {
	cmds := make([]CommandDescriptor, len(pbs))
	for i, pb := range pbs {
		cmds[i] = commandFromProto(pb)
	}
	return cmds
}

func commandFromProto(pb *pluginv1.CommandDescriptor) CommandDescriptor {
	cmd := CommandDescriptor{
		Name:  pb.GetName(),
		Short: pb.GetShort(),
		Long:  pb.GetLong(),
		Usage: pb.GetUsage(),
	}
	for _, f := range pb.GetFlags() {
		cmd.Flags = append(cmd.Flags, FlagDescriptor{
			Name:         f.GetName(),
			Shorthand:    f.GetShorthand(),
			Description:  f.GetDescription(),
			DefaultValue: f.GetDefaultValue(),
			Type:         f.GetType(),
			Required:     f.GetRequired(),
		})
	}
	return cmd
}

func diagnosticsFromProto(pbs []*pluginv1.Diagnostic) []Diagnostic {
	diags := make([]Diagnostic, len(pbs))
	for i, pb := range pbs {
		diags[i] = diagnosticFromProto(pb)
	}
	return diags
}

func diagnosticFromProto(pb *pluginv1.Diagnostic) Diagnostic {
	return Diagnostic{
		Source:   pb.GetSource(),
		Stage:    pb.GetStage(),
		Message:  pb.GetMessage(),
		Severity: severityFromProto(pb.GetSeverity()),
	}
}

// DocumentsFromConfig converts config.Document slice to plugin DocumentPayload slice.
func DocumentsFromConfig(docs []config.Document) []DocumentPayload {
	payloads := make([]DocumentPayload, len(docs))
	for i, d := range docs {
		payloads[i] = DocumentPayload{
			File:     d.File,
			Position: d.Position,
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		}
	}
	return payloads
}
