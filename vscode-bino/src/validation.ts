import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as path from 'path';
import * as fs from 'fs';

/** The project configuration filename that marks a bino project root */
const PROJECT_CONFIG_FILE = 'bino.toml';

/** Diagnostic from bino lsp-helper validate */
export interface LSPDiagnostic {
    file: string;
    position: number;
    line: number;
    column: number;
    severity: 'error' | 'warning' | 'info' | 'hint';
    message: string;
    code?: string;
    field?: string;
}

/** Result from bino lsp-helper validate */
export interface LSPValidateResult {
    valid: boolean;
    diagnostics: LSPDiagnostic[];
    error?: string;
}

/**
 * BinoValidator manages validation of bino documents and provides
 * diagnostics to VS Code's Problems panel.
 */
export class BinoValidator {
    private diagnosticCollection: vscode.DiagnosticCollection;
    private outputChannel: vscode.OutputChannel;
    private _validating = false;

    private _onDidChangeDiagnostics = new vscode.EventEmitter<void>();
    readonly onDidChangeDiagnostics: vscode.Event<void> = this._onDidChangeDiagnostics.event;

    private _onDidStartValidation = new vscode.EventEmitter<void>();
    readonly onDidStartValidation: vscode.Event<void> = this._onDidStartValidation.event;

    private _onDidFinishValidation = new vscode.EventEmitter<void>();
    readonly onDidFinishValidation: vscode.Event<void> = this._onDidFinishValidation.event;

    /** Returns true if validation is currently in progress */
    get isValidating(): boolean {
        return this._validating;
    }

    constructor(outputChannel: vscode.OutputChannel) {
        this.diagnosticCollection = vscode.languages.createDiagnosticCollection('bino');
        this.outputChannel = outputChannel;
    }

    /**
     * Get diagnostics for a specific URI.
     * Returns an empty array if no diagnostics exist for the URI.
     */
    getDiagnosticsForUri(uri: vscode.Uri): readonly vscode.Diagnostic[] {
        return this.diagnosticCollection.get(uri) ?? [];
    }

    /**
     * Get the maximum (worst) severity for a URI.
     * Returns undefined if no diagnostics exist for the URI.
     * Severity order: Error > Warning > Information > Hint
     */
    getMaxSeverityForUri(uri: vscode.Uri): vscode.DiagnosticSeverity | undefined {
        const diags = this.getDiagnosticsForUri(uri);
        if (diags.length === 0) {
            return undefined;
        }
        // DiagnosticSeverity: Error=0, Warning=1, Information=2, Hint=3
        // Lower value = more severe
        let maxSeverity = vscode.DiagnosticSeverity.Hint;
        for (const diag of diags) {
            if (diag.severity < maxSeverity) {
                maxSeverity = diag.severity;
            }
            if (maxSeverity === vscode.DiagnosticSeverity.Error) {
                break; // Can't get worse
            }
        }
        return maxSeverity;
    }

    /**
     * Get the underlying DiagnosticCollection (for advanced usage).
     */
    get collection(): vscode.DiagnosticCollection {
        return this.diagnosticCollection;
    }

    /** Get the configured bino CLI path */
    private getBinoPath(): string {
        const config = vscode.workspace.getConfiguration('bino');
        const binPath = config.get<string>('binPath');
        return binPath && binPath.trim() ? binPath : 'bino';
    }

