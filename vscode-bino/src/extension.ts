import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { WorkspaceIndexer, LSPDocument, DatasetCandidate, LSPGraphNode, GraphDirection } from './indexer';
import { BinoCompletionProvider } from './completion';
import { BinoTreeProvider, BinoTreeItem, InlineChild } from './tree';
import { BinoDefinitionProvider } from './definition';
import { BinoHoverProvider } from './hover';
import { BinoValidator } from './validation';
import { BinoPreviewManager } from './preview';
import { showSetupCheckResults } from './setup';
import { registerRenameProvider } from './rename';
import { registerPrqlFeatures } from './prql';
import { registerPrqlHighlighting } from './prqlHighlight';
import { registerPrqlCompletion } from './prqlCompletion';

let indexer: WorkspaceIndexer | undefined;
let validator: BinoValidator | undefined;
let previewManager: BinoPreviewManager | undefined;
let indexerStatusBarItem: vscode.StatusBarItem | undefined;
let validationStatusBarItem: vscode.StatusBarItem | undefined;

// Schema URI for bino manifests
const BINO_SCHEMA = 'bino-schema';

/**
 * Find the line number where the Nth document starts in a multi-doc YAML file.
 * @param document The VS Code text document
 * @param docIndex 1-based document index
 * @returns 0-based line number
 */
