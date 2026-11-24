import * as vscode from 'vscode';
import { DaemonClient, DaemonStatus } from './daemonClient';
import { BinoPreviewManager, PreviewStatus } from './preview';
import { WorkspaceIndexer } from './indexer';
import { BinoValidator } from './validation';

/** What the single Bino status bar item summarizes. */
export interface StatusState {
    /** undefined when the daemon is disabled for this window. */
    daemon: DaemonStatus | undefined;
    daemonVersion?: string;
    preview: PreviewStatus;
    previewPort?: number;
    indexing: boolean;
    validating: boolean;
    errors: number;
    warnings: number;
}

export interface StatusView {
    text: string;
    tooltip: string;
    background?: 'error' | 'warning';
}

/** Render the status bar item from the state. */
export function statusView(state: StatusState): StatusView {
    const busy = state.indexing || state.validating || state.daemon === 'connecting' || state.preview === 'starting';
    let text: string;
    let background: StatusView['background'];
    if (busy) {
        text = '$(sync~spin) Bino';
    } else if (state.errors > 0) {
        text = `$(error) Bino: ${state.errors}`;
        background = 'error';
    } else if (state.daemon === 'error' || state.preview === 'error') {
        text = '$(warning) Bino';
        background = 'warning';
    } else if (state.warnings > 0) {
        text = `$(warning) Bino: ${state.warnings}`;
    } else {
        text = '$(check) Bino';
    }
    if (state.preview === 'running') {
        text += ' $(eye)';
    }

    const daemon = state.daemon === undefined
        ? 'disabled'
        : state.daemon + (state.daemon === 'connected' && state.daemonVersion ? ` (bino ${state.daemonVersion})` : '');
    const preview = state.preview + (state.preview === 'running' && state.previewPort ? ` on port ${state.previewPort}` : '');
    const tooltip = [
        `Daemon: ${daemon}`,
        `Preview: ${preview}`,
        `Index: ${state.indexing ? 'indexing' : 'ready'}`,
        `Diagnostics: ${state.validating ? 'validating' : `${state.errors} errors, ${state.warnings} warnings`}`,
        'Click for Bino actions',
    ].join('\n');
    return { text, tooltip, background };
}

/** One Bino status bar item; a click opens a menu of the common actions. */
export class BinoStatusBar implements vscode.Disposable {
    private item: vscode.StatusBarItem;
    private disposables: vscode.Disposable[] = [];

    constructor(
        private readonly daemon: () => DaemonClient | undefined,
        private readonly preview: BinoPreviewManager,
        private readonly indexer: WorkspaceIndexer,
        private readonly validator: BinoValidator,
        private readonly outputChannel: vscode.OutputChannel
    ) {
        this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
        this.item.name = 'Bino';
        this.item.command = 'bino.statusMenu';
        const update = () => this.update();
        this.disposables.push(
            vscode.commands.registerCommand('bino.statusMenu', () => this.showMenu()),
            preview.onStatusChange(update),
            indexer.onDidStartIndex(update),
            indexer.onDidFinishIndex(update),
            validator.onDidStartValidation(update),
            validator.onDidFinishValidation(update),
            validator.onDidChangeDiagnostics(update)
        );
        const client = daemon();
        if (client) {
            this.disposables.push(client.onStatusChange(update));
        }
        this.update();
        this.item.show();
    }

    update(): void {
        const client = this.daemon();
        const summary = this.validator.getWorkspaceSummary();
        const view = statusView({
            daemon: client?.getStatus(),
            daemonVersion: client?.version,
            preview: this.preview.getStatus(),
            previewPort: this.preview.port,
            indexing: this.indexer.isIndexing,
            validating: this.validator.isValidating,
            errors: summary.errors,
            warnings: summary.warnings,
        });
        this.item.text = view.text;
        this.item.tooltip = view.tooltip;
        this.item.backgroundColor = view.background
            ? new vscode.ThemeColor(`statusBarItem.${view.background}Background`)
            : undefined;
    }

    private async showMenu(): Promise<void> {
        type Item = vscode.QuickPickItem & { run: () => unknown };
        const client = this.daemon();
        const summary = this.validator.getWorkspaceSummary();
        const items: Item[] = [
            {
                label: '$(eye) Preview...',
                description: this.preview.getStatus(),
                run: () => vscode.commands.executeCommand('bino.previewMenu'),
            },
            ...(client ? [{
                label: '$(server-process) Restart Daemon',
                description: client.getStatus(),
                run: () => vscode.commands.executeCommand('bino.restartDaemon'),
            }] : []),
            { label: '$(refresh) Refresh Index', run: () => vscode.commands.executeCommand('bino.refreshIndex') },
            {
                label: '$(warning) Show Problems',
                description: `${summary.errors} errors, ${summary.warnings} warnings`,
                run: () => vscode.commands.executeCommand('workbench.actions.view.problems'),
            },
            { label: '$(output) Open Log', run: () => this.outputChannel.show() },
            { label: '$(checklist) Check Setup', run: () => vscode.commands.executeCommand('bino.checkSetup') },
            { label: '$(gear) Settings', run: () => vscode.commands.executeCommand('bino.openSettings') },
        ];
        const pick = await vscode.window.showQuickPick(items, { title: 'Bino' });
        await pick?.run();
    }

    dispose(): void {
        this.item.dispose();
        this.disposables.forEach(d => d.dispose());
    }
}
