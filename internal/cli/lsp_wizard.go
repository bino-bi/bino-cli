package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/sqlgen"
	"bino.bi/bino/internal/schema"
	"bino.bi/bino/internal/version"
	"bino.bi/bino/pkg/duckdb"
)

// --- introspect-draft -------------------------------------------------------

type lspIntrospectResult struct {
	Version     string                   `json:"version"`
	Columns     []datasource.ProbeColumn `json:"columns"`
	Sheets      []string                 `json:"sheets,omitempty"`
	SampleRows  []map[string]any         `json:"sampleRows"`
	Truncated   bool                     `json:"truncated"`
	DetectedCSV *datasource.DetectedCSV  `json:"detectedCsv,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

func newLSPIntrospectDraftCommand() *cobra.Command {
	var (
		specFile string
		baseDir  string
		sheet    string
		limit    int
	)
	cmd := &cobra.Command{
		Use:   "introspect-draft <directory>",
		Short: "Introspect a not-yet-registered data source (schema, types, sample rows)",
		Long:  "Reads a DataSource spec (from --spec-file or stdin) and returns its columns, types, a row sample, detected CSV options and Excel sheet names. Used by the DataSource wizard.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specJSON, err := readFileOrStdin(cmd, specFile)
			if err != nil {
				return outputJSON(cmd.OutOrStdout(), lspIntrospectResult{
					Version: version.Version, Columns: []datasource.ProbeColumn{}, SampleRows: []map[string]any{},
					Error: fmt.Sprintf("read spec: %v", err),
				})
			}
			return runLSPIntrospectDraft(cmd.Context(), args[0], baseDir, specJSON, sheet, limit, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&specFile, "spec-file", "-", "Path to a JSON spec file, or '-' for stdin")
	cmd.Flags().StringVar(&baseDir, "base-dir", "", "Directory to resolve relative paths against (default: project root)")
	cmd.Flags().StringVar(&sheet, "sheet", "", "Excel sheet name to read")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of sample rows")
	return cmd
}

func runLSPIntrospectDraft(ctx context.Context, dir, baseDir string, specJSON []byte, sheet string, limit int, out io.Writer) error {
	result := lspIntrospectResult{Version: version.Version, Columns: []datasource.ProbeColumn{}, SampleRows: []map[string]any{}}

	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}
	if baseDir == "" {
		baseDir = absDir
	}

	// Sibling documents let database sources resolve their ConnectionSecret.
	docs, _ := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	defer session.Close()

	res, err := datasource.Probe(ctx, session, datasource.ProbeRequest{
		SpecJSON: specJSON,
		BaseDir:  baseDir,
		Docs:     docs,
		Sheet:    sheet,
		Limit:    limit,
	})
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	result.Columns = res.Columns
	result.Sheets = res.Sheets
	result.SampleRows = res.SampleRows
	result.Truncated = res.Truncated
	result.DetectedCSV = res.DetectedCSV
	return outputJSON(out, result)
}

// --- typed-select -----------------------------------------------------------

type lspTypedSelectResult struct {
	Version string   `json:"version"`
	SQL     string   `json:"sql"`
	Aliases []string `json:"aliases"`
	Error   string   `json:"error,omitempty"`
}

func newLSPTypedSelectCommand() *cobra.Command {
	var payloadFile string
	cmd := &cobra.Command{
		Use:   "typed-select",
		Short: "Generate a column-aware SELECT statement",
		Long:  "Reads a request {source, columns, pretty, castMode} (from --payload-file or stdin) and returns the generated SQL. Used by the wizard's live SQL preview as a daemon fallback.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payloadJSON, err := readFileOrStdin(cmd, payloadFile)
			if err != nil {
				return outputJSON(cmd.OutOrStdout(), lspTypedSelectResult{Version: version.Version, Error: fmt.Sprintf("read payload: %v", err)})
			}
			var req struct {
				Source   string          `json:"source"`
				Columns  []sqlgen.Column `json:"columns"`
				Pretty   bool            `json:"pretty"`
				CastMode string          `json:"castMode"`
			}
			if err := json.Unmarshal(payloadJSON, &req); err != nil {
				return outputJSON(cmd.OutOrStdout(), lspTypedSelectResult{Version: version.Version, Error: fmt.Sprintf("parse payload: %v", err)})
			}
			sql, aliases := sqlgen.TypedSelect(req.Source, req.Columns, sqlgen.Options{
				Pretty:   req.Pretty,
				CastMode: sqlgen.ParseCastMode(req.CastMode),
			})
			return outputJSON(cmd.OutOrStdout(), lspTypedSelectResult{Version: version.Version, SQL: sql, Aliases: aliases})
		},
	}
	cmd.Flags().StringVar(&payloadFile, "payload-file", "-", "Path to a JSON payload file, or '-' for stdin")
	return cmd
}

// --- preview-dataset --------------------------------------------------------

type lspPreviewDataSetResult struct {
	Version   string                   `json:"version"`
	Columns   []datasource.ProbeColumn `json:"columns"`
	Rows      []map[string]any         `json:"rows"`
	Truncated bool                     `json:"truncated"`
	Error     string                   `json:"error,omitempty"`
}

func newLSPPreviewDataSetCommand() *cobra.Command {
	var payloadFile string
	cmd := &cobra.Command{
		Use:   "preview-dataset <directory>",
		Short: "Run a draft DataSet SQL against a not-yet-registered DataSource",
		Long:  "Reads a request {spec, sourceName, sql, baseDir, sheet, limit} (from --payload-file or stdin), registers the draft DataSource as a view, runs the SQL, and returns a sample of result rows. Used by the wizard's \"Preview dataset\" action as a daemon fallback.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payloadJSON, err := readFileOrStdin(cmd, payloadFile)
			if err != nil {
				return outputJSON(cmd.OutOrStdout(), lspPreviewDataSetResult{Version: version.Version, Columns: []datasource.ProbeColumn{}, Rows: []map[string]any{}, Error: fmt.Sprintf("read payload: %v", err)})
			}
			return runLSPPreviewDataSet(cmd.Context(), args[0], payloadJSON, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&payloadFile, "payload-file", "-", "Path to a JSON payload file, or '-' for stdin")
	return cmd
}

func runLSPPreviewDataSet(ctx context.Context, dir string, payloadJSON []byte, out io.Writer) error {
	result := lspPreviewDataSetResult{Version: version.Version, Columns: []datasource.ProbeColumn{}, Rows: []map[string]any{}}

	var req struct {
		Spec       json.RawMessage `json:"spec"`
		SourceName string          `json:"sourceName"`
		SQL        string          `json:"sql"`
		BaseDir    string          `json:"baseDir"`
		Sheet      string          `json:"sheet"`
		Limit      int             `json:"limit"`
	}
	if err := json.Unmarshal(payloadJSON, &req); err != nil {
		result.Error = fmt.Sprintf("parse payload: %v", err)
		return outputJSON(out, result)
	}

	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}
	baseDir := req.BaseDir
	if baseDir == "" {
		baseDir = absDir
	}

	// Sibling documents let database sources resolve their ConnectionSecret.
	docs, _ := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	defer session.Close()

	res, err := datasource.PreviewDataSet(ctx, session, datasource.PreviewRequest{
		SpecJSON:   req.Spec,
		SourceName: req.SourceName,
		SQL:        req.SQL,
		BaseDir:    baseDir,
		Docs:       docs,
		Sheet:      req.Sheet,
		Limit:      req.Limit,
	})
	if err != nil {
		result.Error = err.Error()
		return outputJSON(out, result)
	}
	result.Columns = res.Columns
	result.Rows = res.Rows
	result.Truncated = res.Truncated
	return outputJSON(out, result)
}

// --- dataset-schema ---------------------------------------------------------

type lspDatasetSchemaResult struct {
	Version string                   `json:"version"`
	Columns []dataset.StandardColumn `json:"columns"`
}

func newLSPDatasetSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dataset-schema",
		Short: "Print the canonical dataset schema (standard columns)",
		Long:  "Returns the standard dataset columns (name, kind, group, pair) the wizard maps source columns onto. Static; needs no project directory.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return outputJSON(cmd.OutOrStdout(), lspDatasetSchemaResult{Version: version.Version, Columns: dataset.StandardColumns()})
		},
	}
}

// --- scaffold ---------------------------------------------------------------

type scaffoldConnection struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Schema   string `json:"schema"`
	User     string `json:"user"`
	Secret   string `json:"secret"`
}

type scaffoldDataSource struct {
	Name             string              `json:"name"`
	Description      string              `json:"description"`
	Type             string              `json:"type"`
	Path             string              `json:"path"`
	Sheet            string              `json:"sheet"`
	Delimiter        string              `json:"delimiter"`
	Header           *bool               `json:"header"`
	SkipRows         int                 `json:"skipRows"`
	Thousands        string              `json:"thousands"`
	DecimalSeparator string              `json:"decimalSeparator"`
	DateFormat       string              `json:"dateFormat"`
	Columns          map[string]string   `json:"columns"`
	Ephemeral        *bool               `json:"ephemeral"`
	Connection       *scaffoldConnection `json:"connection"`
	Query            string              `json:"query"`
}

type scaffoldDataSet struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	CastMode    string          `json:"castMode"`
	Pretty      bool            `json:"pretty"`
	Columns     []sqlgen.Column `json:"columns"`
	// SQL, when non-empty, is the user-edited query to write verbatim; the
	// column-based typed SELECT is only generated when it is blank.
	SQL string `json:"sql"`
}

type scaffoldPayload struct {
	DataSource scaffoldDataSource `json:"dataSource"`
	DataSet    *scaffoldDataSet   `json:"dataSet"`
}

type scaffoldFileResult struct {
	Path     string `json:"path"`
	Appended bool   `json:"appended"`
}

type lspScaffoldResult struct {
	OK      bool                 `json:"ok"`
	Files   []scaffoldFileResult `json:"files"`
	Error   string               `json:"error,omitempty"`
	Version string               `json:"version"`
}

func newLSPScaffoldCommand() *cobra.Command {
	var payloadFile string
	cmd := &cobra.Command{
		Use:   "scaffold <directory>",
		Short: "Create DataSource and DataSet manifests from a wizard payload",
		Long:  "Reads a scaffold payload (from --payload-file or stdin) and writes a DataSource (and optionally a typed-SELECT DataSet) manifest into the project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payloadJSON, err := readFileOrStdin(cmd, payloadFile)
			if err != nil {
				return outputJSON(cmd.OutOrStdout(), lspScaffoldResult{Version: version.Version, Files: []scaffoldFileResult{}, Error: fmt.Sprintf("read payload: %v", err)})
			}
			return runLSPScaffold(cmd.Context(), args[0], payloadJSON, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&payloadFile, "payload-file", "-", "Path to a JSON payload file, or '-' for stdin")
	return cmd
}

func runLSPScaffold(ctx context.Context, dir string, payloadJSON []byte, out io.Writer) error {
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		return outputJSON(out, lspScaffoldResult{Version: version.Version, Files: []scaffoldFileResult{}, Error: fmt.Sprintf("resolve project root: %v", err)})
	}

	var payload scaffoldPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return outputJSON(out, lspScaffoldResult{Version: version.Version, Files: []scaffoldFileResult{}, Error: fmt.Sprintf("parse payload: %v", err)})
	}

	return outputJSON(out, scaffoldManifests(ctx, absDir, payload))
}

// scaffoldManifests writes a DataSource (and optional DataSet) from a parsed
// payload into absDir, choosing file placement by project convention. It is
// shared by the lsp-helper `scaffold` command and the MCP scaffold_source tool.
func scaffoldManifests(ctx context.Context, absDir string, payload scaffoldPayload) lspScaffoldResult {
	result := lspScaffoldResult{Version: version.Version, Files: []scaffoldFileResult{}}

	if payload.DataSource.Name == "" {
		result.Error = "dataSource.name is required"
		return result
	}

	manifests, _ := ScanManifests(ctx, absDir)

	// --- DataSource ---
	dsPattern := DetectFilePattern(manifests, schema.KindDataSource)
	dsPath := SuggestOutputPath(dsPattern, payload.DataSource.Name, schema.KindDataSource)
	dsAppended := dsPattern.Mode == "multi-document"
	dsDoc := buildDataSourceDocument(dataSourceManifestFromPayload(payload.DataSource, filepath.Dir(filepath.Join(absDir, dsPath))))
	if err := WriteSchemaDocument(dsDoc, absDir, dsPath, dsAppended, io.Discard); err != nil {
		result.Error = fmt.Sprintf("write datasource: %v", err)
		return result
	}
	result.Files = append(result.Files, scaffoldFileResult{Path: dsPath, Appended: dsAppended})

	// --- DataSet (optional) ---
	if payload.DataSet != nil {
		// Honor an explicitly edited SQL from the wizard; otherwise regenerate
		// the typed SELECT from the column selection.
		query := strings.TrimSpace(payload.DataSet.SQL)
		if query == "" {
			query, _ = sqlgen.TypedSelect(payload.DataSource.Name, payload.DataSet.Columns, sqlgen.Options{
				Pretty:   payload.DataSet.Pretty,
				CastMode: sqlgen.ParseCastMode(payload.DataSet.CastMode),
			})
		}
		dsetDoc := buildDataSetDocument(DataSetManifestData{
			Name:         payload.DataSet.Name,
			Description:  payload.DataSet.Description,
			Query:        query,
			Dependencies: []string{payload.DataSource.Name},
		})
		dsetPattern := DetectFilePattern(manifests, schema.KindDataSet)
		dsetPath := SuggestOutputPath(dsetPattern, payload.DataSet.Name, schema.KindDataSet)
		dsetAppended := dsetPattern.Mode == "multi-document"
		if err := WriteSchemaDocument(dsetDoc, absDir, dsetPath, dsetAppended, io.Discard); err != nil {
			// The DataSource was written successfully; surface the error but keep it.
			result.Error = fmt.Sprintf("write dataset: %v", err)
			return result
		}
		result.Files = append(result.Files, scaffoldFileResult{Path: dsetPath, Appended: dsetAppended})
	}

	result.OK = true
	return result
}

// dataSourceManifestFromPayload converts a wizard payload into DataSourceManifestData,
// rewriting an absolute file path to be relative to the target manifest directory.
func dataSourceManifestFromPayload(ds scaffoldDataSource, manifestDir string) DataSourceManifestData {
	data := DataSourceManifestData{
		Name:                ds.Name,
		Description:         ds.Description,
		Type:                parseScaffoldType(ds.Type),
		Path:                relativeManifestPath(manifestDir, ds.Path),
		Sheet:               ds.Sheet,
		CSVDelimiter:        ds.Delimiter,
		CSVHeader:           ds.Header,
		CSVSkipRows:         ds.SkipRows,
		CSVThousands:        ds.Thousands,
		CSVDecimalSeparator: ds.DecimalSeparator,
		CSVDateFormat:       ds.DateFormat,
		Columns:             ds.Columns,
		Ephemeral:           ds.Ephemeral,
		DBQuery:             ds.Query,
	}
	if ds.Connection != nil {
		data.DBHost = ds.Connection.Host
		data.DBPort = ds.Connection.Port
		data.DBDatabase = ds.Connection.Database
		data.DBSchema = ds.Connection.Schema
		data.DBUser = ds.Connection.User
		data.DBSecret = ds.Connection.Secret
	}
	return data
}

// relativeManifestPath makes an absolute file path relative to the manifest
// directory so the written spec.path is portable. Non-absolute paths and URLs
// are returned unchanged.
func relativeManifestPath(manifestDir, path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	if rel, err := filepath.Rel(manifestDir, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

func parseScaffoldType(s string) DataSourceType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres_query":
		return DataSourceTypePostgres
	case "mysql_query":
		return DataSourceTypeMySQL
	default:
		return ParseDataSourceType(s)
	}
}

// readFileOrStdin reads from path, or from the command's stdin when path is "-".
func readFileOrStdin(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path) //nolint:gosec // G304: path supplied by the local IDE client
}