function findDocumentLine(document: vscode.TextDocument, docIndex: number): number {
    const text = document.getText();
    const lines = text.split('\n');

    let currentDocIndex = 0;

    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        const line = lines[lineNum].trim();

        // Check for document start: either start of file with content,
        // or a line starting with '---'
        if (lineNum === 0) {
            // First document starts at line 0 (or after initial ---)
            if (line === '---') {
                // Explicit document start, don't count yet
                continue;
            } else if (line && !line.startsWith('#')) {
                // Content starts immediately
                currentDocIndex = 1;
                if (currentDocIndex === docIndex) {
                    return 0;
                }
            }
        } else if (line === '---') {
            // New document separator
            currentDocIndex++;
            if (currentDocIndex === docIndex) {
                // Return the line after ---
                return lineNum + 1;
            }
        } else if (currentDocIndex === 0 && line && !line.startsWith('#')) {
            // First content after potential leading comments/blank lines
            currentDocIndex = 1;
            if (currentDocIndex === docIndex) {
                return lineNum;
            }
        }
    }

    // Fallback to start of file
    return 0;
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
    console.log('Bino Reports extension activating...');

    // Create shared output channel
    const outputChannel = vscode.window.createOutputChannel('Bino Reports');
    context.subscriptions.push(outputChannel);

    // Initialize workspace indexer
    indexer = new WorkspaceIndexer(context);

    // Initialize validator
    validator = new BinoValidator(outputChannel);
    context.subscriptions.push({ dispose: () => validator?.dispose() });

    // Initialize preview manager
    previewManager = new BinoPreviewManager(outputChannel);
    context.subscriptions.push({ dispose: () => previewManager?.dispose() });

    // Register schema provider with RedHat YAML extension
    await registerSchemaProvider(context);

    // Register completion provider for YAML files
    const completionProvider = new BinoCompletionProvider(indexer);

    const yamlSelector: vscode.DocumentSelector = [
        { language: 'yaml', scheme: 'file' },
        { language: 'yaml', scheme: 'untitled' }
    ];

    context.subscriptions.push(
        vscode.languages.registerCompletionItemProvider(
            yamlSelector,
            completionProvider,
            ':', ' ', '-', '$'
        )
    );

    // Register definition provider for go-to-definition
    const definitionProvider = new BinoDefinitionProvider(indexer);
    context.subscriptions.push(
        vscode.languages.registerDefinitionProvider(
            yamlSelector,
            definitionProvider
        )
    );

    // Register hover provider for dataset column info
    const hoverProvider = new BinoHoverProvider(indexer);
    context.subscriptions.push(
        vscode.languages.registerHoverProvider(
            yamlSelector,
            hoverProvider
        )
    );

    // Register rename provider for document identifiers
    registerRenameProvider(context, indexer);

    // Register PRQL features (editor, SQL preview integration)
    registerPrqlFeatures(context);

    // Register PRQL inline highlighting in YAML
    registerPrqlHighlighting(context);

    // Register PRQL completions/snippets in YAML
    registerPrqlCompletion(context);

    // Register tree view for Bino Explorer
    const treeProvider = new BinoTreeProvider(indexer, validator);
    const treeView = vscode.window.createTreeView('binoExplorer', {
        treeDataProvider: treeProvider,
        showCollapseAll: true
    });
    context.subscriptions.push(treeView);

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.refreshIndex', async () => {
            await indexer?.refreshIndex();
            vscode.window.showInformationMessage('Bino index refreshed');
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openDocument', async (doc: LSPDocument) => {
            if (!doc) {
                return;
            }
            try {
                const uri = vscode.Uri.file(doc.file);
                const document = await vscode.workspace.openTextDocument(uri);
                const editor = await vscode.window.showTextDocument(document);

                // Check if there are diagnostics for this file - if so, jump to first one
                if (validator) {
                    const diagnostics = validator.getDiagnosticsForUri(uri);
                    if (diagnostics.length > 0) {
                        // Sort by severity (Error=0 first), then by position
                        const sorted = [...diagnostics].sort((a, b) => {
                            if (a.severity !== b.severity) {
                                return a.severity - b.severity;
                            }
                            // Same severity: compare by line, then column
                            if (a.range.start.line !== b.range.start.line) {
                                return a.range.start.line - b.range.start.line;
                            }
                            return a.range.start.character - b.range.start.character;
                        });

                        const firstDiag = sorted[0];
                        const position = firstDiag.range.start;
                        const range = new vscode.Range(position, position);
                        editor.selection = new vscode.Selection(position, position);
                        editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
                        return;
                    }
                }

                // No diagnostics - fall back to default behavior: go to document start
                const lineNumber = findDocumentLine(document, doc.position);
                const position = new vscode.Position(lineNumber, 0);
                const range = new vscode.Range(position, position);
                editor.selection = new vscode.Selection(position, position);
                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
            } catch (err) {
                vscode.window.showErrorMessage(`Failed to open document: ${err}`);
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openInlineChild', async (child: InlineChild) => {
            if (!child) {
                return;
            }
            try {
                const uri = vscode.Uri.file(child.file);
                const document = await vscode.workspace.openTextDocument(uri);
                const editor = await vscode.window.showTextDocument(document);

                // Navigate to the child's line (already 0-based)
                const position = new vscode.Position(child.line, 0);
                const range = new vscode.Range(position, position);
                editor.selection = new vscode.Selection(position, position);
                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
            } catch (err) {
                vscode.window.showErrorMessage(`Failed to open component: ${err}`);
            }
        })
    );

    // Open first problem command (for context menu)
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openFirstProblem', async (item: { document?: LSPDocument }) => {
            const doc = item?.document;
            if (!doc || !validator) {
                return;
            }
            try {
                const uri = vscode.Uri.file(doc.file);
                const diagnostics = validator.getDiagnosticsForUri(uri);

                if (diagnostics.length === 0) {
                    vscode.window.showInformationMessage('No problems found for this document');
                    return;
                }

                const document = await vscode.workspace.openTextDocument(uri);
                const editor = await vscode.window.showTextDocument(document);

                // Sort by severity (Error=0 first), then by position
                const sorted = [...diagnostics].sort((a, b) => {
                    if (a.severity !== b.severity) {
                        return a.severity - b.severity;
                    }
                    if (a.range.start.line !== b.range.start.line) {
                        return a.range.start.line - b.range.start.line;
                    }
                    return a.range.start.character - b.range.start.character;
                });

                const firstDiag = sorted[0];
                const position = firstDiag.range.start;
                const range = new vscode.Range(position, position);
                editor.selection = new vscode.Selection(position, position);
                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
            } catch (err) {
                vscode.window.showErrorMessage(`Failed to open document: ${err}`);
            }
        })
    );

    // Validation commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.validateWorkspace', async () => {
            await validator?.validateWorkspace();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.clearDiagnostics', () => {
            validator?.clearDiagnostics();
            vscode.window.showInformationMessage('Bino diagnostics cleared');
        })
    );

    // Preview commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.startPreview', async () => {
            await previewManager?.startPreview();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.stopPreview', () => {
            previewManager?.stopPreview();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.restartPreview', async () => {
            await previewManager?.restartPreview();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openPreviewInBrowser', () => {
            previewManager?.openInBrowser();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openPreviewInWebview', async () => {
            await previewManager?.openPreviewInWebview();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.previewMenu', async () => {
            await previewManager?.showPreviewMenu();
        })
    );

    // Build command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.build', async () => {
            await previewManager?.runBuild();
        })
    );

    // Setup check command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.checkSetup', async () => {
            await showSetupCheckResults();
        })
    );

    // Show columns for current dataset/datasource command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showColumnsForCurrentDataset', async () => {
            await showColumnsForCurrentDataset(indexer);
        })
    );

    // Graph navigation commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showGraphForItem', async (item: BinoTreeItem) => {
            await showGraphForTreeItem(indexer, item, 'both');
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showDependenciesForItem', async (item: BinoTreeItem) => {
            await showGraphForTreeItem(indexer, item, 'out');
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showDependentsForItem', async (item: BinoTreeItem) => {
            await showGraphForTreeItem(indexer, item, 'in');
        })
    );

    // Validate on save (if enabled)
    context.subscriptions.push(
        vscode.workspace.onDidSaveTextDocument(async (document) => {
            const config = vscode.workspace.getConfiguration('bino');
            const validateOnSave = config.get<boolean>('validateOnSave');

            if (validateOnSave &&
                (document.languageId === 'yaml') &&
                document.getText().includes('apiVersion: bino.bi')) {
                await validator?.validateWorkspace();
            }
        })
    );

    // Watch for file changes to invalidate cache
    const watcher = vscode.workspace.createFileSystemWatcher('**/*.{yaml,yml}');

    watcher.onDidChange(uri => {
        indexer?.invalidateFile(uri.fsPath);
    });

    watcher.onDidCreate(() => {
        indexer?.invalidateIndex();
    });

    watcher.onDidDelete(() => {
        indexer?.invalidateIndex();
    });

    context.subscriptions.push(watcher);

    // Initial index on activation
    await indexer.refreshIndex();

    // --- Indexer status bar item ---
    indexerStatusBarItem = vscode.window.createStatusBarItem(
        vscode.StatusBarAlignment.Left,
        99
    );
    indexerStatusBarItem.command = 'bino.refreshIndex';
    indexerStatusBarItem.tooltip = 'Bino Indexer - click to refresh';
    updateIndexerStatusBar();
    indexerStatusBarItem.show();
    context.subscriptions.push(indexerStatusBarItem);

    // Subscribe to indexer events
    context.subscriptions.push(
        indexer.onDidStartIndex(() => updateIndexerStatusBar()),
        indexer.onDidFinishIndex(() => updateIndexerStatusBar())
    );

    // --- Validation status bar item ---
    validationStatusBarItem = vscode.window.createStatusBarItem(
        vscode.StatusBarAlignment.Left,
        98
    );
    validationStatusBarItem.command = 'bino.validateWorkspace';
    validationStatusBarItem.tooltip = 'Bino Validation - click to validate';
    updateValidationStatusBar();
    validationStatusBarItem.show();
    context.subscriptions.push(validationStatusBarItem);

    // Subscribe to validator events
    context.subscriptions.push(
        validator.onDidStartValidation(() => updateValidationStatusBar()),
        validator.onDidFinishValidation(() => updateValidationStatusBar()),
        validator.onDidChangeDiagnostics(() => updateValidationStatusBar())
    );

    console.log('Bino Reports extension activated');
}

