import * as path from 'path';
import * as vscode from 'vscode';
import { WorkspaceIndexer } from '../indexer';
import { getWizardHtml } from './wizardHtml';
import { fileSourceFromUri } from './sourceFromUri';
import { CreateRequest, DbSource, HostMessage, SourceConfig, WebviewMessage } from './wizardTypes';

/**
 * DataSourceWizardManager owns a single webview that walks the user from a data
 * source (a right-clicked file or a database connection) to scaffolded
 * DataSource + DataSet manifests, with live schema introspection and a
 * server-generated typed SELECT preview.
 */
export class DataSourceWizardManager {
    private panel: vscode.WebviewPanel | undefined;

    constructor(
        private readonly indexer: WorkspaceIndexer,
        private readonly outputChannel: vscode.OutputChannel,
    ) {}

    /** Open the wizard for a right-clicked CSV/Excel/Parquet file. */
    openForFile(uri: vscode.Uri): void {
        const source = fileSourceFromUri(uri);
        if (!source) {
            vscode.window.showErrorMessage(`Bino: unsupported file type for "${path.basename(uri.fsPath)}".`);
            return;
        }
        void this.open(source);
    }

    /** Open the wizard for a new database DataSource. */
    openForDatabase(): void {
        const source: DbSource = {
            kind: 'database',
            dbType: 'postgres_query',
            connection: { host: 'localhost', port: 5432, database: '', user: '', secret: '' },
            query: '',
        };
        void this.open(source);
    }

    private async open(source: SourceConfig): Promise<void> {
        const title = source.kind === 'file' ? `Wizard: ${source.fileName}` : 'Wizard: New DataSource';
        if (this.panel) {
            this.panel.title = title;
            this.panel.reveal(vscode.ViewColumn.Active);
        } else {
            this.panel = vscode.window.createWebviewPanel(
                'binoDataSourceWizard',
                title,
                vscode.ViewColumn.Active,
                { enableScripts: true, retainContextWhenHidden: true },
            );
            this.panel.onDidDispose(() => { this.panel = undefined; });
            this.panel.webview.onDidReceiveMessage((msg: WebviewMessage) => this.handleMessage(msg));
        }

        this.panel.webview.html = getWizardHtml();

        const config = vscode.workspace.getConfiguration('bino');
        const sampleRowLimit = config.get<number>('wizard.sampleRowLimit') ?? 100;
        const secrets = this.indexer.getDocuments(['ConnectionSecret']).map((d) => d.name);
        const base = source.kind === 'file' ? source.fileName : 'query';
        const datasetSchema = await this.indexer.datasetSchema();

        this.post({
            type: 'init',
            source,
            dataSourceName: this.uniqueName(`${suggestNameFrom(base)}_src`, 'DataSource'),
            dataSetName: this.uniqueName(suggestNameFrom(base), 'DataSet'),
            secrets,
            sampleRowLimit,
            datasetSchema,
        });
    }

    private async handleMessage(msg: WebviewMessage): Promise<void> {
        switch (msg.type) {
            case 'introspect':
                await this.handleIntrospect(msg.source);
                break;
            case 'generateSql':
                await this.handleGenerateSql(msg.source, msg.dataSourceName, msg.columns, msg.castMode);
                break;
            case 'previewDataset':
                await this.handlePreviewDataset(msg.source, msg.dataSourceName, msg.sql);
                break;
            case 'create':
                await this.handleCreate(msg.payload);
                break;
        }
    }

    private async handleIntrospect(source: SourceConfig): Promise<void> {
        this.post({ type: 'busy', busy: true });
        try {
            const { spec, sheet } = sourceToSpec(source);
            const result = await this.indexer.introspectDraft({
                spec,
                baseDir: this.workspaceRoot(),
                sheet,
                limit: vscode.workspace.getConfiguration('bino').get<number>('wizard.sampleRowLimit') ?? 100,
            });
            this.post({ type: 'introspectResult', result });
        } catch (err) {
            this.post({ type: 'introspectResult', result: { columns: [], sampleRows: [], truncated: false, error: String(err) } });
        } finally {
            this.post({ type: 'busy', busy: false });
        }
    }

    private async handleGenerateSql(source: SourceConfig, dataSourceName: string, columns: CreateRequest['columns'], castMode: string): Promise<void> {
        try {
            const result = await this.indexer.typedSelect({
                source: dataSourceName,
                pretty: true,
                castMode,
                columns,
            });
            this.post({ type: 'sql', sql: result?.sql ?? '' });
        } catch (err) {
            this.outputChannel.appendLine(`[Wizard] typed-select failed: ${err}`);
        }
    }