    /**
     * Find the bino project root by searching for bino.toml.
     * Starts from the given directory and walks up the hierarchy.
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
                return undefined;
            }
            current = parent;
        }
    }

    /** Get workspace root directory (bino project root containing bino.toml) */
    private getWorkspaceRoot(): string | undefined {
        // Try active editor first
        const activeEditor = vscode.window.activeTextEditor;
        if (activeEditor?.document.uri.scheme === 'file') {
            const fileDir = path.dirname(activeEditor.document.uri.fsPath);
            const projectRoot = this.findProjectRoot(fileDir);
            if (projectRoot) {
                return projectRoot;
            }
        }

        // Fallback: search workspace folders
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

    /** Execute bino command and return stdout */
    private async execBino(args: string[]): Promise<string> {
        const binPath = this.getBinoPath();
        const workDir = this.getWorkspaceRoot();

        return new Promise((resolve, reject) => {
            const options: cp.ExecOptionsWithStringEncoding = {
                cwd: workDir,
                maxBuffer: 10 * 1024 * 1024,
                timeout: 60000, // 60 seconds for validation
                encoding: 'utf8'
            };

            const cmd = [binPath, ...args].join(' ');
            this.outputChannel.appendLine(`[Validate] Executing: ${cmd}`);

            cp.exec(cmd, options, (error, stdout, stderr) => {
                if (stderr) {
                    this.outputChannel.appendLine(`[Validate] Stderr: ${stderr}`);
                }
                // Validation returns JSON even on validation errors
                if (stdout) {
                    resolve(stdout);
                    return;
                }
                if (error) {
                    this.outputChannel.appendLine(`[Validate] Error: ${error.message}`);
                    reject(error);
                    return;
                }
                resolve('');
            });
        });
    }

    /** Convert LSP severity to VS Code DiagnosticSeverity */
    private toVSCodeSeverity(severity: string): vscode.DiagnosticSeverity {
        switch (severity) {
            case 'error':
                return vscode.DiagnosticSeverity.Error;
            case 'warning':
                return vscode.DiagnosticSeverity.Warning;
            case 'info':
                return vscode.DiagnosticSeverity.Information;
            case 'hint':
                return vscode.DiagnosticSeverity.Hint;
            default:
                return vscode.DiagnosticSeverity.Warning;
        }
    }

    /**
     * Find the line number where the Nth document starts in a multi-doc YAML file.
     * @param text The file text content
     * @param docIndex 1-based document index
     * @returns 0-based line number
     */
    private findDocumentLine(text: string, docIndex: number): number {
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

    /** Validate workspace and update diagnostics */
    async validateWorkspace(): Promise<boolean> {
        if (this._validating) {
            this.outputChannel.appendLine('[Validate] Validation already in progress');
            return false;
        }

        const workDir = this.getWorkspaceRoot();
        if (!workDir) {
            vscode.window.showWarningMessage('No workspace folder open');
            return false;
        }

        this._validating = true;
        this._onDidStartValidation.fire();
        this.diagnosticCollection.clear();

        try {
            const output = await this.execBino(['lsp-helper', 'validate', workDir]);
            const result: LSPValidateResult = JSON.parse(output);

            if (result.error) {
                this.outputChannel.appendLine(`[Validate] Error: ${result.error}`);
                vscode.window.showErrorMessage(`Validation error: ${result.error}`);
                return false;
            }

            // Ensure diagnostics is an array (may be null/undefined if valid)
            const diagnostics = result.diagnostics ?? [];

            // Group diagnostics by file
            const byFile = new Map<string, LSPDiagnostic[]>();
            for (const diag of diagnostics) {
                const file = diag.file || workDir;
                if (!byFile.has(file)) {
                    byFile.set(file, []);
                }
                byFile.get(file)!.push(diag);
            }

            // Convert to VS Code diagnostics
            for (const [file, diags] of byFile) {
                const uri = vscode.Uri.file(file);
                let fileContent: string | undefined;

                try {
                    const doc = await vscode.workspace.openTextDocument(uri);
                    fileContent = doc.getText();
                } catch {
                    // File may not exist or be readable
                }

                const vsDiags: vscode.Diagnostic[] = [];

                for (const diag of diags) {
                    let line = diag.line > 0 ? diag.line - 1 : 0;

                    // If we have position but no line, calculate from document position
                    if (diag.line === 0 && diag.position > 0 && fileContent) {
                        line = this.findDocumentLine(fileContent, diag.position);
                    }

                    const column = diag.column > 0 ? diag.column - 1 : 0;
                    const range = new vscode.Range(line, column, line, Number.MAX_SAFE_INTEGER);

                    const vsDiag = new vscode.Diagnostic(
                        range,
                        diag.message,
                        this.toVSCodeSeverity(diag.severity)
                    );

                    vsDiag.source = 'bino';
                    if (diag.code) {
                        vsDiag.code = diag.code;
                    }

                    vsDiags.push(vsDiag);
                }

                this.diagnosticCollection.set(uri, vsDiags);
            }

            const errorCount = diagnostics.filter(d => d.severity === 'error').length;
            const warningCount = diagnostics.filter(d => d.severity === 'warning').length;

            // Fire event to notify listeners (e.g., tree view) that diagnostics changed
            this._onDidChangeDiagnostics.fire();

            if (result.valid) {
                vscode.window.showInformationMessage('✓ Bino workspace validation passed');
            } else {
                vscode.window.showWarningMessage(
                    `Bino validation: ${errorCount} error(s), ${warningCount} warning(s)`
                );
            }

            return result.valid;
        } catch (err) {
            this.outputChannel.appendLine(`[Validate] Failed: ${err}`);
            vscode.window.showErrorMessage(`Validation failed: ${err}`);
            return false;
        } finally {
            this._validating = false;
            this._onDidFinishValidation.fire();
        }
    }

    /**
     * Get a summary of all diagnostics in the workspace.
     * Returns counts of errors, warnings, info, and hints.
     */
    getWorkspaceSummary(): { errors: number; warnings: number; info: number; hints: number } {
        let errors = 0;
        let warnings = 0;
        let info = 0;
        let hints = 0;

        this.diagnosticCollection.forEach((uri, diags) => {
            for (const diag of diags) {
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
        });

        return { errors, warnings, info, hints };
    }

    /** Clear all diagnostics */
    clearDiagnostics(): void {
        this.diagnosticCollection.clear();
        this._onDidChangeDiagnostics.fire();
    }

    /** Dispose resources */
    dispose(): void {
        this.diagnosticCollection.dispose();
        this._onDidChangeDiagnostics.dispose();
        this._onDidStartValidation.dispose();
        this._onDidFinishValidation.dispose();
    }
}
