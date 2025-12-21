import * as vscode from 'vscode';
import * as cp from 'child_process';
import { WorkspaceIndexer, LSPDocument } from './indexer';

export type PreviewStatus = 'stopped' | 'starting' | 'running' | 'error';

/**
 * BinoPreviewManager manages the lifecycle of bino preview and build processes.
 */
export class BinoPreviewManager {
    private outputChannel: vscode.OutputChannel;
    private previewProcess: cp.ChildProcess | undefined;
    private previewStatus: PreviewStatus = 'stopped';
    private statusBarItem: vscode.StatusBarItem;
    private previewPanel: vscode.WebviewPanel | undefined;
    private indexer: WorkspaceIndexer | undefined;

    // Event emitter for status changes
    private _onStatusChange = new vscode.EventEmitter<PreviewStatus>();
    readonly onStatusChange = this._onStatusChange.event;

    constructor(outputChannel: vscode.OutputChannel, indexer?: WorkspaceIndexer) {
        this.outputChannel = outputChannel;
        this.indexer = indexer;
        this.statusBarItem = vscode.window.createStatusBarItem(
            vscode.StatusBarAlignment.Left,
            100
        );
        this.statusBarItem.command = 'bino.previewMenu';
        this.updateStatusBar();
    }

    /** Set the workspace indexer for source navigation */
    setIndexer(indexer: WorkspaceIndexer): void {
        this.indexer = indexer;
    }

    /** Get the configured bino CLI path */
    private getBinoPath(): string {
        const config = vscode.workspace.getConfiguration('bino');
        const binPath = config.get<string>('binPath');
        return binPath && binPath.trim() ? binPath : 'bino';
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

    /** Get the preview port from config */
    private getPreviewPort(): number {
        const config = vscode.workspace.getConfiguration('bino');
        return config.get<number>('previewPort') ?? 3000;
    }

    /** Update status bar display */
    private updateStatusBar(): void {
        switch (this.previewStatus) {
            case 'stopped':
                this.statusBarItem.text = '$(play) Bino Preview';
                this.statusBarItem.tooltip = 'Click to start Bino preview';
                this.statusBarItem.backgroundColor = undefined;
                break;
            case 'starting':
                this.statusBarItem.text = '$(sync~spin) Bino Preview';
                this.statusBarItem.tooltip = 'Starting preview...';
                this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
                break;
            case 'running':
                this.statusBarItem.text = '$(check) Bino Preview';
                this.statusBarItem.tooltip = 'Preview running - click to open or stop';
                this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.prominentBackground');
                break;
            case 'error':
                this.statusBarItem.text = '$(error) Bino Preview';
                this.statusBarItem.tooltip = 'Preview error - click to retry';
                this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.errorBackground');
                break;
        }
        this.statusBarItem.show();
    }

    /** Set preview status and emit event */
    private setStatus(status: PreviewStatus): void {
        this.previewStatus = status;
        this.updateStatusBar();
        this._onStatusChange.fire(status);
    }

    /** Start the preview server */
    async startPreview(): Promise<void> {
        if (this.previewProcess) {
            vscode.window.showInformationMessage('Preview is already running');
            return;
        }

        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            vscode.window.showWarningMessage('No workspace folder open');
            return;
        }

        const binPath = this.getBinoPath();
        const port = this.getPreviewPort();

        this.setStatus('starting');
        this.outputChannel.appendLine(`[Preview] Starting preview server on port ${port}...`);
        this.outputChannel.show(true);

        try {
            this.previewProcess = cp.spawn(binPath, ['preview', '--port', String(port)], {
                cwd: workDir,
                shell: true,
                stdio: ['ignore', 'pipe', 'pipe']
            });

            this.previewProcess.stdout?.on('data', (data: Buffer) => {
                const text = data.toString();
                this.outputChannel.append(text);

                // Detect when server is ready
                if (text.includes('listening') || text.includes('http://') || text.includes('ready')) {
                    this.setStatus('running');
                }
            });

            this.previewProcess.stderr?.on('data', (data: Buffer) => {
                this.outputChannel.append(data.toString());
            });

            this.previewProcess.on('error', (err) => {
                this.outputChannel.appendLine(`[Preview] Error: ${err.message}`);
                this.setStatus('error');
                this.previewProcess = undefined;
            });

            this.previewProcess.on('exit', (code) => {
                this.outputChannel.appendLine(`[Preview] Process exited with code ${code}`);
                if (this.previewStatus !== 'error') {
                    this.setStatus('stopped');
                }
                this.previewProcess = undefined;
            });

            // Give it a moment to start, then assume running if no error
            setTimeout(() => {
                if (this.previewStatus === 'starting') {
                    this.setStatus('running');
                }
            }, 2000);

        } catch (err) {
            this.outputChannel.appendLine(`[Preview] Failed to start: ${err}`);
            this.setStatus('error');
            vscode.window.showErrorMessage(`Failed to start preview: ${err}`);
        }
    }