/** Update the indexer status bar item based on current state */
function updateIndexerStatusBar(): void {
    if (!indexerStatusBarItem || !indexer) {
        return;
    }
    if (indexer.isIndexing) {
        indexerStatusBarItem.text = '$(sync~spin) Bino: Indexing';
        indexerStatusBarItem.backgroundColor = undefined;
    } else {
        indexerStatusBarItem.text = '$(check) Bino: Indexed';
        indexerStatusBarItem.backgroundColor = undefined;
    }
}

/** Update the validation status bar item based on current state */
function updateValidationStatusBar(): void {
    if (!validationStatusBarItem || !validator) {
        return;
    }
    if (validator.isValidating) {
        validationStatusBarItem.text = '$(sync~spin) Bino: Validating';
        validationStatusBarItem.backgroundColor = undefined;
        return;
    }
    const summary = validator.getWorkspaceSummary();
    if (summary.errors > 0) {
        validationStatusBarItem.text = `$(error) Bino: ${summary.errors} error${summary.errors !== 1 ? 's' : ''}`;
        validationStatusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.errorBackground');
    } else if (summary.warnings > 0) {
        validationStatusBarItem.text = `$(warning) Bino: ${summary.warnings} warning${summary.warnings !== 1 ? 's' : ''}`;
        validationStatusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
    } else {
        validationStatusBarItem.text = '$(pass) Bino: 0 errors';
        validationStatusBarItem.backgroundColor = undefined;
    }
}

