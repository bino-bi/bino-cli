import * as vscode from 'vscode';
import * as path from 'path';
import { WorkspaceIndexer, LSPDocument } from './indexer';

/** PRQL extension ID on the VS Code Marketplace */
const PRQL_EXTENSION_ID = 'PRQL-lang.prql-vscode';

/** Virtual document scheme for extracted PRQL snippets */
const PRQL_SCHEME = 'bino-prql';

/**
 * Information about a PRQL block found in a DataSet manifest.
 */
export interface PrqlBlockInfo {
    /** Name of the dataset (from metadata.name) */
    datasetName: string;
    /** The PRQL query text */
    prqlText: string;
    /** Start line in the YAML file (0-based) */
    startLine: number;
    /** Start character in the YAML file */
    startChar: number;
    /** End line in the YAML file (0-based) */
    endLine: number;
    /** Source file path */
    filePath: string;
}

/**
 * Content provider for virtual PRQL documents extracted from bino YAML.
 */
class PrqlContentProvider implements vscode.TextDocumentContentProvider {
    private contents: Map<string, string> = new Map();

    onDidChangeEmitter = new vscode.EventEmitter<vscode.Uri>();
    onDidChange = this.onDidChangeEmitter.event;

    setContent(uri: vscode.Uri, content: string): void {
        this.contents.set(uri.toString(), content);
        this.onDidChangeEmitter.fire(uri);
    }

    provideTextDocumentContent(uri: vscode.Uri): string {
        return this.contents.get(uri.toString()) ?? '';
    }
}

const prqlContentProvider = new PrqlContentProvider();

/**
 * Check if the PRQL extension is installed.
 */
export function isPrqlExtensionInstalled(): boolean {
    return vscode.extensions.getExtension(PRQL_EXTENSION_ID) !== undefined;
}

/**
 * Prompt the user to install the PRQL extension if not present.
 */
export async function promptInstallPrqlExtension(): Promise<boolean> {
    if (isPrqlExtensionInstalled()) {
        return true;
    }

    const install = 'Install PRQL Extension';
    const choice = await vscode.window.showInformationMessage(
        'The PRQL VS Code extension is recommended for editing PRQL queries. ' +
        'It provides syntax highlighting, diagnostics, and SQL preview.',
        install,
        'Not Now'
    );

    if (choice === install) {
        await vscode.commands.executeCommand(
            'workbench.extensions.installExtension',
            PRQL_EXTENSION_ID
        );
        return true;
    }
    return false;
}

/**
 * Find PRQL block info at the current cursor position in a YAML document.
 * Returns undefined if the cursor is not in a DataSet spec.prql block.
 */
