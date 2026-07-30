import * as vscode from 'vscode';

/**
 * BinoValidator is a thin reader over VS Code's diagnostics. Diagnostics are now
 * produced by the bino Language Server (`bino lsp`) and published to the Problems
 * panel under the `bino` source; this class exposes summaries of them to the tree
 * views and status bar without owning a DiagnosticCollection of its own (which
 * would duplicate the LSP's diagnostics).
 */
export class BinoValidator {
    private readonly source = 'bino';
    private readonly disposables: vscode.Disposable[] = [];

    private _onDidChangeDiagnostics = new vscode.EventEmitter<void>();
    readonly onDidChangeDiagnostics: vscode.Event<void> = this._onDidChangeDiagnostics.event;

    // Retained for API compatibility with the tree/status-bar subscribers; the
    // LSP validates continuously, so these never fire a distinct phase.
    private _onDidStartValidation = new vscode.EventEmitter<void>();
    readonly onDidStartValidation: vscode.Event<void> = this._onDidStartValidation.event;

    private _onDidFinishValidation = new vscode.EventEmitter<void>();
    readonly onDidFinishValidation: vscode.Event<void> = this._onDidFinishValidation.event;

    constructor(_outputChannel: vscode.OutputChannel) {
        this.disposables.push(
            vscode.languages.onDidChangeDiagnostics(() => this._onDidChangeDiagnostics.fire())
        );
    }

    /** The LSP validates live, so there is never a discrete in-progress phase. */
    get isValidating(): boolean {
        return false;
    }

    /** Returns the bino diagnostics VS Code currently holds for a URI. */
    getDiagnosticsForUri(uri: vscode.Uri): readonly vscode.Diagnostic[] {
        return vscode.languages.getDiagnostics(uri).filter(d => d.source === this.source);
    }

    /** Aggregates bino diagnostics across the workspace by severity. */
    getWorkspaceSummary(): { errors: number; warnings: number; info: number; hints: number } {
        let errors = 0;
        let warnings = 0;
        let info = 0;
        let hints = 0;
        for (const [, diags] of vscode.languages.getDiagnostics()) {
            for (const diag of diags) {
                if (diag.source !== this.source) {
                    continue;
                }
                switch (diag.severity) {
                    case vscode.DiagnosticSeverity.Error:
                        errors++;
                        break;
                    case vscode.DiagnosticSeverity.Warning:
                        warnings++;
                        break;
                    case vscode.DiagnosticSeverity.Information:
                        info++;
                        break;
                    case vscode.DiagnosticSeverity.Hint:
                        hints++;
                        break;
                }
            }
        }
        return { errors, warnings, info, hints };
    }

    dispose(): void {
        for (const d of this.disposables) {
            d.dispose();
        }
        this._onDidChangeDiagnostics.dispose();
        this._onDidStartValidation.dispose();
        this._onDidFinishValidation.dispose();
    }
}