/**
 * Register a custom schema provider with the RedHat YAML extension.
 * This allows us to dynamically associate the bino schema only with files
 * that contain `apiVersion: bino.bi/v1alpha1`.
 */
async function registerSchemaProvider(context: vscode.ExtensionContext): Promise<void> {
    const yamlExtension = vscode.extensions.getExtension('redhat.vscode-yaml');

    if (!yamlExtension) {
        console.warn('RedHat YAML extension not found');
        return;
    }

    // Wait for the extension to activate
    const yamlApi = await yamlExtension.activate();

    if (!yamlApi || !yamlApi.registerContributor) {
        console.warn('RedHat YAML extension API not available');
        return;
    }

    // Get the schema file path and load content
    const schemaFilePath = path.join(context.extensionPath, 'schema', 'document.schema.json');
    let schemaContent: string;

    try {
        schemaContent = fs.readFileSync(schemaFilePath, 'utf8');
    } catch (err) {
        console.error('Failed to load bino schema:', err);
        return;
    }

    // Register our schema contributor
    // The label parameter enables content-based matching: only files containing
    // 'apiVersion: bino.bi' will have this schema applied
    yamlApi.registerContributor(
        BINO_SCHEMA,
        (resource: string) => {
            // Return schema URI for any YAML file - the label filter handles the rest
            if (resource.endsWith('.yaml') || resource.endsWith('.yml')) {
                return `${BINO_SCHEMA}://schema/bino`;
            }
            return undefined;
        },
        (uri: string) => {
            // Return the actual schema JSON content
            return schemaContent;
        },
        // Label: only apply to files containing this pattern
        'apiVersion: bino.bi'
    );

    console.log('Registered bino schema contributor with YAML extension');
}

/**
 * Show dependency graph for a tree item.
 * @param indexer The workspace indexer
 * @param item The tree item (from Bino Explorer)
 * @param direction Traversal direction
 */