export function findPrqlBlockAtCursor(
    document: vscode.TextDocument,
    position: vscode.Position
): PrqlBlockInfo | undefined {
    const text = document.getText();
    const lines = text.split('\n');

    // We need to find which YAML document the cursor is in,
    // then check if it's a DataSet with a spec.prql field.

    let currentDocStart = 0;
    let currentDocKind = '';
    let currentDocName = '';
    let inPrqlBlock = false;
    let prqlStartLine = -1;
    let prqlStartChar = 0;
    let prqlIndent = 0;
    let prqlLines: string[] = [];

    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        const line = lines[lineNum];
        const trimmed = line.trim();

        // Document separator
        if (trimmed === '---') {
            // If we were in a PRQL block and cursor was in it, return what we have
            if (inPrqlBlock && position.line >= prqlStartLine && position.line < lineNum) {
                return {
                    datasetName: currentDocName,
                    prqlText: prqlLines.join('\n'),
                    startLine: prqlStartLine,
                    startChar: prqlStartChar,
                    endLine: lineNum - 1,
                    filePath: document.uri.fsPath
                };
            }

            // Reset for new document
            currentDocStart = lineNum + 1;
            currentDocKind = '';
            currentDocName = '';
            inPrqlBlock = false;
            prqlLines = [];
            continue;
        }

        // Track kind
        const kindMatch = trimmed.match(/^kind:\s*(\w+)/);
        if (kindMatch) {
            currentDocKind = kindMatch[1];
        }

        // Track name
        const nameMatch = trimmed.match(/^name:\s*(\w+)/);
        if (nameMatch && currentDocName === '') {
            currentDocName = nameMatch[1];
        }

        // Look for prql: field start
        const prqlMatch = line.match(/^(\s*)prql:\s*\|?\s*$/);
        if (prqlMatch && currentDocKind === 'DataSet') {
            inPrqlBlock = true;
            prqlStartLine = lineNum + 1; // PRQL content starts on next line
            prqlIndent = prqlMatch[1].length + 2; // Expect content indented by 2 more spaces
            prqlLines = [];
            continue;
        }

        // Also check for inline prql with content on same line
        const prqlInlineMatch = line.match(/^(\s*)prql:\s*(.+)$/);
        if (prqlInlineMatch && currentDocKind === 'DataSet' && prqlInlineMatch[2].trim()) {
            // Single-line PRQL
            if (position.line === lineNum) {
                return {
                    datasetName: currentDocName,
                    prqlText: prqlInlineMatch[2].trim(),
                    startLine: lineNum,
                    startChar: prqlInlineMatch[1].length + 'prql: '.length,
                    endLine: lineNum,
                    filePath: document.uri.fsPath
                };
            }
        }

        // Collect PRQL block lines
        if (inPrqlBlock) {
            // Check if this line is still part of the block (proper indentation)
            const lineIndent = line.length - line.trimStart().length;

            if (trimmed === '' || lineIndent >= prqlIndent) {
                // Still in the block
                // Remove the common indent
                const content = lineIndent >= prqlIndent
                    ? line.substring(prqlIndent)
                    : '';
                prqlLines.push(content);
            } else {
                // Block ended
                if (position.line >= prqlStartLine && position.line < lineNum) {
                    return {
                        datasetName: currentDocName,
                        prqlText: prqlLines.join('\n').trimEnd(),
                        startLine: prqlStartLine,
                        startChar: prqlIndent,
                        endLine: lineNum - 1,
                        filePath: document.uri.fsPath
                    };
                }
                inPrqlBlock = false;
                prqlLines = [];
            }
        }
    }

    // Check if cursor is in the last PRQL block
    if (inPrqlBlock && position.line >= prqlStartLine) {
        return {
            datasetName: currentDocName,
            prqlText: prqlLines.join('\n').trimEnd(),
            startLine: prqlStartLine,
            startChar: prqlIndent,
            endLine: lines.length - 1,
            filePath: document.uri.fsPath
        };
    }

    return undefined;
}

/**
 * Find any PRQL blocks in a DataSet document by dataset name.
 */
