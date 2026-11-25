import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as path from 'path';
import { WorkspaceIndexer, LSPDocument } from './indexer';
import { DaemonClient } from './daemonClient';
import { getEmbeddableKinds } from './embeddable';
import { getNonce } from './webviewNonce';
import { getBinoPath } from './bino';
import { fullPreviewPath, previewBaseUrl, urlOrigin } from './previewUrl';

/** A Cmd/Ctrl-click in the preview. sourceFile and line come from newer bino releases. */
interface RevealSourceMessage {
    kind?: string;
    name?: string;
    ref?: string;
    file?: string;
    sourceFile?: string;
    line?: number;
}

/** A click in annotate mode: the component, its panel and the data point. */
export interface PickPointMessage {
    kind: string;
    name: string;
    panel?: string;
    point?: Record<string, string>;
    sourceFile?: string;
    line?: number;
}

export type PreviewStatus = 'stopped' | 'starting' | 'running' | 'error';

/** `bino preview`'s default port, used when bino.previewPort is unset. */
const DEFAULT_PREVIEW_PORT = 45678;

/** The message for a preview that exited before it served. */
function previewStartError(port: number, output: string): string {
    if (!output || /address already in use|only one usage of each socket address/i.test(output)) {
        return `Bino preview could not start on port ${port}. The port may be in use by another program. ` +
            'Stop that program or change the "bino.previewPort" setting. See the "Bino Reports" output for details.';
    }
    return 'Bino preview exited before it started. See the "Bino Reports" output for details.';
}

/**
 * BinoPreviewManager manages the lifecycle of bino preview and build processes.
 */
export class BinoPreviewManager {
    private outputChannel: vscode.OutputChannel;
    private previewProcess: cp.ChildProcess | undefined;
    private previewStatus: PreviewStatus = 'stopped';
    private previewPanel: vscode.WebviewPanel | undefined;
    // Route the full preview shows ("/", "/<artefact>", "/doc/<artefact>").
    private fullPreviewRoute = '/';
    private fullPreviewBase: string | undefined;
    private fullFollowDisposables: vscode.Disposable[] = [];
    private indexer: WorkspaceIndexer | undefined;
    private daemonClient: DaemonClient | undefined;
    private usingDaemonPreview = false;
    // Port of the running preview, as the daemon or the spawn reported it.
    private activePort: number | undefined;
    // Resolves a pending daemon stop when the daemon reports the old preview gone.
    private daemonStopWaiter: (() => void) | undefined;

    // Embedded single-artefact preview (chrome-free /__embedding/{name} view)
    private artefactPanel: vscode.WebviewPanel | undefined;
    private readonly pickPointEmitter = new vscode.EventEmitter<PickPointMessage>();
    /** A data point picked in a preview in annotate mode. */
    readonly onDidPickPoint = this.pickPointEmitter.event;
    private readonly annotateEmitter = new vscode.EventEmitter<boolean>();
    /** Whether a preview that can annotate is open, and its mode is on. */
    readonly onDidChangeAnnotate = this.annotateEmitter.event;
    /** Panels whose page said it can annotate, with the mode each one is in. */
    private readonly annotating = new Map<vscode.WebviewPanel, boolean>();
    private currentArtefact: { name: string; kind: string } | undefined;
    private artefactFollowDisposables: vscode.Disposable[] = [];
    // Absolute paths we have pushed a buffer override for; cleared on save/close
    // so the disk becomes authoritative again.
    private overriddenFiles = new Set<string>();

    // Event emitter for status changes
    private _onStatusChange = new vscode.EventEmitter<PreviewStatus>();
    readonly onStatusChange = this._onStatusChange.event;

    constructor(outputChannel: vscode.OutputChannel, indexer?: WorkspaceIndexer) {
        this.outputChannel = outputChannel;
        this.indexer = indexer;
        // A restarted preview may serve on another port or for another project.
        this.onStatusChange(status => {
            if (status === 'running' && this.previewPanel) {
                void this.loadFullPreview();
            }
        });
    }

    /** Set the workspace indexer for source navigation */
    setIndexer(indexer: WorkspaceIndexer): void {
        this.indexer = indexer;
    }

    /** Set the daemon client for preview management */
    setDaemonClient(client: DaemonClient | undefined): void {
        this.daemonClient = client;
    }

    /** Get the configured bino CLI path */
    private getBinoPath(): string {
        return getBinoPath();
    }

    /** Get workspace root directory (bino project root containing bino.toml) */
    private getWorkspaceRoot(): string | undefined {
        // Use indexer's project root detection if available
        if (this.indexer) {
            return this.indexer.getProjectRootForUri();
        }
        // Fallback to first workspace folder
        const folders = vscode.workspace.workspaceFolders;
        if (!folders || folders.length === 0) {
            return undefined;
        }
        return folders[0].uri.fsPath;
    }

    /** The port the running preview serves on, or the configured port when none runs. */
    private getPreviewPort(): number {
        if (this.activePort !== undefined) {
            return this.activePort;
        }
        const config = vscode.workspace.getConfiguration('bino');
        return config.get<number>('previewPort') ?? DEFAULT_PREVIEW_PORT;
    }

    /** Set preview status and emit event */
    private setStatus(status: PreviewStatus): void {
        this.previewStatus = status;
        this._onStatusChange.fire(status);
    }

