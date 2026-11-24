import * as vscode from 'vscode';
import * as path from 'path';
import { BinoDocumentsResult, blockText, getBinoDocuments } from './binoDocuments';

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

/** PRQL blocks of a buffer's outline, as PrqlBlockInfo. */
export function prqlBlocksOf(outline: BinoDocumentsResult, document: vscode.TextDocument): PrqlBlockInfo[] {
    const text = document.getText();
    const out: PrqlBlockInfo[] = [];
    for (const doc of outline.documents) {
        for (const block of doc.blocks) {
            if (block.language !== 'prql') {
                continue;
            }
            out.push({
                datasetName: doc.name,
                prqlText: blockText(text, block),
                startLine: block.range.start.line,
                startChar: block.range.start.character,
                endLine: block.range.end.line,
                filePath: document.uri.fsPath
            });
        }
    }
    return out;
}

/** The PRQL block whose lines contain the cursor, if any. */
export function prqlBlockAt(
    outline: BinoDocumentsResult,
    document: vscode.TextDocument,
    position: vscode.Position
): PrqlBlockInfo | undefined {
    return prqlBlocksOf(outline, document).find(b => position.line >= b.startLine && position.line <= b.endLine);
}

/**
 * Find the DataSet spec.prql block at the cursor, located by the language
 * server on the live buffer. Undefined when the cursor is in no PRQL block.
 */
export async function findPrqlBlockAtCursor(
    document: vscode.TextDocument,
    position: vscode.Position
): Promise<PrqlBlockInfo | undefined> {
    const outline = await getBinoDocuments(document);
    return outline ? prqlBlockAt(outline, document, position) : undefined;
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

    const prqlInfo = await findPrqlBlockAtCursor(editor.document, editor.selection.active);
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

    const prqlInfo = await findPrqlBlockAtCursor(editor.document, editor.selection.active);
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
export async function updatePrqlContext(editor: vscode.TextEditor | undefined): Promise<void> {
    if (!editor || editor.document.languageId !== 'yaml') {
        await vscode.commands.executeCommand('setContext', 'bino.inPrqlBlock', false);
        return;
    }

    const prqlInfo = await findPrqlBlockAtCursor(editor.document, editor.selection.active);
    // Skip a stale answer when the user moved on while the request ran.
    if (vscode.window.activeTextEditor !== editor) {
        return;
    }
    await vscode.commands.executeCommand('setContext', 'bino.inPrqlBlock', prqlInfo !== undefined);
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
            void updatePrqlContext(e.textEditor);
        })
    );

    // Update context when active editor changes
    context.subscriptions.push(
        vscode.window.onDidChangeActiveTextEditor((editor) => {
            void updatePrqlContext(editor);
        })
    );

    // Initial context update
    void updatePrqlContext(vscode.window.activeTextEditor);

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
