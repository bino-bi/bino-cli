import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import { DaemonClient } from './daemonClient';
import { setRenderEmbeddableKinds } from './embeddable';

/** The project configuration filename that marks a bino project root */
const PROJECT_CONFIG_FILE = 'bino.toml';

/** Document entry from the LSP index */
export interface LSPDocument {
    kind: string;
    name: string;
    file: string;
    position: number;
}

/** Result from bino lsp-helper index */
interface LSPIndexResult {
    documents: LSPDocument[];
    error?: string;
}

/** Result from bino lsp-helper columns */
interface LSPColumnsResult {
    name: string;
    columns: string[];
    error?: string;
}

/** A manifest kind with its served capability category and render-embeddable flag (from /kinds). */
export interface KindInfo {
    name: string;
    /** Capability category: data | layout | embeddable | artefact | config. */
    category: string;
    embeddable: boolean;
}

/** Result from bino lsp-helper kinds */
interface LSPKindsResult {
    kinds: KindInfo[];
    error?: string;
}

/** Cache entry for column data */
interface ColumnCacheEntry {
    columns: string[];
    timestamp: number;
}

/** Candidate dataset/datasource for disambiguation */
export interface DatasetCandidate {
    name: string;
    displayName: string;
    kind: 'DataSet' | 'DataSource';
}

/** Direction for graph traversal */
export type GraphDirection = 'in' | 'out' | 'both';

/** Node in the dependency graph from bino lsp-helper graph-deps */
export interface LSPGraphNode {
    id: string;
    kind: string;
    name: string;
    file?: string;
    hash?: string;
}

/** Edge in the dependency graph */
export interface LSPGraphEdge {
    fromId: string;
    toId: string;
    direction: 'in' | 'out';
}

/** Result from bino lsp-helper graph-deps */
export interface LSPGraphDepsResult {
    rootId: string;
    direction: GraphDirection;
    nodes: LSPGraphNode[];
    edges: LSPGraphEdge[];
    error?: string;
}

/** Result from bino lsp-helper rows */
export interface LSPRowsResult {
    name: string;
    kind: string;
    columns: string[];
    rows: Record<string, unknown>[];
    limit: number;
    truncated: boolean;
    error?: string;
}

/** A single schema-validation diagnostic for an edit (from lsp-helper edit). */
export interface LSPEditDiagnostic {
    message: string;
    line: number;
    col: number;
    severity: string;
}

/** Result from bino lsp-helper edit (compute/write of an edit/remove/reorder). */
export interface LSPEditResult {
    ok: boolean;
    full?: string;
    edited?: string;
    file?: string;
    diagnostics: LSPEditDiagnostic[];
    error?: string;
}

/** Result from bino lsp-helper create (envelope build + validate + atomic write). */
export interface LSPCreateResult {
    ok: boolean;
    /** Path of the written manifest, relative to the project root. */
    file?: string;
    /** "created" or "appended". */
    action?: string;
    diagnostics: LSPEditDiagnostic[];
    error?: string;
}

/**
 * WorkspaceIndexer maintains an index of all Bino documents in the workspace
 * and provides column introspection with caching.
 */
export class WorkspaceIndexer {
    private context: vscode.ExtensionContext;
    private documents: LSPDocument[] = [];
    private kinds: KindInfo[] | undefined;
    private columnsCache: Map<string, ColumnCacheEntry> = new Map();
    private indexPromise: Promise<void> | undefined;
    private outputChannel: vscode.OutputChannel;

    // Event emitter for index updates
    private _onDidUpdateIndex: vscode.EventEmitter<void> = new vscode.EventEmitter<void>();
    readonly onDidUpdateIndex: vscode.Event<void> = this._onDidUpdateIndex.event;

    // Event emitters for indexing state
    private _onDidStartIndex: vscode.EventEmitter<void> = new vscode.EventEmitter<void>();
    readonly onDidStartIndex: vscode.Event<void> = this._onDidStartIndex.event;