    /** Start the preview server */
    async startPreview(): Promise<void> {
        if (this.previewProcess || this.usingDaemonPreview) {
            vscode.window.showInformationMessage('Preview is already running');
            return;
        }

        const port = this.getPreviewPort();

        // Try daemon first
        if (this.daemonClient?.isConnected) {
            this.setStatus('starting');
            this.outputChannel.appendLine(`[Preview] Starting via daemon on port ${port}...`);
            try {
                const result = await this.daemonClient.startPreview(port);
                if (result && result.status !== 'error') {
                    this.usingDaemonPreview = true;
                    // A preview the daemon already runs keeps its own port.
                    this.activePort = result.port || port;
                    if (this.activePort !== port) {
                        this.outputChannel.appendLine(`[Preview] Daemon preview already runs on port ${this.activePort}`);
                    }
                    if (result.status === 'running') {
                        this.setStatus('running');
                    }
                    // Status updates come via SSE preview-status events
                    return;
                }
                if (result?.error) {
                    this.outputChannel.appendLine(`[Preview] Daemon error: ${result.error}`);
                }
            } catch {
                this.outputChannel.appendLine('[Preview] Daemon start failed, falling back to subprocess');
            }
        }

        // Fallback to subprocess
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            vscode.window.showWarningMessage('No workspace folder open');
            return;
        }

