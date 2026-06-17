// Shared types for the DataSource/DataSet wizard webview and its host.

export type FileFormat = 'csv' | 'excel' | 'parquet';
export type DbType = 'postgres_query' | 'mysql_query';

export interface FileSource {
    kind: 'file';
    format: FileFormat;
    absPath: string;
    fileName: string;
    // CSV options
    delimiter?: string;
    header?: boolean;
    skipRows?: number;
    // Excel
    sheet?: string;
    sheets?: string[];
}

export interface DbConnection {
    host: string;
    port: number;
    database: string;
    schema?: string;
    user: string;
    secret: string;
}

export interface DbSource {
    kind: 'database';
    dbType: DbType;
    connection: DbConnection;
    query: string;
}

export type SourceConfig = FileSource | DbSource;

export interface ProbeColumn {
    name: string;
    type: string;
}

export interface DetectedCSV {
    delimiter?: string;
    hasHeader?: boolean;
    skipRows?: number;
}

export interface IntrospectResult {
    columns: ProbeColumn[];
    sheets?: string[];
    sampleRows: Record<string, any>[];
    truncated: boolean;
    detectedCsv?: DetectedCSV;
    error?: string;
}

/** A generated output column for the DataSet SELECT — a mapped source column,
 *  a constant, or a raw expression (the schema-driven mapper builds these). */
export interface MappedColumn {
    /** Source column to read from. Omitted for constant/expression columns. */
    name?: string;
    /** Dataset column name (the output alias), e.g. "ac1", "category", "_region". */
    alias: string;
    /** Introspected source type (informational; decides if a cast is redundant). */
    type?: string;
    /** Cast type to apply to a source column. */
    targetType?: string;
    /** Raw SQL expression emitted verbatim (constant or expression columns). */
    expr?: string;
}

/** A standard dataset column (mirrors dataset.StandardColumn in Go). */
export interface StandardColumn {
    name: string;
    kind: 'number' | 'string';
    group: string;
    /** Partner column that must accompany this one (e.g. category <-> categoryIndex). */
    pair?: string;
}

// --- Host -> webview messages ---
export type HostMessage =
    | { type: 'init'; source: SourceConfig; dataSourceName: string; dataSetName: string; secrets: string[]; sampleRowLimit: number; datasetSchema: StandardColumn[] }
    | { type: 'introspectResult'; result: IntrospectResult }
    | { type: 'sql'; sql: string }
    | { type: 'datasetPreview'; columns: ProbeColumn[]; rows: Record<string, any>[]; truncated: boolean; error?: string }
    | { type: 'busy'; busy: boolean }
    | { type: 'created'; files: { path: string }[]; error?: string };

// --- Webview -> host messages ---
export type WebviewMessage =
    | { type: 'introspect'; source: SourceConfig }
    | { type: 'generateSql'; source: SourceConfig; dataSourceName: string; columns: MappedColumn[]; castMode: string }
    | { type: 'previewDataset'; source: SourceConfig; dataSourceName: string; sql: string }
    | { type: 'create'; payload: CreateRequest };

/** What the webview sends on Create; the host expands it into a scaffold payload. */
export interface CreateRequest {
    source: SourceConfig;
    dataSourceName: string;
    dataSetName: string;
    createDataSet: boolean;
    castMode: string;
    columns: MappedColumn[];
    /** User-edited DataSet SQL; when set, scaffolding writes it verbatim. */
    sql?: string;
}