export function findPrqlBlockByDatasetName(
    document: vscode.TextDocument,
    datasetName: string
): PrqlBlockInfo | undefined {
    const text = document.getText();
    const lines = text.split('\n');

    let currentDocName = '';
    let currentDocKind = '';
    let inPrqlBlock = false;
    let prqlStartLine = -1;
    let prqlIndent = 0;
    let prqlLines: string[] = [];

    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        const line = lines[lineNum];
        const trimmed = line.trim();

        // Document separator
        if (trimmed === '---') {
            // Check if we found our dataset
            if (currentDocName === datasetName && prqlLines.length > 0) {
                return {
                    datasetName,
                    prqlText: prqlLines.join('\n').trimEnd(),
                    startLine: prqlStartLine,
                    startChar: prqlIndent,
                    endLine: lineNum - 1,
                    filePath: document.uri.fsPath
                };
            }
            currentDocName = '';
            currentDocKind = '';
            inPrqlBlock = false;
            prqlLines = [];
            continue;
        }

        // Track kind
        const kindMatch = trimmed.match(/^kind:\s*(\w+)/);
        if (kindMatch) {
            currentDocKind = kindMatch[1];
        }

        // Track name
        const nameMatch = trimmed.match(/^name:\s*(\w+)/);
        if (nameMatch && currentDocName === '') {
            currentDocName = nameMatch[1];
        }

        // Look for prql: field
        const prqlMatch = line.match(/^(\s*)prql:\s*\|?\s*$/);
        if (prqlMatch && currentDocKind === 'DataSet') {
            inPrqlBlock = true;
            prqlStartLine = lineNum + 1;
            prqlIndent = prqlMatch[1].length + 2;
            prqlLines = [];
            continue;
        }

        // Collect PRQL lines
        if (inPrqlBlock) {
            const lineIndent = line.length - line.trimStart().length;
            if (trimmed === '' || lineIndent >= prqlIndent) {
                const content = lineIndent >= prqlIndent ? line.substring(prqlIndent) : '';
                prqlLines.push(content);
            } else {
                inPrqlBlock = false;
            }
        }
    }

    // Check last document
    if (currentDocName === datasetName && prqlLines.length > 0) {
        return {
            datasetName,
            prqlText: prqlLines.join('\n').trimEnd(),
            startLine: prqlStartLine,
            startChar: prqlIndent,
            endLine: lines.length - 1,
            filePath: document.uri.fsPath
        };
    }

    return undefined;
}

/**
 * Open a PRQL block in a dedicated PRQL editor.
 * Creates a virtual document with the PRQL content and opens it with PRQL language mode.
 */
export async function openPrqlEditor(prqlInfo: PrqlBlockInfo): Promise<vscode.TextDocument | undefined> {
    // Create a virtual URI for this PRQL snippet
    const baseName = path.basename(prqlInfo.filePath, path.extname(prqlInfo.filePath));
    const uri = vscode.Uri.parse(`${PRQL_SCHEME}:/${baseName}-${prqlInfo.datasetName}.prql`);

    // Add header comment with source info
    const headerComment = `# PRQL for DataSet: ${prqlInfo.datasetName}\n` +
        `# Source: ${path.basename(prqlInfo.filePath)}:${prqlInfo.startLine + 1}\n` +
        `# Target: DuckDB (via bino build)\n\n`;

    const content = headerComment + prqlInfo.prqlText;

    // Set content in our provider
    prqlContentProvider.setContent(uri, content);

    // Open the document
    const doc = await vscode.workspace.openTextDocument(uri);
    await vscode.window.showTextDocument(doc, { preview: false });

    return doc;
}

/**
 * Open PRQL SQL Preview for a PRQL block.
 * Requires the PRQL extension to be installed.
 */
export async function openPrqlSqlPreview(prqlInfo: PrqlBlockInfo): Promise<void> {
    // First, ensure PRQL extension is available
    if (!isPrqlExtensionInstalled()) {
        const installed = await promptInstallPrqlExtension();
        if (!installed) {
            vscode.window.showWarningMessage(
                'PRQL SQL Preview requires the PRQL extension. ' +
                'You can still use bino build to execute PRQL queries.'
            );
            return;
        }
        // Extension was just installed, wait a bit for activation
        await new Promise(resolve => setTimeout(resolve, 1000));
    }

    // Open the PRQL editor first
    const doc = await openPrqlEditor(prqlInfo);
    if (!doc) {
        return;
    }

    // Wait for editor to be active
    await new Promise(resolve => setTimeout(resolve, 100));

    // Try to invoke the PRQL extension's SQL Preview command
    try {
        await vscode.commands.executeCommand('prql.openSqlPreview', doc.uri);
    } catch (err) {
        // The command might not exist if extension isn't fully loaded
        vscode.window.showWarningMessage(
            'Could not open PRQL SQL Preview. Make sure the PRQL extension is installed and enabled.'
        );
    }
}

/**
 * Command handler: Open PRQL Editor for current DataSet.
 * Extracts spec.prql from the DataSet at cursor and opens it in a PRQL editor.
 */