        this.setStatus('starting');
        this.outputChannel.appendLine(`[Preview] Starting preview server on port ${port}...`);
        this.outputChannel.show(true);
        this.spawnPreview(workDir, port, true);
    }

    /**
     * Spawn `bino preview` as a child process. Older binaries do not know
     * --no-open; when one rejects it, the spawn is retried without it.
     */
    private spawnPreview(workDir: string, port: number, noOpen: boolean): void {
        const args = ['preview', '--port', String(port)];
        if (noOpen) {
            args.push('--no-open');
        }
        let proc: cp.ChildProcess;
        try {
            proc = cp.spawn(this.getBinoPath(), args, {
                cwd: workDir,
                stdio: ['ignore', 'pipe', 'pipe']
            });
        } catch (err) {
            this.outputChannel.appendLine(`[Preview] Failed to start: ${err}`);
            this.setStatus('error');
            vscode.window.showErrorMessage(`Failed to start preview: ${err}`);
            return;
        }
        this.previewProcess = proc;
        this.activePort = port;

        // Recent output, to find the ready line and the reason for an early exit.
        let output = '';
        const onOutput = (data: Buffer) => {
            const text = data.toString();
            this.outputChannel.append(text);
            output = (output + text).slice(-4096);
            if (this.previewProcess === proc && this.previewStatus === 'starting' && output.includes('Serving preview at')) {
                this.setStatus('running');
            }
        };
        proc.stdout?.on('data', onOutput);
        proc.stderr?.on('data', onOutput);

        proc.on('error', (err) => {
            if (this.previewProcess !== proc) {
                return;
            }
            this.outputChannel.appendLine(`[Preview] Error: ${err.message}`);
            this.previewProcess = undefined;
            this.activePort = undefined;
            this.setStatus('error');
            vscode.window.showErrorMessage(`Failed to start preview: ${err.message}`);
        });

        proc.on('exit', (code) => {
            this.outputChannel.appendLine(`[Preview] Process exited with code ${code}`);
            // A stopped or replaced process must not change the current state.
            if (this.previewProcess !== proc) {
                return;
            }
            this.previewProcess = undefined;
            this.activePort = undefined;
            const wasStarting = this.previewStatus === 'starting';
            if (wasStarting && noOpen && output.includes('unknown flag: --no-open')) {
                this.spawnPreview(workDir, port, false);
                return;
            }
            if (wasStarting) {
                this.setStatus('error');
                vscode.window.showErrorMessage(previewStartError(port, output));
                return;
            }
            if (this.previewStatus !== 'error') {
                this.setStatus('stopped');
            }
        });
    }

    /** Stop the preview server. Resolves once the preview has exited (or a timeout passed). */
    async stopPreview(): Promise<void> {
        // Stop daemon-managed preview
        if (this.usingDaemonPreview) {
            this.outputChannel.appendLine('[Preview] Stopping via daemon...');
            this.usingDaemonPreview = false;
            this.activePort = undefined;
            this.setStatus('stopped');
            // The daemon reports "stopped" once the old process has exited and freed its port.
            const exited = new Promise<void>(resolve => {
                const timer = setTimeout(() => { this.daemonStopWaiter = undefined; resolve(); }, 5000);
                this.daemonStopWaiter = () => { clearTimeout(timer); resolve(); };
            });
            await this.daemonClient?.stopPreview().catch(() => undefined);
            await exited;
            return;
        }

        const proc = this.previewProcess;
        if (!proc) {
            vscode.window.showInformationMessage('Preview is not running');
            return;
        }

        this.outputChannel.appendLine('[Preview] Stopping preview server...');
        this.previewProcess = undefined;
        this.activePort = undefined;
        this.setStatus('stopped');

        if (proc.exitCode !== null || proc.signalCode !== null) {
            return;
        }
        await new Promise<void>(resolve => {
            // Force kill after 3 seconds if not terminated
            const timer = setTimeout(() => proc.kill('SIGKILL'), 3000);
            proc.once('exit', () => { clearTimeout(timer); resolve(); });
            try {
                proc.kill('SIGTERM');
            } catch (err) {
                this.outputChannel.appendLine(`[Preview] Error stopping: ${err}`);
            }
        });
    }

    /** Restart the preview server */
    async restartPreview(): Promise<void> {
        await this.stopPreview();
        await this.startPreview();
    }

    /** Wait until a started preview runs, fails, or the timeout passes. */
    private waitForRunning(timeoutMs = 15000): Promise<void> {
        if (this.previewStatus !== 'starting') {
            return Promise.resolve();
        }
        return new Promise(resolve => {
            const timer = setTimeout(() => { sub.dispose(); resolve(); }, timeoutMs);
            const sub = this.onStatusChange(status => {
                if (status !== 'starting') {
                    clearTimeout(timer);
                    sub.dispose();
                    resolve();
                }
            });
        });
    }

    /** Open preview in browser */
    async openInBrowser(): Promise<void> {
        const base = await previewBaseUrl(this.getPreviewPort());
        await vscode.env.openExternal(vscode.Uri.parse(base + '/'));
    }

    /** Open preview in a VS Code webview panel (side-by-side) */
    async openPreviewInWebview(): Promise<void> {
        // Ensure preview is running
        if (this.previewStatus !== 'running') {
            await this.startPreview();
            await this.waitForRunning();
        }

        const active = this.resolveActiveArtefact();
        const route = active ? fullPreviewPath(active.kind, active.name) : undefined;
        if (route) {
            this.fullPreviewRoute = route;
        }

        // Reuse existing panel if it exists
        if (this.previewPanel) {
            this.previewPanel.reveal(vscode.ViewColumn.Beside);
            // Update the content in case port changed
            await this.loadFullPreview();
            return;
        }

        // Create a new webview panel
        this.previewPanel = vscode.window.createWebviewPanel(
            'binoPreview',
            'Bino Preview',
            vscode.ViewColumn.Beside,
            {
                enableScripts: true,
                retainContextWhenHidden: true
            }
        );

        // Handle messages from the webview (click-to-source)
        const full = this.previewPanel;
        full.webview.onDidReceiveMessage(async (msg) => {
            if (msg.type === 'bino:revealSource') {
                await this.handleRevealSource(msg);
            } else {
                this.handleAnnotateMessage(full, msg);
            }
        });

        // Clear reference when panel is closed
        this.previewPanel.onDidDispose(() => {
            this.forgetAnnotate(full);
            this.previewPanel = undefined;
            this.fullFollowDisposables.forEach(d => d.dispose());
            this.fullFollowDisposables = [];
        });

        // Follow the active editor to the artefact it shows.
        this.fullFollowDisposables = this.followActiveEditor(next => {
            const nextRoute = fullPreviewPath(next.kind, next.name);
            if (!nextRoute || nextRoute === this.fullPreviewRoute || !this.fullPreviewBase) {
                return;
            }
            this.fullPreviewRoute = nextRoute;
            void this.previewPanel?.webview.postMessage({ type: 'bino:navigate', url: this.fullPreviewBase + nextRoute });
        });

        await this.loadFullPreview();
    }

    /** (Re)load the full preview webview at the current route. */
    private async loadFullPreview(): Promise<void> {
        const base = await previewBaseUrl(this.getPreviewPort());
        if (!this.previewPanel) {
            return;
        }
        this.fullPreviewBase = base;
        this.previewPanel.webview.html = this.getWebviewContent(base, this.fullPreviewRoute);
    }

    /**
     * Open the embedded single-artefact preview (chrome-free /__embedding/{name}).
     * Auto-follows the active editor while the panel is open.
     * @param target Optional artefact document from a tree item or CodeLens.
     * @param preserveFocus Keep the focus where it is, e.g. in an editor that refreshes the preview after an edit.
     */
    async previewArtefactEmbedded(target?: LSPDocument, preserveFocus = false): Promise<void> {
        let artefact = target
            ? { name: target.name, kind: target.kind }
            : this.resolveActiveArtefact();

        // No explicit target and nothing embeddable under the cursor: ask.
        if (!artefact && !target) {
            artefact = await this.pickArtefact();
            if (!artefact) {
                return; // user cancelled
            }
        }
        if (!artefact) {
            vscode.window.showInformationMessage('No embeddable artefact selected');
            return;
        }

        // Ensure the preview server is running.
        if (this.previewStatus !== 'running') {
            await this.startPreview();
            await this.waitForRunning();
        }

        this.currentArtefact = artefact;

        if (!this.artefactPanel) {
            this.artefactPanel = vscode.window.createWebviewPanel(
                'binoArtefactPreview',
                `Embed: ${artefact.name}`,
                vscode.ViewColumn.Beside,
                {
                    enableScripts: true,
                    retainContextWhenHidden: true
                }
            );

            const embedded = this.artefactPanel;
            this.artefactPanel.onDidDispose(() => {
                this.forgetAnnotate(embedded);
                this.artefactPanel = undefined;
                this.currentArtefact = undefined;
                this.disposeArtefactFollow();
                // Disk is authoritative again: clear every override we pushed.
                void this.clearAllOverrides();
            });

            this.artefactFollowDisposables.push(...this.followActiveEditor(next => this.switchArtefact(next.name, next.kind)));
            this.registerLiveOverridePush();
            this.artefactPanel.webview.onDidReceiveMessage(async (msg) => {
                if (msg.type === 'bino:revealSource') {
                    await this.handleRevealSource(msg);
                } else {
                    this.handleAnnotateMessage(embedded, msg);
                }
            });
        }

        // Reflect any unsaved buffers before the first iframe load so opening
        // the preview shows the editor's current state, not stale disk content.
        await this.pushAllDirtyOverrides();

        this.artefactPanel.reveal(vscode.ViewColumn.Beside, preserveFocus);
        this.artefactPanel.title = `Embed: ${artefact.name}`;
        const base = await previewBaseUrl(this.getPreviewPort());
        if (!this.artefactPanel) {
            return; // closed while the port was forwarded
        }
        this.artefactPanel.webview.html = this.getEmbeddedWebviewContent(base, artefact.name, artefact.kind);
    }

    /** Whether the embedded artefact preview panel is currently open. */
    isEmbeddedPreviewOpen(): boolean {
        return this.artefactPanel !== undefined;
    }

    /** Switch the embedded preview to another artefact without rebuilding the webview (keeps SSE alive). */
    private switchArtefact(name: string, kind: string): void {
        if (!this.artefactPanel || this.currentArtefact?.name === name) {
            return;
        }
        this.currentArtefact = { name, kind };
        this.artefactPanel.title = `Embed: ${name}`;
        this.artefactPanel.webview.postMessage({ type: 'bino:setArtefact', name, kind });
    }

    /**
     * Resolve the embeddable artefact at the active editor's cursor.
     * Returns undefined when the active file holds no ReportArtefact/DocumentArtefact
     * (e.g. a dependency or a LiveReportArtefact) so the caller keeps the last artefact.
     */
    private resolveActiveArtefact(): { name: string; kind: string } | undefined {
        const editor = vscode.window.activeTextEditor;
        if (!editor || !this.indexer) {
            return undefined;
        }
        const doc = editor.document;
        const cursorLine = editor.selection.active.line; // 0-based

        const filePath = doc.uri.fsPath.replace(/\\/g, '/');
        const candidates = this.indexer
            .getDocuments(getEmbeddableKinds())
            .filter(d => d.file.replace(/\\/g, '/') === filePath);

        if (candidates.length === 0) {
            return undefined; // dependency / non-artefact → keep last
        }

        // Pick the candidate whose start line is the greatest startLine <= cursorLine.
        const withLines = candidates.map(d => ({ d, startLine: this.findDocumentLine(doc, d.position) }));
        let chosen = withLines[0];
        for (const c of withLines) {
            if (c.startLine <= cursorLine && c.startLine >= chosen.startLine) {
                chosen = c;
            }
        }
        return { name: chosen.d.name, kind: chosen.d.kind };
    }

    /** Prompt the user to choose an embeddable artefact. */
    private async pickArtefact(): Promise<{ name: string; kind: string } | undefined> {
        const artefacts = this.indexer?.getDocuments(getEmbeddableKinds()) ?? [];
        if (artefacts.length === 0) {
            vscode.window.showInformationMessage('No embeddable documents found in workspace');
            return undefined;
        }
        const items = artefacts.map(a => ({ label: a.name, description: a.kind }));
        const picked = await vscode.window.showQuickPick(items, {
            placeHolder: 'Select a document to preview (embedded)',
            title: 'Bino: Preview (Embedded)'
        });
        return picked ? { name: picked.label, kind: picked.description! } : undefined;
    }

    /** Call onChange with the artefact at the active editor's cursor as it moves. */
    private followActiveEditor(onChange: (next: { name: string; kind: string }) => void): vscode.Disposable[] {
        let timer: ReturnType<typeof setTimeout> | undefined;
        const followNow = () => {
            const next = this.resolveActiveArtefact();
            if (next) {
                onChange(next);
            }
            // next === undefined → editing a dependency/non-artefact → keep last
        };
        const debounced = () => {
            if (timer) {
                clearTimeout(timer);
            }
            timer = setTimeout(followNow, 200);
        };
        return [
            vscode.window.onDidChangeActiveTextEditor(() => followNow()),
            vscode.window.onDidChangeTextEditorSelection(debounced),
            { dispose: () => clearTimeout(timer) }
        ];
    }

    /**
     * Live preview: while the embedded preview is open, push unsaved Bino YAML
     * buffer content to the preview server so the previewed component renders
     * straight from the editor - both raw typing and designer form edits (which
     * land as unsaved WorkspaceEdits). No file is written; on success we ask the
     * embedded webview to reload the (component-scoped) iframe.
     *
     * On save, the buffer matches disk again, so we clear that file's override.
     * Scoped to Bino manifests and only while a preview panel is open.
     */
    private registerLiveOverridePush(): void {
        const timers = new Map<string, ReturnType<typeof setTimeout>>();
        this.artefactFollowDisposables.push(
            vscode.workspace.onDidChangeTextDocument(async e => {
                const doc = e.document;
                if (!this.isEmbeddedPreviewOpen() || doc.isUntitled || !doc.isDirty) {
                    return;
                }
                if (!(await this.isBinoManifest(doc))) {
                    return;
                }
                const key = doc.uri.toString();
                const existing = timers.get(key);
                if (existing) {
                    clearTimeout(existing);
                }
                timers.set(key, setTimeout(() => {
                    timers.delete(key);
                    // Re-check: the panel may have closed during the debounce.
                    if (this.isEmbeddedPreviewOpen() && doc.isDirty) {
                        void this.pushOverride(doc.uri.fsPath, doc.getText());
                    }
                }, 150));
            }),
            // On save the buffer equals disk; drop the override so the disk path
            // (with its embedding cache) serves again.
            vscode.workspace.onDidSaveTextDocument(async doc => {
                if (!this.isEmbeddedPreviewOpen() || !(await this.isBinoManifest(doc))) {
                    return;
                }
                const existing = timers.get(doc.uri.toString());
                if (existing) {
                    clearTimeout(existing);
                    timers.delete(doc.uri.toString());
                }
                void this.clearOverride(doc.uri.fsPath);
            })
        );
    }

    /** Whether a document is a Bino YAML manifest (.yaml/.yml the language server accepts). */
    private async isBinoManifest(doc: vscode.TextDocument): Promise<boolean> {
        const name = doc.fileName.toLowerCase();
        if (!name.endsWith('.yaml') && !name.endsWith('.yml')) {
            return false;
        }
        return (await this.indexer?.isBinoDocument(doc)) ?? false;
    }

    /** POST a buffer override and, on success, reload the embedded iframe. */
    private async pushOverride(fsPath: string, content: string): Promise<void> {
        const ok = await this.postOverrideRequest({ file: fsPath, content });
        if (ok) {
            this.overriddenFiles.add(fsPath);
            this.artefactPanel?.webview.postMessage({ type: 'bino:reload' });
        }
    }

    /** POST a clear for a single file and reload so the disk version shows. */
    private async clearOverride(fsPath: string): Promise<void> {
        if (!this.overriddenFiles.has(fsPath)) {
            return;
        }
        const ok = await this.postOverrideRequest({ file: fsPath, clear: true });
        if (ok) {
            this.overriddenFiles.delete(fsPath);
            this.artefactPanel?.webview.postMessage({ type: 'bino:reload' });
        }
    }

    /** Push every currently-dirty Bino manifest as an override (no reload spam - caller reloads once). */
    private async pushAllDirtyOverrides(): Promise<void> {
        for (const doc of vscode.workspace.textDocuments) {
            if (doc.isUntitled || !doc.isDirty || !(await this.isBinoManifest(doc))) {
                continue;
            }
            const ok = await this.postOverrideRequest({ file: doc.uri.fsPath, content: doc.getText() });
            if (ok) {
                this.overriddenFiles.add(doc.uri.fsPath);
            }
        }
    }

    /** Clear every override we pushed (on panel close); best-effort. */
    private async clearAllOverrides(): Promise<void> {
        const files = [...this.overriddenFiles];
        this.overriddenFiles.clear();
        for (const file of files) {
            await this.postOverrideRequest({ file, clear: true });
        }
    }

    /**
     * POST to /__bino/embedding/override on 127.0.0.1 (matching the server bind,
     * and Node-side so no CORS applies). Returns true on a 2xx response.
     */
    private async postOverrideRequest(body: { file: string; content?: string; clear?: boolean }): Promise<boolean> {
        const port = this.getPreviewPort();
        try {
            const res = await fetch(`http://127.0.0.1:${port}/__bino/embedding/override`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
            if (!res.ok) {
                this.outputChannel.appendLine(`[Preview] Override POST failed: HTTP ${res.status}`);
                return false;
            }
            return true;
        } catch (err) {
            this.outputChannel.appendLine(`[Preview] Override POST error: ${err}`);
            return false;
        }
    }

    /** Tear down the active-editor follow listeners. */
    private disposeArtefactFollow(): void {
        for (const d of this.artefactFollowDisposables) {
            d.dispose();
        }
        this.artefactFollowDisposables = [];
    }

    /** Generate the embedded webview HTML: chrome-free artefact iframe + SSE auto-reload. */
    private getEmbeddedWebviewContent(base: string, name: string, kind: string): string {
        const nonce = getNonce();
        const origin = urlOrigin(base);
        const csp = [
            "default-src 'none'",
            `img-src http://localhost:* http://127.0.0.1:* ${origin} data:`,
            "style-src 'unsafe-inline'",
            `script-src 'nonce-${nonce}'`,
            `frame-src http://localhost:* http://127.0.0.1:* ${origin}`,
            `connect-src http://localhost:* http://127.0.0.1:* ${origin}`
        ].join('; ');
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="${csp}">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Bino Artefact Preview</title>
    <style>
        html, body {
            margin: 0;
            padding: 0;
            width: 100%;
            height: 100%;
            overflow: hidden;
            background: var(--vscode-editor-background);
            color: var(--vscode-foreground);
            font-family: var(--vscode-font-family);
        }
        #bar {
            height: 24px;
            line-height: 24px;
            padding: 0 8px;
            font-size: 12px;
            display: flex;
            gap: 8px;
            align-items: center;
            border-bottom: 1px solid var(--vscode-panel-border);
        }
        #status { opacity: 0.7; }
        .grow { flex: 1; }
        #annotate { font-size: 11px; padding: 0 8px; height: 18px; cursor: pointer;
            background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); border: none; border-radius: 2px; }
        #annotate.on { background: var(--vscode-button-background); color: var(--vscode-button-foreground); }
        #frame {
            width: 100%;
            height: calc(100% - 25px);
            border: none;
            display: block;
            background: #fff;
        }
    </style>