    /** Stop the preview server */
    stopPreview(): void {
        if (!this.previewProcess) {
            vscode.window.showInformationMessage('Preview is not running');
            return;
        }

        this.outputChannel.appendLine('[Preview] Stopping preview server...');

        try {
            this.previewProcess.kill('SIGTERM');
            // Force kill after 3 seconds if not terminated
            setTimeout(() => {
                if (this.previewProcess) {
                    this.previewProcess.kill('SIGKILL');
                }
            }, 3000);
        } catch (err) {
            this.outputChannel.appendLine(`[Preview] Error stopping: ${err}`);
        }

        this.previewProcess = undefined;
        this.setStatus('stopped');
    }

    /** Restart the preview server */
    async restartPreview(): Promise<void> {
        this.stopPreview();
        // Wait a bit for cleanup
        await new Promise(resolve => setTimeout(resolve, 500));
        await this.startPreview();
    }

    /** Open preview in browser */
    openInBrowser(): void {
        const port = this.getPreviewPort();
        const url = `http://localhost:${port}`;
        vscode.env.openExternal(vscode.Uri.parse(url));
    }

    /** Open preview in a VS Code webview panel (side-by-side) */
    async openPreviewInWebview(): Promise<void> {
        // Ensure preview is running
        if (this.previewStatus !== 'running') {
            await this.startPreview();
            // Wait a moment for the server to be ready
            await new Promise(resolve => setTimeout(resolve, 1500));
        }

        const port = this.getPreviewPort();
        const url = `http://localhost:${port}`;

        // Reuse existing panel if it exists
        if (this.previewPanel) {
            this.previewPanel.reveal(vscode.ViewColumn.Beside);
            // Update the content in case port changed
            this.previewPanel.webview.html = this.getWebviewContent(url);
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

        this.previewPanel.webview.html = this.getWebviewContent(url);

        // Handle messages from the webview (click-to-source)
        this.previewPanel.webview.onDidReceiveMessage(async (msg) => {
            if (msg.type === 'bino:revealSource') {
                await this.handleRevealSource(msg);
            }
        });

        // Clear reference when panel is closed
        this.previewPanel.onDidDispose(() => {
            this.previewPanel = undefined;
        });
    }

    /** Handle reveal source message from preview webview */
    private async handleRevealSource(msg: { kind: string; name: string; ref: string }): Promise<void> {
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
    private getWebviewContent(url: string): string {
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
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
    </style>
</head>
<body>
    <iframe src="${url}" sandbox="allow-scripts allow-same-origin allow-forms allow-popups"></iframe>
    <script>
        // Forward messages from iframe to VS Code extension
        const vscode = acquireVsCodeApi();
        window.addEventListener('message', (event) => {
            const msg = event.data;
            if (msg && msg.type === 'bino:revealSource') {
                vscode.postMessage(msg);
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

    /** Run build command */
    async runBuild(): Promise<boolean> {
        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            vscode.window.showWarningMessage('No workspace folder open');
            return false;
        }

        const binPath = this.getBinoPath();

        this.outputChannel.appendLine('[Build] Starting build...');
        this.outputChannel.show(true);

        return new Promise((resolve) => {
            const options: cp.ExecOptionsWithStringEncoding = {
                cwd: workDir,
                maxBuffer: 50 * 1024 * 1024, // 50MB for build output
                timeout: 300000, // 5 minutes
                encoding: 'utf8'
            };

            cp.exec(`${binPath} build`, options, (error, stdout, stderr) => {
                if (stdout) {
                    this.outputChannel.append(stdout);
                }
                if (stderr) {
                    this.outputChannel.append(stderr);
                }

                if (error) {
                    this.outputChannel.appendLine(`[Build] Failed with code ${error.code}`);
                    vscode.window.showErrorMessage('Build failed - see output for details');
                    resolve(false);
                    return;
                }

                this.outputChannel.appendLine('[Build] Completed successfully');
                vscode.window.showInformationMessage('✓ Build completed successfully');
                resolve(true);
            });
        });
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
            this.openInBrowser();
        } else if (selection.label.includes('Open in Webview')) {
            await this.openPreviewInWebview();
        } else if (selection.label.includes('Restart')) {
            await this.restartPreview();
        } else if (selection.label.includes('Stop')) {
            this.stopPreview();
        } else if (selection.label.includes('Start')) {
            await this.startPreview();
        }
    }

    /** Dispose resources */
    dispose(): void {
        this.stopPreview();
        this.previewPanel?.dispose();
        this.statusBarItem.dispose();
        this._onStatusChange.dispose();
    }
}