    private _onDidFinishIndex: vscode.EventEmitter<void> = new vscode.EventEmitter<void>();
    readonly onDidFinishIndex: vscode.Event<void> = this._onDidFinishIndex.event;

    private _isIndexing = false;
    private daemonClient: DaemonClient | undefined;

    /** Returns true if indexing is currently in progress */
    get isIndexing(): boolean {
        return this._isIndexing;
    }

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
        this.outputChannel = vscode.window.createOutputChannel('Bino Indexer');
        context.subscriptions.push(this.outputChannel);
        context.subscriptions.push(this._onDidUpdateIndex);
        context.subscriptions.push(this._onDidStartIndex);
        context.subscriptions.push(this._onDidFinishIndex);
    }

    /** Set the daemon client for fast operations */
    setDaemonClient(client: DaemonClient | undefined): void {
        this.daemonClient = client;
    }

    /** Get the configured bino CLI path */
    private getBinoPath(): string {
        const config = vscode.workspace.getConfiguration('bino');
        const binPath = config.get<string>('binPath');
        return binPath && binPath.trim() ? binPath : 'bino';
    }

    /** Get the column cache TTL from settings */
    private getCacheTTL(): number {
        const config = vscode.workspace.getConfiguration('bino');
        return config.get<number>('columnCacheTTL') ?? 60000;
    }

    /**
     * Find the bino project root by searching for bino.toml.
     * Starts from the given directory and walks up the hierarchy.
     * Returns undefined if no bino.toml is found.
     */
    private findProjectRoot(startDir: string): string | undefined {
        let current = startDir;
        while (true) {
            const configPath = path.join(current, PROJECT_CONFIG_FILE);
            if (fs.existsSync(configPath)) {
                return current;
            }
            const parent = path.dirname(current);
            if (parent === current) {
                // Reached filesystem root
                return undefined;
            }
            current = parent;
        }
    }

    /**
     * Get the bino project root for the given URI or active editor.
     * For multi-root workspaces, finds the appropriate project root
     * based on the file's location.
     */
    /**
     * The workspace's bino project root, independent of which editor happens
     * to be active — the deterministic root for long-lived services (the
     * language server). Terminal commands keep the active-editor preference
     * of getProjectRootForUri.
     */
    getWorkspaceProjectRoot(): string | undefined {
        const folders = vscode.workspace.workspaceFolders;
        if (folders) {
            for (const folder of folders) {
                const projectRoot = this.findProjectRoot(folder.uri.fsPath);
                if (projectRoot) {
                    return projectRoot;
                }
            }
        }
        return undefined;
    }

    getProjectRootForUri(uri?: vscode.Uri): string | undefined {
        // If a specific URI is provided, find the project root for that file
        if (uri) {
            const fileDir = path.dirname(uri.fsPath);
            return this.findProjectRoot(fileDir);
        }

        // Try active editor first
        const activeEditor = vscode.window.activeTextEditor;
        if (activeEditor?.document.uri.scheme === 'file') {
            const fileDir = path.dirname(activeEditor.document.uri.fsPath);
            const projectRoot = this.findProjectRoot(fileDir);
            if (projectRoot) {
                return projectRoot;
            }
        }

        // Fallback: search all workspace folders for bino.toml
        const folders = vscode.workspace.workspaceFolders;
        if (folders) {
            for (const folder of folders) {
                const projectRoot = this.findProjectRoot(folder.uri.fsPath);
                if (projectRoot) {
                    return projectRoot;
                }
            }
        }

        return undefined;
    }

    /** Get workspace root directory (legacy method, now uses project root detection) */
    private getWorkspaceRoot(): string | undefined {
        return this.getProjectRootForUri();
    }

    /**
     * Check if a bino project exists in any workspace folder.
     * Used to determine if the extension should be fully active.
     */
    hasProjectInWorkspace(): boolean {
        const folders = vscode.workspace.workspaceFolders;
        if (!folders) {
            return false;
        }
        for (const folder of folders) {
            if (this.findProjectRoot(folder.uri.fsPath)) {
                return true;
            }
        }
        return false;
    }

    /** Execute bino lsp-helper command */
    private async execBino(args: string[]): Promise<string> {
        const binPath = this.getBinoPath();
        const workDir = this.getWorkspaceRoot();

        return new Promise((resolve, reject) => {
            const options: cp.ExecOptionsWithStringEncoding = {
                cwd: workDir,
                maxBuffer: 10 * 1024 * 1024, // 10MB
                timeout: 30000, // 30 seconds
                encoding: 'utf8'
            };

            const cmd = [binPath, ...args].join(' ');
            this.outputChannel.appendLine(`Executing: ${cmd}`);

            cp.exec(cmd, options, (error, stdout, stderr) => {
                if (error) {
                    this.outputChannel.appendLine(`Error: ${error.message}`);
                    if (stderr) {
                        this.outputChannel.appendLine(`Stderr: ${stderr}`);
                    }
                    reject(error);
                    return;
                }
                resolve(stdout);
            });
        });
    }

    /** Refresh the workspace index */
    async refreshIndex(): Promise<void> {
        if (this.indexPromise) {
            return this.indexPromise;
        }

        this.indexPromise = this.doRefreshIndex();
        try {
            await this.indexPromise;
        } finally {
            this.indexPromise = undefined;
        }
    }

    private async doRefreshIndex(): Promise<void> {
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            this.outputChannel.appendLine('No workspace folder open');
            return;
        }

        this._isIndexing = true;
        this._onDidStartIndex.fire();

        // Refresh the render-embeddable set from the served `embeddable` flag so
        // it never drifts from the Go authority. Cached for the session; only
        // re-fetched until the first success.
        await this.ensureRenderEmbeddableKinds();

        try {
            // Try daemon first for faster indexing
            if (this.daemonClient?.isConnected) {
                const result = await this.daemonClient.getIndex();
                if (result && !result.error) {
                    this.documents = result.documents;
                    this.outputChannel.appendLine(`Indexed ${this.documents.length} documents (daemon)`);
                    this._onDidUpdateIndex.fire();
                    return;
                }
            }

            // Fallback to subprocess
            const output = await this.execBino(['lsp-helper', 'index', workDir]);
            const result: LSPIndexResult = JSON.parse(output);

            if (result.error) {
                this.outputChannel.appendLine(`Index error: ${result.error}`);
                return;
            }

            this.documents = result.documents;
            this.outputChannel.appendLine(`Indexed ${this.documents.length} documents`);

            // Fire event to notify listeners (e.g., tree view)
            this._onDidUpdateIndex.fire();
        } catch (err) {
            this.outputChannel.appendLine(`Failed to index workspace: ${err}`);
        } finally {
            this._isIndexing = false;
            this._onDidFinishIndex.fire();
        }
    }

    /** Invalidate the entire index (e.g., on file create/delete) */
    invalidateIndex(): void {
        this.documents = [];
        this.columnsCache.clear();
        // Trigger re-index in background
        this.refreshIndex();
    }

    /** Invalidate cache for a specific file */
    invalidateFile(filePath: string): void {
        // Find documents from this file and invalidate their column cache
        const affectedDocs = this.documents.filter(doc => doc.file === filePath);
        for (const doc of affectedDocs) {
            this.columnsCache.delete(doc.name);
            this.columnsCache.delete(`$${doc.name}`);
        }
        // Trigger re-index in background
        this.refreshIndex();
    }

    /** Get all documents of specified kinds */
    getDocuments(kinds?: string[]): LSPDocument[] {
        if (!kinds || kinds.length === 0) {
            return this.documents;
        }
        return this.documents.filter(doc => kinds.includes(doc.kind));
    }

    /** Get document names for completion, optionally filtered by kind */
    getDocumentNames(kinds?: string[]): string[] {
        return this.getDocuments(kinds).map(doc => doc.name);
    }

    /** Get DataSource names with $ prefix and DataSet names */
    getDatasetCompletions(): string[] {
        const dataSources = this.getDocuments(['DataSource']).map(doc => `$${doc.name}`);
        const dataSets = this.getDocuments(['DataSet']).map(doc => doc.name);
        return [...dataSets, ...dataSources];
    }

    /** Get Asset names for image source completion */
    getAssetCompletions(): string[] {
        return this.getDocuments(['Asset']).map(doc => doc.name);
    }

    /** Get columns for a datasource/dataset (with caching) */
    async getColumns(name: string): Promise<string[]> {
        const cacheTTL = this.getCacheTTL();
        const cached = this.columnsCache.get(name);
        const now = Date.now();

        if (cached && (now - cached.timestamp) < cacheTTL) {
            return cached.columns;
        }

        // Try daemon first
        if (this.daemonClient?.isConnected) {
            try {
                const result = await this.daemonClient.getColumns(name);
                if (result && !result.error) {
                    this.columnsCache.set(name, { columns: result.columns, timestamp: now });
                    return result.columns;
                }
                if (result?.error) {
                    this.outputChannel.appendLine(`Columns error for ${name} (daemon): ${result.error}`);
                }
            } catch {
                // Fall through to subprocess
            }
        }

        // Fallback to subprocess
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return [];
        }

        try {
            const output = await this.execBino(['lsp-helper', 'columns', workDir, name]);
            const result: LSPColumnsResult = JSON.parse(output);

            if (result.error) {
                this.outputChannel.appendLine(`Columns error for ${name}: ${result.error}`);
                return [];
            }

            this.columnsCache.set(name, {
                columns: result.columns,
                timestamp: now
            });

            return result.columns;
        } catch (err) {
            this.outputChannel.appendLine(`Failed to get columns for ${name}: ${err}`);
            return [];
        }
    }

    /**
     * The live manifest-kind list (built-in + plugin) as served by the backend,
     * fetched once and cached for the session. Surfacing this lets GUI surfaces
     * (the "Add element" palette) enumerate every kind from one backend fact, so
     * plugin-provided kinds appear with no extension change.
     */
    async getKindInfos(): Promise<KindInfo[]> {
        if (this.kinds) {
            return this.kinds;
        }
        return this.refreshKinds();
    }

    /**
     * Fetch every manifest kind and its served render-embeddable flag from the
     * backend (daemon /kinds, falling back to `lsp-helper kinds`). This is the
     * single render-embeddable authority (internal/report/embed); the extension
     * derives membership from it instead of a hand-maintained list. The result is
     * cached for the session.
     */
    private async refreshKinds(): Promise<KindInfo[]> {
        // Try daemon first
        if (this.daemonClient?.isConnected) {
            try {
                const result = await this.daemonClient.getKinds();
                if (result && !result.error && Array.isArray(result.kinds)) {
                    this.kinds = result.kinds;
                    return this.kinds;
                }
            } catch {
                // Fall through to subprocess
            }
        }

        // Fallback to subprocess
        try {
            const output = await this.execBino(['lsp-helper', 'kinds']);
            const result: LSPKindsResult = JSON.parse(output);
            if (!result.error && Array.isArray(result.kinds)) {
                this.kinds = result.kinds;
            }
        } catch (err) {
            this.outputChannel.appendLine(`Failed to get kinds: ${err}`);
        }
        return this.kinds ?? [];
    }

    /**
     * Fetch the served kind list once and apply the render-embeddable flag to the
     * shared embeddable set. Idempotent: re-fetches only until the first success,
     * so it can be called on every index refresh without repeated backend hits.
     */
    private async ensureRenderEmbeddableKinds(): Promise<void> {
        if (this.kinds) {
            return;
        }
        const kinds = await this.refreshKinds();
        if (kinds.length > 0) {
            setRenderEmbeddableKinds(kinds);
        }
    }

    /**
     * Get dependency graph for a node.
     * @param kind Node kind (ReportArtefact, DataSet, DataSource, LayoutPage, LayoutCard, Component)
     * @param name Node name
     * @param direction Traversal direction: 'in' (dependents), 'out' (dependencies), 'both'
     * @param maxDepth Maximum traversal depth (0 = unlimited)
     */
    async getGraphDeps(
        kind: string,
        name: string,
        direction: GraphDirection = 'both',
        maxDepth: number = 0
    ): Promise<LSPGraphDepsResult | undefined> {
        // Try daemon first
        if (this.daemonClient?.isConnected) {
            try {
                const result = await this.daemonClient.getGraphDeps(kind, name, direction, maxDepth);
                if (result && !result.error) {
                    return result as LSPGraphDepsResult;
                }
            } catch {
                // Fall through to subprocess
            }
        }

        // Fallback to subprocess
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return undefined;
        }

        try {
            const args = [
                'lsp-helper', 'graph-deps', workDir,
                '--kind', kind,
                '--name', name,
                '--direction', direction
            ];

            if (maxDepth > 0) {
                args.push('--max-depth', String(maxDepth));
            }

            const output = await this.execBino(args);
            const result: LSPGraphDepsResult = JSON.parse(output);

            if (result.error) {
                this.outputChannel.appendLine(`Graph deps error for ${kind}:${name}: ${result.error}`);
                return undefined;
            }

            return result;
        } catch (err) {
            this.outputChannel.appendLine(`Failed to get graph deps for ${kind}:${name}: ${err}`);
            return undefined;
        }
    }

    /**
     * Get preview rows for a DataSource or DataSet.
     * @param name Document name
     * @param limit Maximum number of rows to return
     */
    async getRows(name: string, limit: number = 100): Promise<LSPRowsResult | undefined> {
        // Try daemon first
        if (this.daemonClient?.isConnected) {
            try {
                const result = await this.daemonClient.getRows(name, limit);
                if (result) {
                    return result as LSPRowsResult;
                }
            } catch {
                // Fall through to subprocess
            }
        }

        // Fallback to subprocess
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return undefined;
        }

        try {
            const args = [
                'lsp-helper', 'rows', workDir, name,
                '--limit', String(limit)
            ];

            const output = await this.execBino(args);
            const result: LSPRowsResult = JSON.parse(output);

            if (result.error) {
                this.outputChannel.appendLine(`Rows error for ${name}: ${result.error}`);
                return result; // Return with error so caller can show error message
            }

            return result;
        } catch (err) {
            this.outputChannel.appendLine(`Failed to get rows for ${name}: ${err}`);
            return undefined;
        }
    }

    /**
     * Execute a bino lsp-helper command, piping `input` to its stdin. Used for
     * the wizard subcommands, which take JSON on stdin (--spec-file -) to keep
     * SQL and file paths off the argv.
     */
    private execBinoStdin(args: string[], input: string): Promise<string> {
        const binPath = this.getBinoPath();
        const workDir = this.getWorkspaceRoot();
        return new Promise((resolve, reject) => {
            const proc = cp.spawn(binPath, args, { cwd: workDir, stdio: ['pipe', 'pipe', 'pipe'] });
            let stdout = '';
            let stderr = '';
            proc.stdout.on('data', (d: Buffer) => { stdout += d.toString(); });
            proc.stderr.on('data', (d: Buffer) => { stderr += d.toString(); });
            proc.on('error', reject);
            proc.on('close', (code) => {
                if (code !== 0) {
                    this.outputChannel.appendLine(`lsp-helper ${args.join(' ')} exited ${code}: ${stderr}`);
                    reject(new Error(stderr || `exited with code ${code}`));
                    return;
                }
                resolve(stdout);
            });
            proc.stdin.write(input);
            proc.stdin.end();
        });
    }

    /** Introspect a draft data source (schema, types, sample, sheets, csv options). */
    async introspectDraft(req: { spec: any; baseDir?: string; sheet?: string; limit?: number }): Promise<any> {
        if (this.daemonClient?.isConnected && this.daemonClient.hasCapability('introspect-draft')) {
            try {
                const result = await this.daemonClient.introspectDraft(req);
                if (result) {
                    return result;
                }
            } catch {
                // Fall through to subprocess
            }
        }
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return { error: 'no workspace folder open', columns: [], sampleRows: [] };
        }
        const args = ['lsp-helper', 'introspect-draft', workDir, '--spec-file', '-', '--limit', String(req.limit ?? 100)];
        if (req.baseDir) { args.push('--base-dir', req.baseDir); }
        if (req.sheet) { args.push('--sheet', req.sheet); }
        const output = await this.execBinoStdin(args, JSON.stringify(req.spec));
        return JSON.parse(output);
    }

    /** Generate a column-aware SELECT statement server-side. */
    async typedSelect(req: { source: string; columns: any[]; pretty?: boolean; castMode?: string }): Promise<{ sql: string; aliases: string[] } | undefined> {
        if (this.daemonClient?.isConnected && this.daemonClient.hasCapability('typed-select')) {
            try {
                const result = await this.daemonClient.typedSelect(req);
                if (result) {
                    return result;
                }
            } catch {
                // Fall through to subprocess
            }
        }
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return undefined;
        }
        const output = await this.execBinoStdin(['lsp-helper', 'typed-select', '--payload-file', '-'], JSON.stringify(req));
        return JSON.parse(output);
    }

    /** Run a draft DataSet SQL against a not-yet-registered DataSource (ephemeral). */
    async previewDataSet(req: { spec: any; sourceName: string; sql: string; baseDir?: string; sheet?: string; limit?: number }): Promise<any> {
        if (this.daemonClient?.isConnected && this.daemonClient.hasCapability('preview-dataset')) {
            try {
                const result = await this.daemonClient.previewDataSet(req);
                if (result) {
                    return result;
                }
            } catch {
                // Fall through to subprocess
            }
        }
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return { error: 'no workspace folder open', columns: [], rows: [] };
        }
        const output = await this.execBinoStdin(['lsp-helper', 'preview-dataset', workDir, '--payload-file', '-'], JSON.stringify(req));
        return JSON.parse(output);
    }

    /** Fetch the canonical dataset schema (standard columns) from the CLI/daemon. */
    async datasetSchema(): Promise<any[]> {
        if (this.daemonClient?.isConnected && this.daemonClient.hasCapability('dataset-schema')) {
            try {
                const result = await this.daemonClient.datasetSchema();
                if (result?.columns) {
                    return result.columns;
                }
            } catch {
                // Fall through to subprocess
            }
        }
        try {
            const output = await this.execBino(['lsp-helper', 'dataset-schema']);
            return JSON.parse(output).columns ?? [];
        } catch {
            return [];
        }
    }

    /** Write DataSource (and optionally DataSet) manifests from a wizard payload. */
    async scaffold(payload: any): Promise<{ ok: boolean; files: { path: string; appended: boolean }[]; error?: string }> {
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return { ok: false, files: [], error: 'no workspace folder open' };
        }
        const output = await this.execBinoStdin(['lsp-helper', 'scaffold', workDir, '--payload-file', '-'], JSON.stringify(payload));
        return JSON.parse(output);
    }

    /**
     * Compute or write a fidelity-preserving manifest edit through the one Go
     * engine (EditYAMLDocument). The payload selects op (edit/remove/reorder)
     * and mode (compute/write); compute returns the rewritten file without
     * touching disk, write applies it atomically. A non-empty diagnostics list
     * means the edit was rejected. Used by the Design-mode authoring client.
     */
    async editManifest(payload: Record<string, unknown>): Promise<LSPEditResult> {
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return { ok: false, diagnostics: [], error: 'no workspace folder open' };
        }
        const output = await this.execBinoStdin(['lsp-helper', 'edit', workDir, '--payload-file', '-'], JSON.stringify(payload));
        return JSON.parse(output);
    }

    /**
     * Create a new manifest of any kind through the one Go create path
     * (CreateManifest): it builds the apiVersion/kind/metadata/spec envelope,
     * validates it against the schema, and writes it atomically, auto-placing the
     * file by project convention unless `file` is given. A non-empty diagnostics
     * list means the manifest was rejected and nothing was written. Used by the
     * Design-mode Add-element palette via the AuthoringClient.
     */
    async createManifest(payload: Record<string, unknown>): Promise<LSPCreateResult> {
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            return { ok: false, diagnostics: [], error: 'no workspace folder open' };
        }
        const output = await this.execBinoStdin(['lsp-helper', 'create', workDir, '--payload-file', '-'], JSON.stringify(payload));
        return JSON.parse(output);
    }

    /** Check if a document is a Bino manifest (has apiVersion: bino.bi) */
    async isBinoDocument(document: vscode.TextDocument): Promise<boolean> {
        const text = document.getText();
        return text.includes('apiVersion: bino.bi') ||
            text.includes('apiVersion: "bino.bi') ||
            text.includes("apiVersion: 'bino.bi");
    }

    /**
     * Infer dataset/datasource candidates from the current cursor position.
     * This uses similar logic to completion: looks for `dataset:` field value
     * on the current line or in the surrounding context.
     * 
     * @returns Array of candidates (may be empty if none found, or multiple if ambiguous)
     */
    inferDatasetCandidatesAtPosition(
        document: vscode.TextDocument,
        position: vscode.Position
    ): DatasetCandidate[] {
        const candidates: DatasetCandidate[] = [];
        const text = document.getText();
        const lines = text.split('\n');

        // Strategy 1: Check if cursor is on a line with `dataset: <name>`
        const currentLine = lines[position.line] || '';
        const datasetMatch = currentLine.match(/^\s*dataset:\s*(.+?)\s*$/);
        if (datasetMatch) {
            const name = datasetMatch[1].trim();
            if (name) {
                this.addCandidate(candidates, name);
                return candidates;
            }
        }

        // Strategy 2: Look backwards for the nearest `dataset:` field within the same component
        let componentIndent = -1;
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 50; lineNum--) {
            const line = lines[lineNum];
            const trimmed = line.trim();
            const indent = this.getIndentation(line);

            // Look for dataset field
            if (trimmed.startsWith('dataset:')) {
                if (componentIndent === -1 || indent >= componentIndent - 2) {
                    const match = trimmed.match(/^dataset:\s*(.+)$/);
                    if (match) {
                        const name = match[1].trim();
                        if (name) {
                            this.addCandidate(candidates, name);
                            return candidates;
                        }
                    }
                }
            }

            // Track component boundaries (kind field usually indicates component start)
            if (trimmed.startsWith('kind:')) {
                componentIndent = indent;
            }
        }

        // Strategy 3: If no dataset field found, return all datasets/datasources
        // so user can pick from a full list
        if (candidates.length === 0) {
            const dataSets = this.getDocuments(['DataSet']);
            const dataSources = this.getDocuments(['DataSource']);

            for (const ds of dataSets) {
                candidates.push({
                    name: ds.name,
                    displayName: ds.name,
                    kind: 'DataSet'
                });
            }
            for (const ds of dataSources) {
                candidates.push({
                    name: `$${ds.name}`,
                    displayName: `$${ds.name}`,
                    kind: 'DataSource'
                });
            }
        }

        return candidates;
    }

    private addCandidate(candidates: DatasetCandidate[], name: string): void {
        if (name.startsWith('$')) {
            // DataSource reference
            candidates.push({
                name: name,
                displayName: name,
                kind: 'DataSource'
            });
        } else {
            // DataSet reference
            candidates.push({
                name: name,
                displayName: name,
                kind: 'DataSet'
            });
        }
    }

    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }
}