</head>
<body>
    <div id="bar"><span id="title"></span><span id="status"></span><span class="grow"></span><button id="annotate" hidden title="Click a data point to annotate it. Ctrl/Cmd+click still shows the source.">Annotate</button></div>
    <iframe id="frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups"></iframe>
    <script nonce="${nonce}">
        const base = ${JSON.stringify(base)};
        let currentName = ${JSON.stringify(name)};
        let currentKind = ${JSON.stringify(kind)};
        const frame = document.getElementById('frame');
        const titleEl = document.getElementById('title');
        const statusEl = document.getElementById('status');

        function setStatus(s) { statusEl.textContent = s ? '(' + s + ')' : ''; }
        function load(reason) {
            titleEl.textContent = currentName;
            frame.dataset.loaded = '';
            frame.src = base + '/__embedding/' + encodeURIComponent(currentName)
                + '?kind=' + encodeURIComponent(currentKind) + '&t=' + Date.now();
            setStatus(reason);
        }
        frame.addEventListener('load', () => { frame.dataset.loaded = '1'; setStatus(''); });
        load('loading');

        // One delayed retry to cover the boot 503 race.
        setTimeout(() => { if (!frame.dataset.loaded) { load('retry'); } }, 1500);

        // Reload on preview-server refresh events (embedding cache is reset each refresh).
        function connect() {
            let es;
            try { es = new EventSource(base + '/__preview/events'); } catch (e) { return; }
            es.addEventListener('ready', () => load('ready'));
            es.addEventListener('refresh-done', () => load('updated'));
            es.addEventListener('path-changed', () => load('updated'));
            // EventSource auto-reconnects on error.
        }
        connect();

        const vscode = acquireVsCodeApi();
