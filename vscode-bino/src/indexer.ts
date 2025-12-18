import * as vscode from 'vscode';
import * as cp from 'child_process';

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

/**
 * WorkspaceIndexer maintains an index of all Bino documents in the workspace
 * and provides column introspection with caching.
 */
export class WorkspaceIndexer {
    private context: vscode.ExtensionContext;
    private documents: LSPDocument[] = [];
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

    /** Returns true if indexing is currently in progress */
    get isIndexing(): boolean {
        return this._isIndexing;
    }

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
        this.outputChannel = vscode.window.createOutputChannel('Bino Reports');
        context.subscriptions.push(this.outputChannel);
        context.subscriptions.push(this._onDidUpdateIndex);
        context.subscriptions.push(this._onDidStartIndex);
        context.subscriptions.push(this._onDidFinishIndex);
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

    /** Get workspace root directory */
    private getWorkspaceRoot(): string | undefined {
        const folders = vscode.workspace.workspaceFolders;
        if (!folders || folders.length === 0) {
            return undefined;
        }
        return folders[0].uri.fsPath;
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

        try {
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