async function showGraphForTreeItem(
    indexer: WorkspaceIndexer | undefined,
    item: BinoTreeItem,
    direction: GraphDirection
): Promise<void> {
    if (!indexer) {
        vscode.window.showWarningMessage('Bino indexer not initialized');
        return;
    }

    if (!item || !item.document) {
        vscode.window.showWarningMessage('No document selected');
        return;
    }

    const doc = item.document;

    // Map document kind to graph node kind
    // Most kinds map directly; inline components would need different handling
    const graphKind = doc.kind;
    const graphName = doc.name;

    // Determine title based on direction
    let title: string;
    switch (direction) {
        case 'in':
            title = `Dependents of ${graphName}`;
            break;
        case 'out':
            title = `Dependencies of ${graphName}`;
            break;
        case 'both':
            title = `Graph for ${graphName}`;
            break;
    }

    // Fetch graph dependencies
    const result = await vscode.window.withProgress(
        {
            location: vscode.ProgressLocation.Notification,
            title: `Fetching ${title}...`,
            cancellable: false
        },
        async () => {
            return await indexer.getGraphDeps(graphKind, graphName, direction);
        }
    );

    if (!result) {
        vscode.window.showInformationMessage(`No graph data available for ${graphKind}:${graphName}`);
        return;
    }

    // Filter out the root node from the results
    const otherNodes = result.nodes.filter(n => n.id !== result.rootId);

    if (otherNodes.length === 0) {
        const msg = direction === 'in'
            ? `No dependents found for ${graphName}`
            : direction === 'out'
                ? `No dependencies found for ${graphName}`
                : `No dependencies or dependents found for ${graphName}`;
        vscode.window.showInformationMessage(msg);
        return;
    }

    // Build a map of node relationships for labeling
    const nodeRelations = new Map<string, 'in' | 'out' | 'both'>();
    for (const edge of result.edges) {
        const otherId = edge.fromId === result.rootId ? edge.toId : edge.fromId;
        const existing = nodeRelations.get(otherId);
        if (!existing) {
            nodeRelations.set(otherId, edge.direction);
        } else if (existing !== edge.direction) {
            nodeRelations.set(otherId, 'both');
        }
    }

    // Build QuickPick items
    interface GraphQuickPickItem extends vscode.QuickPickItem {
        node: LSPGraphNode;
    }

    const items: GraphQuickPickItem[] = otherNodes.map(node => {
        const relation = nodeRelations.get(node.id) || 'out';
        let prefix: string;
        switch (relation) {
            case 'in':
                prefix = '↑ dependent';
                break;
            case 'out':
                prefix = '↓ dependency';
                break;
            case 'both':
                prefix = '↕ both';
                break;
        }

        // Build relative path for description
        let description = node.kind;
        if (node.file) {
            const workspaceFolders = vscode.workspace.workspaceFolders;
            if (workspaceFolders && workspaceFolders.length > 0) {
                const root = workspaceFolders[0].uri.fsPath;
                if (node.file.startsWith(root)) {
                    description = node.file.substring(root.length + 1);
                } else {
                    description = path.basename(node.file);
                }
            } else {
                description = path.basename(node.file);
            }
        }

        return {
            label: `$(${getIconForKind(node.kind)}) ${node.name}`,
            description,
            detail: `[${prefix}] ${node.kind}`,
            node
        };
    });

    // Sort items: dependencies first, then dependents, then by name
    items.sort((a, b) => {
        const relA = nodeRelations.get(a.node.id) || 'out';
        const relB = nodeRelations.get(b.node.id) || 'out';
        if (relA !== relB) {
            // out < both < in
            const order = { out: 0, both: 1, in: 2 };
            return order[relA] - order[relB];
        }
        return a.node.name.localeCompare(b.node.name);
    });

    // Show QuickPick
    const selected = await vscode.window.showQuickPick(items, {
        placeHolder: `${otherNodes.length} node(s) - select to open`,
        title,
        matchOnDescription: true,
        matchOnDetail: true
    });

    if (!selected) {
        return;
    }

    // Open the selected node's file
    if (selected.node.file) {
        try {
            const uri = vscode.Uri.file(selected.node.file);
            const document = await vscode.workspace.openTextDocument(uri);
            const editor = await vscode.window.showTextDocument(document);

            // Try to find the document position from the indexer
            // The node name might be the document name, or for inline components it might be "parent#index"
            const docs = indexer.getDocuments();
            let targetDoc = docs.find(d => d.name === selected.node.name && d.file === selected.node.file);

            // For inline components (e.g., "myFirstPage#1"), try to find the parent layout
            if (!targetDoc && selected.node.name.includes('#')) {
                const parentName = selected.node.name.split('#')[0];
                targetDoc = docs.find(d => d.name === parentName && d.file === selected.node.file);
            }

            if (targetDoc) {
                // Jump to the document position
                const lineNumber = findDocumentLine(document, targetDoc.position);
                const position = new vscode.Position(lineNumber, 0);
                const range = new vscode.Range(position, position);
                editor.selection = new vscode.Selection(position, position);
                editor.revealRange(range, vscode.TextEditorRevealType.InCenter);
            }
        } catch (err) {
            vscode.window.showErrorMessage(`Failed to open file: ${err}`);
        }
    } else {
        vscode.window.showWarningMessage(`No file path available for ${selected.node.name}`);
    }
}