${annotateScript()}
        // Messages from the extension, and click-to-source from the iframe.
        window.addEventListener('message', (event) => {
            const msg = event.data;
            if (event.source === frame.contentWindow) {
                if (msg && (msg.type === 'bino:revealSource' || msg.type === 'bino:pickPoint')) {
                    vscode.postMessage(msg);
                } else if (msg && msg.type === 'bino:capabilities') {
                    annotateCapable(msg);
                }
                return;
            }
            if (msg && msg.type === 'bino:setArtefact') {
                currentName = msg.name;
                currentKind = msg.kind;
                load('switch');
            } else if (msg && msg.type === 'bino:reload') {
                // Buffer override applied: reload this component (cache-busted,
                // bypasses the same-name skip) so unsaved edits appear.
                load('updated');
            }
        });
    </script>
</body>
</html>`;
    }

    /**
     * Annotate mode messages of a preview webview: its page can annotate
     * (`bino:capabilities`), the user turned the mode on or off, or picked a point.
     */
    private handleAnnotateMessage(panel: vscode.WebviewPanel, msg: { type?: string; [k: string]: unknown }): void {
        if (msg.type === 'bino:capabilities') {
            if (!this.annotating.has(panel)) { this.annotating.set(panel, false); }
            this.annotateEmitter.fire(this.isAnnotating());
        } else if (msg.type === 'bino:annotateState') {
            this.annotating.set(panel, msg.on === true);
            this.annotateEmitter.fire(this.isAnnotating());
        } else if (msg.type === 'bino:pickPoint' && typeof msg.kind === 'string' && typeof msg.name === 'string') {
            this.pickPointEmitter.fire(msg as unknown as PickPointMessage);
        }
    }

    private forgetAnnotate(panel: vscode.WebviewPanel): void {
        if (this.annotating.delete(panel)) { this.annotateEmitter.fire(this.isAnnotating()); }
    }

    /** Whether an open preview is in annotate mode. */
    isAnnotating(): boolean {
        return [...this.annotating.values()].some(Boolean);
    }

    /** Handle reveal source message from preview webview */
    private async handleRevealSource(msg: RevealSourceMessage): Promise<void> {
        if (msg.sourceFile && typeof msg.line === 'number' && msg.line > 0) {
            await this.revealFileLine(msg.sourceFile, msg.line - 1);
            return;
        }
        // Markdown source sections carry a project-root-relative file path
        // (no manifest to index) - open the file directly.
        if (msg.file) {
            const root = this.getWorkspaceRoot();
            const abs = path.isAbsolute(msg.file) ? msg.file : path.join(root ?? '', msg.file);
            try {
                const document = await vscode.workspace.openTextDocument(vscode.Uri.file(abs));
                await vscode.window.showTextDocument(document, {
                    viewColumn: vscode.ViewColumn.One,
                    preview: false
                });
            } catch (err) {
                this.outputChannel.appendLine(`[Preview] Failed to open source file ${msg.file}: ${err}`);
            }
            return;
        }

        if (!this.indexer) {
            this.outputChannel.appendLine('[Preview] No indexer available for source navigation');
            return;
        }

        // For ref children, look up the referenced document by kind:ref
        // For inline children, we need to find them within their parent LayoutPage
        const searchName = msg.ref || msg.name;
        if (!searchName || !msg.kind) {
            this.outputChannel.appendLine('[Preview] Missing kind or name for source navigation');
            return;
        }

        const documents = this.indexer.getDocuments([msg.kind]);
        const targetDoc = documents.find(doc => doc.name === searchName);

        if (targetDoc) {
            // Found a standalone document, open it
            try {
                const uri = vscode.Uri.file(targetDoc.file);
                const document = await vscode.workspace.openTextDocument(uri);
                const editor = await vscode.window.showTextDocument(document, {
                    viewColumn: vscode.ViewColumn.One,
                    preview: false
                });

                // Navigate to the document start (position 1 = first doc in file)
                const lineNumber = this.findDocumentLine(document, targetDoc.position);
                const position = new vscode.Position(lineNumber, 0);
                const range = new vscode.Range(position, position);
                editor.selection = new vscode.Selection(position, position);
                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
            } catch (err) {
                this.outputChannel.appendLine(`[Preview] Failed to open document: ${err}`);
            }
        } else if (msg.name && !msg.ref) {
            // Inline child - search for it in LayoutPage documents
            this.outputChannel.appendLine(`[Preview] Inline child "${msg.name}" of kind "${msg.kind}" - searching in LayoutPages...`);
            await this.searchAndRevealInlineChild(msg.kind, msg.name);
        } else {
            this.outputChannel.appendLine(`[Preview] Document ${msg.kind}:${searchName} not found in index`);
        }
    }

    /** Open a file (absolute, or relative to the project root) at a 0-based line. */
    private async revealFileLine(file: string, line: number): Promise<void> {
        const abs = path.isAbsolute(file) ? file : path.join(this.getWorkspaceRoot() ?? '', file);
        try {
            const document = await vscode.workspace.openTextDocument(vscode.Uri.file(abs));
            const editor = await vscode.window.showTextDocument(document, {
                viewColumn: vscode.ViewColumn.One,
                preview: false
            });
            const position = new vscode.Position(Math.min(line, document.lineCount - 1), 0);
            editor.selection = new vscode.Selection(position, position);
            editor.revealRange(new vscode.Range(position, position), vscode.TextEditorRevealType.InCenter);
        } catch (err) {
            this.outputChannel.appendLine(`[Preview] Failed to open source file ${file}: ${err}`);
        }
    }

    /** Search for inline child in LayoutPage files and reveal it */
    private async searchAndRevealInlineChild(kind: string, name: string): Promise<void> {
        // Get all LayoutPage documents
        const layoutPages = this.indexer?.getDocuments(['LayoutPage']) || [];

        for (const layoutPage of layoutPages) {
            try {
                const uri = vscode.Uri.file(layoutPage.file);
                const document = await vscode.workspace.openTextDocument(uri);
                const text = document.getText();

                // Search for the inline child pattern:
                // - kind: <kind>
                //   metadata:
                //     name: <name>
                // or within a children array
                const lines = text.split('\n');
                for (let i = 0; i < lines.length; i++) {
                    const line = lines[i];
                    // Look for "name: <name>" within metadata of a child with matching kind
                    if (line.includes(`name: ${name}`) || line.includes(`name: "${name}"`) || line.includes(`name: '${name}'`)) {
                        // Look backwards to verify the kind matches
                        for (let j = i - 1; j >= Math.max(0, i - 10); j--) {
                            const prevLine = lines[j];
                            if (prevLine.includes(`kind: ${kind}`) || prevLine.includes(`kind: "${kind}"`) || prevLine.includes(`kind: '${kind}'`)) {
                                // Found it! Open and navigate
                                const editor = await vscode.window.showTextDocument(document, {
                                    viewColumn: vscode.ViewColumn.One,
                                    preview: false
                                });
                                const position = new vscode.Position(i, 0);
                                const range = new vscode.Range(position, position);
                                editor.selection = new vscode.Selection(position, position);
                                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
                                return;
                            }
                        }
                    }
                }
            } catch (err) {
                // Continue searching other files
            }
        }

        this.outputChannel.appendLine(`[Preview] Could not find inline child "${name}" of kind "${kind}"`);
    }

    /**
     * Find the line number where the Nth document starts in a multi-doc YAML file.
     * @param document The VS Code text document
     * @param docIndex 1-based document index
     * @returns 0-based line number
     */
    private findDocumentLine(document: vscode.TextDocument, docIndex: number): number {
        const text = document.getText();
        const lines = text.split('\n');

        let currentDocIndex = 0;

        for (let lineNum = 0; lineNum < lines.length; lineNum++) {
            const line = lines[lineNum].trim();

            if (lineNum === 0) {
                if (line === '---') {
                    continue;
                } else if (line && !line.startsWith('#')) {
                    currentDocIndex = 1;
                    if (currentDocIndex === docIndex) {
                        return 0;
                    }
                }
            } else if (line === '---') {
                currentDocIndex++;
                if (currentDocIndex === docIndex) {
                    return lineNum + 1;
                }
            } else if (currentDocIndex === 0 && line && !line.startsWith('#')) {
                currentDocIndex = 1;
                if (currentDocIndex === docIndex) {
                    return lineNum;
                }
            }
        }

        return 0;
    }

    /** Generate HTML content for the webview with an iframe */
    private getWebviewContent(base: string, route: string): string {
        const nonce = getNonce();
        const origin = urlOrigin(base);
        const csp = [
            "default-src 'none'",
            "style-src 'unsafe-inline'",
            `script-src 'nonce-${nonce}'`,
            `frame-src http://localhost:* http://127.0.0.1:* ${origin}`
        ].join('; ');
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="${csp}">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Bino Preview</title>
    <style>
        html, body, iframe {
            margin: 0;
            padding: 0;
            width: 100%;
            height: 100%;
            border: none;
            overflow: hidden;
        }
        #annotate { position: fixed; right: 12px; bottom: 12px; font-size: 12px; padding: 4px 10px; cursor: pointer; opacity: 0.85;
            background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); border: none; border-radius: 3px; }
        #annotate.on { background: var(--vscode-button-background); color: var(--vscode-button-foreground); opacity: 1; }
    </style>