export async function openPrqlEditorForCurrentDataset(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.languageId !== 'yaml') {
        vscode.window.showWarningMessage('Please open a bino YAML file and place cursor in a DataSet with spec.prql');
        return;
    }

    const prqlInfo = findPrqlBlockAtCursor(editor.document, editor.selection.active);
    if (!prqlInfo) {
        vscode.window.showWarningMessage(
            'No PRQL block found at cursor. ' +
            'Make sure your cursor is inside a DataSet document with a spec.prql field.'
        );
        return;
    }

    await openPrqlEditor(prqlInfo);
}

/**
 * Command handler: Open PRQL SQL Preview for current DataSet.
 */
export async function openPrqlSqlPreviewForCurrentDataset(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.languageId !== 'yaml') {
        vscode.window.showWarningMessage('Please open a bino YAML file and place cursor in a DataSet with spec.prql');
        return;
    }

    const prqlInfo = findPrqlBlockAtCursor(editor.document, editor.selection.active);
    if (!prqlInfo) {
        vscode.window.showWarningMessage(
            'No PRQL block found at cursor. ' +
            'Make sure your cursor is inside a DataSet document with a spec.prql field.'
        );
        return;
    }

    await openPrqlSqlPreview(prqlInfo);
}

/**
 * Check if cursor is in a PRQL block and update context for menu visibility.
 */
export function updatePrqlContext(editor: vscode.TextEditor | undefined): void {
    if (!editor || editor.document.languageId !== 'yaml') {
        vscode.commands.executeCommand('setContext', 'bino.inPrqlBlock', false);
        return;
    }

    const prqlInfo = findPrqlBlockAtCursor(editor.document, editor.selection.active);
    vscode.commands.executeCommand('setContext', 'bino.inPrqlBlock', prqlInfo !== undefined);
}

/**
 * Register all PRQL-related functionality.
 */
export function registerPrqlFeatures(context: vscode.ExtensionContext): void {
    // Register virtual document provider
    context.subscriptions.push(
        vscode.workspace.registerTextDocumentContentProvider(PRQL_SCHEME, prqlContentProvider)
    );

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openPrqlEditor', openPrqlEditorForCurrentDataset)
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openPrqlSqlPreview', openPrqlSqlPreviewForCurrentDataset)
    );

    // Update context when selection changes
    context.subscriptions.push(
        vscode.window.onDidChangeTextEditorSelection((e) => {
            updatePrqlContext(e.textEditor);
        })
    );

    // Update context when active editor changes
    context.subscriptions.push(
        vscode.window.onDidChangeActiveTextEditor((editor) => {
            updatePrqlContext(editor);
        })
    );

    // Initial context update
    updatePrqlContext(vscode.window.activeTextEditor);

    // Show a one-time hint about PRQL support if we detect PRQL usage
    checkAndPromptPrqlExtension(context);
}

/**
 * Check workspace for PRQL usage and suggest installing the extension (once per workspace).
 */
async function checkAndPromptPrqlExtension(context: vscode.ExtensionContext): Promise<void> {
    // Only prompt once per workspace
    const promptedKey = 'bino.prqlExtensionPrompted';
    if (context.workspaceState.get<boolean>(promptedKey)) {
        return;
    }

    // If already installed, no need to prompt
    if (isPrqlExtensionInstalled()) {
        return;
    }

    // Check if any YAML files contain spec.prql
    // Do a quick search in workspace
    try {
        const files = await vscode.workspace.findFiles('**/*.{yaml,yml}', '**/node_modules/**', 10);
        for (const file of files) {
            const doc = await vscode.workspace.openTextDocument(file);
            const text = doc.getText();
            if (text.includes('kind: DataSet') && text.includes('prql:')) {
                // Found PRQL usage, prompt
                context.workspaceState.update(promptedKey, true);
                promptInstallPrqlExtension();
                return;
            }
        }
    } catch {
        // Ignore errors in background check
    }
}