/**
 * Get VS Code icon ID for a document kind.
 */
function getIconForKind(kind: string): string {
    switch (kind) {
        case 'DataSource':
            return 'database';
        case 'DataSet':
            return 'table';
        case 'ReportArtefact':
            return 'file-pdf';
        case 'LayoutPage':
            return 'layout';
        case 'LayoutCard':
            return 'credit-card';
        case 'Component':
            return 'symbol-method';
        case 'Table':
            return 'list-flat';
        case 'Text':
            return 'symbol-text';
        case 'ChartStructure':
        case 'ChartTime':
            return 'graph';
        default:
            return 'file-code';
    }
}

/**
 * Show columns for the current dataset/datasource at the cursor position.
 * If multiple candidates are found, shows a disambiguation QuickPick first.
 */
async function showColumnsForCurrentDataset(indexer: WorkspaceIndexer | undefined): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showInformationMessage('No active editor');
        return;
    }

    if (!indexer) {
        vscode.window.showWarningMessage('Bino indexer not initialized');
        return;
    }

    const document = editor.document;
    const position = editor.selection.active;

    // Check if this is a Bino document
    if (!await indexer.isBinoDocument(document)) {
        vscode.window.showInformationMessage('Not a Bino document');
        return;
    }

    // Find dataset/datasource candidates at cursor
    const candidates = indexer.inferDatasetCandidatesAtPosition(document, position);

    if (candidates.length === 0) {
        vscode.window.showInformationMessage('No datasets or datasources found in workspace');
        return;
    }

    let selectedCandidate: DatasetCandidate | undefined;

    if (candidates.length === 1) {
        // Single candidate - use it directly
        selectedCandidate = candidates[0];
    } else {
        // Multiple candidates - show disambiguation QuickPick
        const items: vscode.QuickPickItem[] = candidates.map(c => ({
            label: c.displayName,
            description: c.kind,
            detail: c.kind === 'DataSource' ? 'Raw data source' : 'Transformed dataset'
        }));

        const picked = await vscode.window.showQuickPick(items, {
            placeHolder: 'Select a dataset or datasource to view columns',
            title: 'Bino: Select Dataset/DataSource'
        });

        if (!picked) {
            return; // User cancelled
        }

        selectedCandidate = candidates.find(c => c.displayName === picked.label);
    }

    if (!selectedCandidate) {
        return;
    }

    // Fetch columns for the selected dataset/datasource
    const columns = await vscode.window.withProgress(
        {
            location: vscode.ProgressLocation.Notification,
            title: `Fetching columns for ${selectedCandidate.displayName}...`,
            cancellable: false
        },
        async () => {
            return await indexer.getColumns(selectedCandidate!.name);
        }
    );

    if (columns.length === 0) {
        vscode.window.showInformationMessage(
            `No columns found for ${selectedCandidate.displayName}`
        );
        return;
    }

    // Show columns in a QuickPick (read-only display)
    const columnItems: vscode.QuickPickItem[] = columns.map(col => ({
        label: col,
        description: '',
        detail: `Column from ${selectedCandidate!.displayName}`
    }));

    const selectedColumn = await vscode.window.showQuickPick(columnItems, {
        placeHolder: `${columns.length} column(s) in ${selectedCandidate.displayName}`,
        title: `Columns: ${selectedCandidate.displayName}`,
        canPickMany: false
    });

    // If user selects a column, copy it to clipboard for convenience
    if (selectedColumn) {
        await vscode.env.clipboard.writeText(selectedColumn.label);
        vscode.window.showInformationMessage(`Copied "${selectedColumn.label}" to clipboard`);
    }
}

export function deactivate(): void {
    previewManager?.dispose();
    validator?.dispose();
    indexerStatusBarItem?.dispose();
    validationStatusBarItem?.dispose();
    indexer = undefined;
    validator = undefined;
    previewManager = undefined;
    indexerStatusBarItem = undefined;
    validationStatusBarItem = undefined;
}