</head>
<body>
    <iframe id="frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups"></iframe>
    <button id="annotate" hidden title="Click a data point to annotate it. Ctrl/Cmd+click still shows the source.">Annotate</button>
    <script nonce="${nonce}">
        const vscode = acquireVsCodeApi();
        const frame = document.getElementById('frame');
        frame.src = ${JSON.stringify(base + route)};
${annotateScript()}
        window.addEventListener('message', (event) => {
            const msg = event.data;
            if (!msg) {
                return;
            }
            // Click-to-source and picked points from the iframe go to the extension.
            if (event.source === frame.contentWindow) {
                if (msg.type === 'bino:revealSource' || msg.type === 'bino:pickPoint') {
                    vscode.postMessage(msg);
                } else if (msg.type === 'bino:capabilities') {
                    annotateCapable(msg);
                }
                return;
            }
            if (msg.type === 'bino:navigate') {
                frame.src = msg.url;
            }
        });
    </script>
</body>
</html>`;
    }

    /** Get current preview status */
    getStatus(): PreviewStatus {
        return this.previewStatus;
    }

    /** Whether the preview runs as this window's own `bino preview` process, not in the daemon. */
    get runsOwnProcess(): boolean {
        return this.previewProcess !== undefined;
    }

    /** The port of the running preview, if one runs. */
    get port(): number | undefined {
        return this.activePort;
    }

    /** Show preview menu */
    async showPreviewMenu(): Promise<void> {
        const items: vscode.QuickPickItem[] = [];

        if (this.previewStatus === 'running') {
            items.push(
                { label: '$(link-external) Open in Browser', description: 'Open preview in default browser' },
                { label: '$(window) Open in Webview', description: 'Open preview side-by-side in VS Code' },
                { label: '$(debug-restart) Restart Preview', description: 'Stop and restart the preview server' },
                { label: '$(debug-stop) Stop Preview', description: 'Stop the preview server' }
            );
        } else {
            items.push(
                { label: '$(play) Start Preview', description: 'Start the preview server' }
            );
        }

        const selection = await vscode.window.showQuickPick(items, {
            placeHolder: 'Bino Preview Actions'
        });

        if (!selection) {
            return;
        }

        if (selection.label.includes('Open in Browser')) {
            await this.openInBrowser();
        } else if (selection.label.includes('Open in Webview')) {
            await this.openPreviewInWebview();
        } else if (selection.label.includes('Restart')) {
            await this.restartPreview();
        } else if (selection.label.includes('Stop')) {
            await this.stopPreview();
        } else if (selection.label.includes('Start')) {
            await this.startPreview();
        }
    }

    /** Handle SSE preview-status events from daemon */
    handlePreviewStatusEvent(data: { status: string; url?: string; port?: number }): void {
        if (data.status === 'stopped' && this.daemonStopWaiter) {
            const done = this.daemonStopWaiter;
            this.daemonStopWaiter = undefined;
            done();
        }
        if (!this.usingDaemonPreview) {
            return;
        }
        switch (data.status) {
            case 'running':
                if (data.port) {
                    this.activePort = data.port;
                }
                this.setStatus('running');
                break;
            case 'stopped': {
                const port = this.getPreviewPort();
                const wasStarting = this.previewStatus === 'starting';
                this.usingDaemonPreview = false;
                this.activePort = undefined;
                if (wasStarting) {
                    this.setStatus('error');
                    vscode.window.showErrorMessage(previewStartError(port, ''));
                } else {
                    this.setStatus('stopped');
                }
                break;
            }
            case 'error':
                this.usingDaemonPreview = false;
                this.activePort = undefined;
                this.setStatus('error');
                break;
        }
    }

    /** Dispose resources */
    dispose(): void {
        void this.stopPreview();
        this.previewPanel?.dispose();
        this.artefactPanel?.dispose();
        this.fullFollowDisposables.forEach(d => d.dispose());
        this.disposeArtefactFollow();
        this._onStatusChange.dispose();
    }
}

/**
 * The script of both preview webviews for annotate mode. The Annotate button
 * shows only when the page in the frame says it can annotate, so a preview of
 * an older bino has none. Each page that loads in the frame says so again, and
 * gets the current mode, so the mode survives a reload of the frame.
 */
function annotateScript(): string {
    return `
        const annotateBtn = document.getElementById('annotate');
        let annotateOn = false;
        function sendAnnotate() {
            annotateBtn.classList.toggle('on', annotateOn);
            annotateBtn.textContent = annotateOn ? 'Annotating: click a point' : 'Annotate';
            if (frame.contentWindow) frame.contentWindow.postMessage({ type: 'bino:annotateMode', on: annotateOn }, '*');
        }
        function annotateCapable(msg) {
            if (!msg.annotate) return;
            annotateBtn.hidden = false;
            vscode.postMessage({ type: 'bino:capabilities', annotate: msg.annotate });
            sendAnnotate();
        }
        annotateBtn.addEventListener('click', () => {
            annotateOn = !annotateOn;
            sendAnnotate();
            vscode.postMessage({ type: 'bino:annotateState', on: annotateOn });
        });
`;
}