    private async handlePreviewDataset(source: SourceConfig, dataSourceName: string, sql: string): Promise<void> {
        this.post({ type: 'busy', busy: true });
        try {
            const { spec, sheet } = sourceToSpec(source);
            const result = await this.indexer.previewDataSet({
                spec,
                sourceName: dataSourceName,
                sql,
                baseDir: this.workspaceRoot(),
                sheet,
                limit: vscode.workspace.getConfiguration('bino').get<number>('wizard.sampleRowLimit') ?? 100,
            });
            this.post({
                type: 'datasetPreview',
                columns: result.columns ?? [],
                rows: result.rows ?? [],
                truncated: !!result.truncated,
                error: result.error,
            });
        } catch (err) {
            this.post({ type: 'datasetPreview', columns: [], rows: [], truncated: false, error: String(err) });
        } finally {
            this.post({ type: 'busy', busy: false });
        }
    }

    private async handleCreate(req: CreateRequest): Promise<void> {
        this.post({ type: 'busy', busy: true });
        try {
            const payload = buildScaffoldPayload(req);
            const result = await this.indexer.scaffold(payload);
            if (!result.ok) {
                this.post({ type: 'created', files: result.files ?? [], error: result.error });
                return;
            }
            this.post({ type: 'created', files: result.files });
            await this.indexer.refreshIndex();
            await this.openCreatedFiles(result.files);
            vscode.window.showInformationMessage(`Bino: created ${result.files.map((f) => f.path).join(', ')}`);
            this.panel?.dispose();
        } catch (err) {
            this.post({ type: 'created', files: [], error: String(err) });
        } finally {
            this.post({ type: 'busy', busy: false });
        }
    }

    private async openCreatedFiles(files: { path: string }[]): Promise<void> {
        const root = this.workspaceRoot();
        if (!root) {
            return;
        }
        for (let i = 0; i < files.length; i++) {
            const abs = path.isAbsolute(files[i].path) ? files[i].path : path.join(root, files[i].path);
            try {
                const doc = await vscode.workspace.openTextDocument(abs);
                await vscode.window.showTextDocument(doc, { preview: false });
            } catch (err) {
                this.outputChannel.appendLine(`[Wizard] could not open ${abs}: ${err}`);
            }
        }
    }

    private uniqueName(base: string, kind: string): string {
        const existing = new Set(this.indexer.getDocuments([kind]).map((d) => d.name));
        if (!existing.has(base)) {
            return base;
        }
        for (let i = 2; ; i++) {
            const candidate = `${base}_${i}`;
            if (!existing.has(candidate)) {
                return candidate;
            }
        }
    }

    private workspaceRoot(): string | undefined {
        return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    }

    private post(msg: HostMessage): void {
        this.panel?.webview.postMessage(msg);
    }

    dispose(): void {
        this.panel?.dispose();
        this.panel = undefined;
    }
}

function suggestNameFrom(fileName: string): string {
    const base = fileName.replace(/\.[^.]+$/, '');
    const cleaned = base.toLowerCase().replace(/[^a-z0-9_]+/g, '_').replace(/^_+|_+$/g, '');
    return cleaned || 'data';
}

function sourceToSpec(s: SourceConfig): { spec: any; sheet?: string } {
    if (s.kind === 'file') {
        if (s.format === 'csv') {
            const spec: any = { type: 'csv', path: s.absPath };
            if (s.delimiter) { spec.delimiter = s.delimiter; }
            if (s.header === false) { spec.header = false; }
            if (s.skipRows) { spec.skipRows = s.skipRows; }
            return { spec };
        }
        if (s.format === 'excel') {
            return { spec: { type: 'excel', path: s.absPath }, sheet: s.sheet };
        }
        return { spec: { type: 'parquet', path: s.absPath } };
    }
    return { spec: { type: s.dbType, connection: s.connection, query: s.query } };
}

function buildScaffoldPayload(req: CreateRequest): any {
    const s = req.source;
    const ds: any = { name: req.dataSourceName };
    if (s.kind === 'file') {
        ds.type = s.format;
        ds.path = s.absPath;
        if (s.format === 'csv') {
            if (s.delimiter) { ds.delimiter = s.delimiter; }
            if (s.header === false) { ds.header = false; }
            if (s.skipRows) { ds.skipRows = s.skipRows; }
        } else if (s.format === 'excel' && s.sheet) {
            ds.sheet = s.sheet;
        }
    } else {
        ds.type = s.dbType;
        ds.connection = s.connection;
        ds.query = s.query;
    }

    const payload: any = { dataSource: ds };
    if (req.createDataSet) {
        payload.dataSet = {
            name: req.dataSetName,
            pretty: true,
            castMode: req.castMode,
            // Edited SQL wins; columns (source mappings, constants, expressions) are
            // the fallback the CLI regenerates from when sql is empty.
            sql: (req.sql ?? '').trim(),
            columns: req.columns,
        };
    }
    return payload;
}
