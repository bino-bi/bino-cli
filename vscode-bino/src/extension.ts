import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as cp from 'child_process';
import { WorkspaceIndexer, LSPDocument, DatasetCandidate, LSPGraphNode, GraphDirection } from './indexer';
import { BinoTreeProvider, BinoTreeItem, InlineChild, kindIcon } from './tree';
import { BinoValidator } from './validation';
import { BinoPreviewManager } from './preview';
import { showSetupCheckResults } from './setup';
import { startLanguageClient, stopLanguageClient } from './languageClient';
import { registerPrqlFeatures, updatePrqlContext } from './prql';
import { registerPrqlCompletion } from './prqlCompletion';
import { BinoCodeLensProvider } from './codelens';
import { registerBinoDocumentsCache } from './binoDocuments';
import { RowsPreviewManager } from './rowsPreview';
import { DataSourceWizardManager } from './wizard/wizardPanel';
import { AddElementCommand } from './addElement';
import { TreeEditorProvider } from './treeTableEditor';
import { DesignerEditorProvider } from './designer/designerPanel';
import { StyleEditorProvider } from './styles/styleEditor';
import { RuleSetEditorProvider } from './styles/ruleSetEditor';
import { AnnotationsViewProvider, AnnotationEntry, annotationEntries, loadManifests } from './annotations/annotationsView';
import { AnnotationFormPanel } from './annotations/annotationForm';
import { documentLines } from './annotations/annotationModel';
import { PreviewTreeProvider } from './previewTree';
import { EnvironmentTreeProvider } from './environmentTree';
import { DependenciesTreeProvider } from './dependenciesTree';
import { registerRegistryCommands } from './registryCommands';
import { DaemonClient } from './daemonClient';
import { registerMcpProvider } from './mcpProvider';
import { loadBackendSchema, SchemaSource } from './schemaResolver';
import { daemonVersionMismatch, getBinoPath, installedBinoVersion, runBino } from './bino';
import { BinoStatusBar } from './statusBar';
import { BINO_INSTALLED_CONTEXT, detectBino, downloadBinoCommand, loadManagedBino } from './binoDownload';
import { BinoTaskProvider, BUILD_KINDS } from './tasks';
import { ProjectTracker } from './project';

let indexer: WorkspaceIndexer | undefined;
let validator: BinoValidator | undefined;
let previewManager: BinoPreviewManager | undefined;
let rowsPreviewManager: RowsPreviewManager | undefined;
let wizardManager: DataSourceWizardManager | undefined;
let treeTableEditorManager: TreeEditorProvider | undefined;
let designerPanel: DesignerEditorProvider | undefined;
let styleEditor: StyleEditorProvider | undefined;
let ruleSetEditor: RuleSetEditorProvider | undefined;
let annotationForm: AnnotationFormPanel | undefined;
let daemonClient: DaemonClient | undefined;

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

let binoTerminal: vscode.Terminal | undefined;
let binoTerminalCwd: string | undefined;

/** The configured bino binary, or `bino` from PATH. */
function binoPath(): string {
    return getBinoPath();
}

/** Quote a program path for the user's default terminal shell. */
function quoteProgram(program: string): string {
    if (/^[\w@%+=:,./\\-]+$/.test(program)) {
        return program;
    }
    const shell = path.basename(vscode.env.shell || '').toLowerCase();
    if (shell.startsWith('pwsh') || shell.startsWith('powershell')) {
        return `& '${program.replace(/'/g, "''")}'`;
    }
    if (shell.startsWith('cmd')) {
        return `"${program}"`;
    }
    return `'${program.replace(/'/g, `'\\''`)}'`;
}

/** Run a bino command in the shared "Bino" terminal. */
function runInTerminal(args: string): void {
    const cwd = indexer?.getProjectRootForUri();
    // A terminal keeps its start directory, so a different project root needs a new one.
    if (!binoTerminal || binoTerminal.exitStatus !== undefined || binoTerminalCwd !== cwd) {
        binoTerminal = vscode.window.createTerminal({ name: 'Bino', cwd });
        binoTerminalCwd = cwd;
    }
    binoTerminal.show();
    binoTerminal.sendText(`${quoteProgram(binoPath())} ${args}`);
}

/** Run bino without a shell and return stdout, or undefined on any failure. */
function execBinoOutput(args: string[], cwd: string | undefined): Promise<string | undefined> {
    return new Promise(resolve => {
        cp.execFile(binoPath(), args, { cwd, timeout: 30_000, maxBuffer: 20 * 1024 * 1024, encoding: 'utf8' },
            (err, stdout) => resolve(err ? undefined : stdout));
    });
}

/**
 * The merged schema and kind list: the daemon first, then `bino schema` (which
 * also loads the project's plugins) and the indexer's kind list.
 */
function backendSchemaSource(cwd: string | undefined): SchemaSource {
    return {
        getSchema: async () =>
            (daemonClient?.isConnected ? await daemonClient.getSchema() : undefined) ?? execBinoOutput(['schema'], cwd),
        getKinds: async () => {
            const served = daemonClient?.isConnected ? await daemonClient.getKinds() : undefined;
            if (served && !served.error && Array.isArray(served.kinds)) {
                return served.kinds;
            }
            return indexer?.getKindInfos();
        },
    };
}

/** The `bino add` subcommands, read from cobra's completion so it matches the installed binary. */
async function listAddSubcommands(cwd: string | undefined): Promise<Set<string>> {
    const out = await execBinoOutput(['__complete', 'add', ''], cwd);
    const names = (out ?? '').split('\n')
        .filter(line => line && !line.startsWith(':'))
        .map(line => line.split('\t')[0].trim());
    return new Set(names);
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
    console.log('Bino Reports extension activating...');

    // Create shared output channel
    const outputChannel = vscode.window.createOutputChannel('Bino Reports', { log: true });
    context.subscriptions.push(outputChannel);

    // Initialize workspace indexer
    indexer = new WorkspaceIndexer(context, outputChannel);

    // bino runs the workspace's YAML and SQL, so nothing starts before the workspace is trusted.
    const trusted = vscode.workspace.isTrusted;
    loadManagedBino(context);
    if (trusted) {
        void detectBino(context);
    }

    // Check if this is a bino project and set context for UI visibility
    const hasBinoProject = await indexer.hasProjectInWorkspace();
    await vscode.commands.executeCommand('setContext', 'bino.projectDetected', hasBinoProject && trusted);

    // Onboarding commands must work without a project, so they are registered before the early return.
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.initProject', () => {
            if (requireTrust()) {
                runInTerminal('init');
            }
        }),
        vscode.commands.registerCommand('bino.checkSetup', async () => {
            if (requireTrust()) {
                await showSetupCheckResults();
                await vscode.commands.executeCommand('setContext', BINO_INSTALLED_CONTEXT, (await installedBinoVersion()) !== undefined);
            }
        }),
        vscode.commands.registerCommand('bino.downloadBino', async () => {
            if (requireTrust()) {
                await downloadBinoCommand(context, outputChannel);
            }
        }),
        vscode.commands.registerCommand('bino.openSettings', () => {
            vscode.commands.executeCommand('workbench.action.openSettings', 'bino');
        })
    );

    if (hasBinoProject && !trusted) {
        outputChannel.appendLine('Workspace is not trusted. Bino features start once you trust it.');
        context.subscriptions.push(
            vscode.window.registerTreeDataProvider('binoGettingStarted', {
                getTreeItem: (item: vscode.TreeItem) => item,
                getChildren: () => [],
            })
        );
        // Commands contributed for a project would otherwise fail with "command not found".
        const own = new Set(['bino.initProject', 'bino.checkSetup', 'bino.downloadBino', 'bino.openSettings']);
        const contributed: { command: string }[] = context.extension.packageJSON?.contributes?.commands ?? [];
        const stubs = contributed
            .filter(c => !own.has(c.command))
            .map(c => vscode.commands.registerCommand(c.command, () => requireTrust()));
        void requireTrust();
        context.subscriptions.push(
            ...stubs,
            vscode.workspace.onDidGrantWorkspaceTrust(async () => {
                stubs.forEach(d => d.dispose());
                void detectBino(context);
                await vscode.commands.executeCommand('setContext', 'bino.projectDetected', true);
                await activateProject(context, outputChannel);
            })
        );
        return;
    }

    if (!hasBinoProject) {
        console.log('No bino.toml found in workspace, extension features disabled');
        outputChannel.appendLine('No bino.toml found in workspace. Run "bino init" to create a new project.');
        // The welcome content needs a registered view, else VS Code shows "no data provider".
        context.subscriptions.push(
            vscode.window.registerTreeDataProvider('binoGettingStarted', {
                getTreeItem: (item: vscode.TreeItem) => item,
                getChildren: () => [],
            })
        );
        // Features are wired once at activation, so a new project needs a window reload.
        const tomlWatcher = vscode.workspace.createFileSystemWatcher('**/bino.toml', false, true, true);
        context.subscriptions.push(
            tomlWatcher,
            tomlWatcher.onDidCreate(async () => {
                tomlWatcher.dispose();
                const choice = await vscode.window.showInformationMessage(
                    'Bino project created. Reload the window to enable all Bino features.',
                    'Reload Window'
                );
                if (choice === 'Reload Window') {
                    await vscode.commands.executeCommand('workbench.action.reloadWindow');
                }
            })
        );
        return;
    }

    await activateProject(context, outputChannel);
}

/** Show the trust message when the workspace is untrusted. Returns whether bino may run. */
function requireTrust(): boolean {
    if (vscode.workspace.isTrusted) {
        return true;
    }
    void vscode.window.showWarningMessage(
        'Bino features need a trusted workspace.',
        'Manage Workspace Trust'
    ).then(choice => {
        if (choice === 'Manage Workspace Trust') {
            void vscode.commands.executeCommand('workbench.trust.manage');
        }
    });
    return false;
}

/** Wire up every feature for a trusted workspace with a bino project. */
async function activateProject(context: vscode.ExtensionContext, outputChannel: vscode.LogOutputChannel): Promise<void> {
    if (!indexer) {
        return;
    }
    console.log('Bino project detected, enabling full extension features');

    const project = new ProjectTracker();
    context.subscriptions.push(project);
    await project.init();
    indexer.setCurrentProject(() => project.current);

    // Initialize validator
    validator = new BinoValidator(outputChannel);
    context.subscriptions.push({ dispose: () => validator?.dispose() });

    // Initialize preview manager with indexer for click-to-source navigation
    previewManager = new BinoPreviewManager(outputChannel, indexer);
    context.subscriptions.push({ dispose: () => previewManager?.dispose() });

    // `bino lsp` attaches to the daemon once, at startup. Track which daemon it
    // got, so it restarts when this window moves to another daemon.
    let lspStarted = false;
    let lspDaemonPid: number | undefined;
    // Restarts run one at a time, so a stop never interleaves with a start.
    let lspQueue: Promise<void> = Promise.resolve();

    // Initialize daemon client (if enabled)
    const binoConfig = vscode.workspace.getConfiguration('bino');
    const daemonEnabled = binoConfig.get<boolean>('daemon.enabled', true);
    let daemonReady: Promise<unknown> = Promise.resolve();

    if (daemonEnabled) {
        daemonClient = new DaemonClient(outputChannel);
        context.subscriptions.push({ dispose: () => daemonClient?.dispose() });
        context.subscriptions.push(registerMcpProvider(daemonClient));

        // Pass daemon client to indexer and preview manager
        indexer.setDaemonClient(daemonClient);
        previewManager.setDaemonClient(daemonClient);

        // Listen for SSE push events. The `diagnostics` event is consumed by
        // the language server (its HTTP backend subscribes to the same SSE
        // stream and refreshes project diagnostics), not here.
        context.subscriptions.push(
            daemonClient.on('index-updated', async () => {
                outputChannel.appendLine('[Daemon] Index updated via SSE push');
                await indexer?.refreshIndex();
            }),
            daemonClient.on('preview-status', (data: any) => {
                previewManager?.handlePreviewStatusEvent(data);
            })
        );

        context.subscriptions.push(
            daemonClient.onStatusChange(status => {
                if (status === 'connected') {
                    void checkDaemonVersion();
                    if (lspStarted && daemonClient?.pid !== lspDaemonPid) {
                        void restartLanguageClient();
                    }
                }
            })
        );

        // Connect to daemon in the background, but keep the promise: the LSP
        // start below chains onto it so `bino lsp` finds the daemon's port
        // file and proxies instead of standing up a second DuckDB + watcher
        // (the server decides proxy-vs-standalone once, at startup).
        const projectRoot = indexer.getProjectRootForUri();
        if (projectRoot) {
            daemonReady = Promise.race([
                daemonClient.connect(projectRoot).then(connected => {
                    if (connected) {
                        outputChannel.appendLine('[Daemon] Connected successfully');
                        // Re-index using daemon for faster initial load
                        indexer?.refreshIndex();
                    } else {
                        outputChannel.appendLine('[Daemon] Connection failed, using subprocess fallback');
                    }
                }),
                // A hung daemon spawn must not delay the LSP indefinitely.
                new Promise<void>(resolve => setTimeout(resolve, 15_000)),
            ]).catch(() => undefined);
        }
    }

    context.subscriptions.push(new BinoStatusBar(() => daemonClient, previewManager, indexer, validator, outputChannel, project));

    // The bino Language Server provides completion, hover, navigation, rename,
    // and diagnostics - server-side over a YAML CST. It is gated on a binary
    // capability: when `bino lsp` is missing (an old binary) the user is prompted
    // to update and no language features are registered. The legacy heuristic
    // providers and the redhat schema contributor have been removed; the LSP now
    // serves schema-driven completion and validation. The daemon started above
    // (when enabled) is the single heavy instance the LSP proxies to.
    const yamlSelector: vscode.DocumentSelector = [
        { language: 'yaml', scheme: 'file' },
        { language: 'yaml', scheme: 'untitled' }
    ];

    // Root the LSP at the current project. Start it after the daemon connect
    // settles, off the activation path: a language-server failure must not
    // reject activate() and silently kill every command and tree view -
    // features degrade with a visible warning instead.
    if (project.current) {
        void daemonReady.then(() => {
            lspQueue = lspQueue.then(async () => {
                const lspProjectRoot = indexer?.getWorkspaceProjectRoot();
                // A project switch may have started it already.
                if (!lspProjectRoot || lspStarted) {
                    return;
                }
                lspStarted = true;
                lspDaemonPid = daemonClient?.isConnected ? daemonClient.pid : undefined;
                languageClientStarted(await startLanguageClient(context, outputChannel, lspProjectRoot));
            }).catch(err => {
                outputChannel.appendLine(`[LSP] failed to start: ${err}`);
                vscode.window.showWarningMessage(
                    'bino language server failed to start - completion and diagnostics are unavailable. See the "Bino Reports" output for details.'
                );
            });
        });
    }

    // Positions and (without the daemon) the index come from the server, so
    // every new client is attached again.
    function languageClientStarted(started: boolean): void {
        if (started) {
            indexer?.attachLanguageClient();
            codeLensProvider.refresh();
            treeTableEditorManager?.refreshAll();
            void updatePrqlContext(vscode.window.activeTextEditor);
        }
    }

    function restartLanguageClient(): Promise<void> {
        lspQueue = lspQueue.then(async () => {
            const lspRoot = indexer?.getWorkspaceProjectRoot();
            if (!lspRoot) {
                return;
            }
            lspStarted = true;
            lspDaemonPid = daemonClient?.isConnected ? daemonClient.pid : undefined;
            try {
                await stopLanguageClient();
                languageClientStarted(await startLanguageClient(context, outputChannel, lspRoot));
            } catch (err) {
                outputChannel.appendLine(`[LSP] failed to restart: ${err}`);
            }
        });
        return lspQueue;
    }

    // Point the language server, the daemon, the index and a running preview at the new project.
    let projectQueue: Promise<void> = Promise.resolve();
    const switchProject = (root: string) => {
        projectQueue = projectQueue.then(async () => {
            if (root !== project.current) {
                return; // a later switch replaced this one
            }
            outputChannel.appendLine(`[Project] Current project: ${root}`);
            const preview = previewManager;
            const previewWasOn = preview?.getStatus() === 'running' || preview?.getStatus() === 'starting';
            if (previewWasOn) {
                await preview?.stopPreview();
            }
            const pidBefore = lspDaemonPid;
            const connected = daemonClient ? await daemonClient.switchProject(root) : false;
            // A new daemon restarts the language server through the status listener.
            if (!connected || daemonClient?.pid === pidBefore) {
                await restartLanguageClient();
            }
            await indexer?.refreshIndex();
            if (previewWasOn) {
                await preview?.startPreview();
            }
        }).catch(err => outputChannel.appendLine(`[Project] switch failed: ${err}`));
    };
    context.subscriptions.push(
        project.onDidChange(switchProject),
        vscode.commands.registerCommand('bino.selectProject', () => project.pick())
    );

    // Register CodeLens provider for DataSource/DataSet previews
    const codeLensProvider = new BinoCodeLensProvider(indexer);
    context.subscriptions.push(
        vscode.languages.registerCodeLensProvider(
            yamlSelector,
            codeLensProvider
        )
    );
    context.subscriptions.push({ dispose: () => codeLensProvider.dispose() });
    registerBinoDocumentsCache(context);

    // Initialize rows preview manager
    rowsPreviewManager = new RowsPreviewManager(indexer, outputChannel);
    context.subscriptions.push({ dispose: () => rowsPreviewManager?.dispose() });

    // Initialize the DataSource/DataSet wizard
    wizardManager = new DataSourceWizardManager(indexer, outputChannel);
    context.subscriptions.push({ dispose: () => wizardManager?.dispose() });

    // Initialize tree-table editor manager
    treeTableEditorManager = new TreeEditorProvider(indexer, context.extensionPath);
    context.subscriptions.push(
        { dispose: () => treeTableEditorManager?.dispose() },
        vscode.window.registerCustomEditorProvider(TreeEditorProvider.viewType, treeTableEditorManager, {
            webviewOptions: { retainContextWhenHidden: true },
        })
    );

    // Initialize the component designer (schema-driven form + live canvas)
    designerPanel = new DesignerEditorProvider(indexer, previewManager, context.extensionPath);
    context.subscriptions.push(
        { dispose: () => designerPanel?.dispose() },
        vscode.window.registerCustomEditorProvider(DesignerEditorProvider.viewType, designerPanel, {
            webviewOptions: { retainContextWhenHidden: true },
        })
    );

    // Visual editors for ComponentStyle and RuleSet documents
    styleEditor = new StyleEditorProvider(indexer, previewManager, context.extensionPath);
    ruleSetEditor = new RuleSetEditorProvider(indexer, previewManager);
    context.subscriptions.push(
        { dispose: () => { styleEditor?.dispose(); ruleSetEditor?.dispose(); } },
        vscode.window.registerCustomEditorProvider(StyleEditorProvider.viewType, styleEditor, {
            webviewOptions: { retainContextWhenHidden: true },
        }),
        vscode.window.registerCustomEditorProvider(RuleSetEditorProvider.viewType, ruleSetEditor, {
            webviewOptions: { retainContextWhenHidden: true },
        })
    );
    // The editor title shows their buttons only on a file that holds such a document.
    const updateEditorKindContext = (editor: vscode.TextEditor | undefined) => {
        const text = editor?.document.languageId === 'yaml' ? editor.document.getText() : '';
        void vscode.commands.executeCommand('setContext', 'bino.activeHasComponentStyle', /^kind:\s*["']?ComponentStyle["']?\s*(#.*)?$/m.test(text));
        void vscode.commands.executeCommand('setContext', 'bino.activeHasRuleSet', /^kind:\s*["']?RuleSet["']?\s*(#.*)?$/m.test(text));
    };
    let editorKindTimer: ReturnType<typeof setTimeout> | undefined;
    updateEditorKindContext(vscode.window.activeTextEditor);
    context.subscriptions.push(
        vscode.window.onDidChangeActiveTextEditor(updateEditorKindContext),
        vscode.workspace.onDidChangeTextDocument(e => {
            if (e.document !== vscode.window.activeTextEditor?.document) { return; }
            if (editorKindTimer) { clearTimeout(editorKindTimer); }
            editorKindTimer = setTimeout(() => updateEditorKindContext(vscode.window.activeTextEditor), 500);
        })
    );

    // Register PRQL features (editor, SQL preview integration)
    registerPrqlFeatures(context);

    // Register PRQL completions/snippets in YAML
    registerPrqlCompletion(context);

    // Annotations view and form
    const annotationsIndexer = indexer;
    const annotationsView = new AnnotationsViewProvider(annotationsIndexer);
    annotationForm = new AnnotationFormPanel(indexer, () => annotationsView.refresh());
    const form = annotationForm;
    context.subscriptions.push(
        vscode.window.registerTreeDataProvider('binoAnnotations', annotationsView),
        { dispose: () => annotationForm?.dispose() },
        indexer.onDidUpdateIndex(() => annotationsView.refresh()),
        vscode.languages.onDidChangeDiagnostics(() => annotationsView.refresh()),
        vscode.commands.registerCommand('bino.refreshAnnotations', () => annotationsView.refresh()),
        vscode.commands.registerCommand('bino.newAnnotation', () => form.open()),
        vscode.commands.registerCommand('bino.openAnnotationForm', async (arg?: AnnotationEntry | { entry?: AnnotationEntry }) => {
            const entry = (arg as { entry?: AnnotationEntry })?.entry ?? (arg as AnnotationEntry | undefined);
            if (entry?.name) { await form.open(entry); }
        }),
        vscode.commands.registerCommand('bino.revealAnnotation', async (arg?: AnnotationEntry | { entry?: AnnotationEntry }) => {
            const entry = (arg as { entry?: AnnotationEntry })?.entry ?? (arg as AnnotationEntry | undefined);
            if (!entry?.file) { return; }
            const doc = await vscode.workspace.openTextDocument(entry.file);
            const pos = new vscode.Position(entry.startLine, 0);
            await vscode.window.showTextDocument(doc, { selection: new vscode.Range(pos, pos), preview: true });
        }),
        vscode.commands.registerCommand('bino.deleteAnnotation', async (arg?: AnnotationEntry | { entry?: AnnotationEntry }) => {
            const picked = (arg as { entry?: AnnotationEntry })?.entry ?? (arg as AnnotationEntry | undefined);
            if (!picked?.name) { return; }
            const answer = await vscode.window.showWarningMessage(`Delete the annotation ${picked.name}?`, { modal: true }, 'Delete');
            if (answer !== 'Delete') { return; }
            // read again: the file may have changed since the view was built
            const entry = annotationEntries(await loadManifests(annotationsIndexer)).find(e => e.name === picked.name && e.file === picked.file);
            const doc = entry && await vscode.workspace.openTextDocument(entry.file);
            const lines = doc && entry && documentLines(doc.getText(), entry.position);
            if (!doc || !lines) {
                vscode.window.showErrorMessage(`Bino: annotation ${picked.name} not found.`);
                return;
            }
            const edit = new vscode.WorkspaceEdit();
            const end = lines[1] >= doc.lineCount ? doc.lineAt(doc.lineCount - 1).range.end : new vscode.Position(lines[1], 0);
            edit.delete(doc.uri, new vscode.Range(new vscode.Position(lines[0], 0), end));
            // unsaved edits of the user stay unsaved: then the delete is one more of them
            const hadEdits = doc.isDirty;
            if (!(await vscode.workspace.applyEdit(edit))) {
                vscode.window.showErrorMessage(`Bino: could not delete annotation ${picked.name}.`);
                return;
            }
            if (!hadEdits) { await doc.save(); }
            annotationsView.refresh();
        })
    );

    // Register tree view for Bino Documents (sidebar panel)
    const treeProvider = new BinoTreeProvider(indexer, validator);
    const treeView = vscode.window.createTreeView('binoDocuments', {
        treeDataProvider: treeProvider,
        showCollapseAll: true
    });
    context.subscriptions.push(treeView);

    // Replace the bundled schema with the backend's merged one (plugin kinds included).
    const reloadSchema = () => loadBackendSchema(backendSchemaSource(indexer?.getProjectRootForUri()))
        .then(loaded => {
            outputChannel.appendLine(loaded ? 'Loaded the schema from bino' : 'Using the bundled schema: bino did not serve one');
            if (loaded) {
                treeProvider.refresh();
                treeTableEditorManager?.refreshAll();
                designerPanel?.refreshAll();
                styleEditor?.refreshAll();
            }
        });
    void daemonReady.then(() => {
        void reloadSchema();
        // A new daemon connection (e.g. after a restart) may serve other plugin kinds.
        if (daemonClient) {
            context.subscriptions.push(daemonClient.onStatusChange(status => {
                if (status === 'connected') {
                    void reloadSchema();
                }
            }));
        }
    });
    // Plugins are declared in bino.toml.
    const schemaTomlWatcher = vscode.workspace.createFileSystemWatcher('**/bino.toml', false, false, true);
    context.subscriptions.push(
        schemaTomlWatcher,
        schemaTomlWatcher.onDidCreate(() => void reloadSchema()),
        schemaTomlWatcher.onDidChange(() => void reloadSchema()),
    );

    // Register Preview & Build tree
    const previewTreeProvider = new PreviewTreeProvider(previewManager);
    context.subscriptions.push(
        vscode.window.createTreeView('binoPreview', {
            treeDataProvider: previewTreeProvider,
        })
    );

    // Register Environment tree
    const environmentTreeProvider = new EnvironmentTreeProvider(indexer, validator);
    context.subscriptions.push(
        vscode.window.createTreeView('binoEnvironment', {
            treeDataProvider: environmentTreeProvider,
        })
    );

    // Register Dependencies tree (registry packages) - refreshes on the
    // daemon's registry-changed SSE event and on connection changes.
    const dependenciesTreeProvider = new DependenciesTreeProvider(() => daemonClient);
    context.subscriptions.push(
        vscode.window.createTreeView('binoDependencies', {
            treeDataProvider: dependenciesTreeProvider,
        })
    );
    if (daemonClient) {
        context.subscriptions.push(
            daemonClient.on('registry-changed', () => {
                outputChannel.appendLine('[Daemon] Registry changed via SSE push');
                dependenciesTreeProvider.refresh();
            }),
            daemonClient.onStatusChange(() => dependenciesTreeProvider.refresh())
        );
    }
    registerRegistryCommands(context, {
        daemonClient: () => daemonClient,
        projectRoot: () => indexer?.getProjectRootForUri(),
        runInTerminal: (args: string) => runInTerminal(args),
        depsTree: dependenciesTreeProvider,
    });

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
            void previewManager?.openInBrowser();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openPreviewInWebview', async () => {
            await previewManager?.openPreviewInWebview();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.previewArtefactEmbedded', async (arg?: LSPDocument | BinoTreeItem) => {
            // Tree menu passes a BinoTreeItem; CodeLens/palette pass an LSPDocument or nothing.
            const doc = (arg as BinoTreeItem)?.document ?? (arg as LSPDocument | undefined);
            await previewManager?.previewArtefactEmbedded(doc?.kind ? doc : undefined);
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.previewMenu', async () => {
            await previewManager?.showPreviewMenu();
        })
    );

    // Builds run as `bino` tasks: terminal output, Problems entries and "Open PDF".
    const taskProvider = new BinoTaskProvider(indexer, outputChannel, () => project.all);
    context.subscriptions.push(
        taskProvider,
        vscode.tasks.registerTaskProvider('bino', taskProvider),
        vscode.commands.registerCommand('bino.build', () => taskProvider.runBuild())
    );

    // Show columns for current dataset/datasource command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showColumnsForCurrentDataset', async () => {
            await showColumnsForCurrentDataset(indexer);
        })
    );

    // Preview rows for DataSource/DataSet (called from CodeLens)
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.previewRows', async (doc: LSPDocument) => {
            if (doc && rowsPreviewManager) {
                await rowsPreviewManager.showPreview(doc);
            }
        })
    );

    // Tree-table editor command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openTreeEditor', async () => {
            await treeTableEditorManager?.openForActiveEditor();
        })
    );

    // Component designer command
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openDesigner', async (arg?: LSPDocument | BinoTreeItem) => {
            // Tree menu passes a BinoTreeItem; palette passes an LSPDocument or nothing.
            const doc = (arg as BinoTreeItem)?.document ?? (arg as LSPDocument | undefined);
            await designerPanel?.open(doc?.kind ? doc : undefined);
        })
    );

    context.subscriptions.push(
        // The editor title passes the file's Uri, the Documents view its tree item.
        vscode.commands.registerCommand('bino.openStyleEditor', async (arg?: LSPDocument | BinoTreeItem | vscode.Uri) => {
            const doc = arg instanceof vscode.Uri ? arg : (arg as BinoTreeItem)?.document ?? (arg as LSPDocument | undefined);
            await styleEditor?.open(doc instanceof vscode.Uri || doc?.kind ? doc : undefined);
        }),
        vscode.commands.registerCommand('bino.openRuleSetEditor', async (arg?: LSPDocument | BinoTreeItem | vscode.Uri) => {
            const doc = arg instanceof vscode.Uri ? arg : (arg as BinoTreeItem)?.document ?? (arg as LSPDocument | undefined);
            await ruleSetEditor?.open(doc instanceof vscode.Uri || doc?.kind ? doc : undefined);
        })
    );

    // Code↔Design bridge: open the designer for the manifest under the cursor in a
    // text editor. With no argument the designer resolves the active editor's
    // cursor to its embeddable (falling back to a picker), so this is a thin entry
    // point that surfaces "Open in Designer" in the editor context / palette.
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.openInDesigner', async () => {
            await designerPanel?.open();
        })
    );

    // "Add element" palette - the single entry point that supersedes the static
    // `bino add` command list. Sources kinds (and their categories) from the live
    // backend list (plugin kinds appear automatically) and creates each pick
    // through the one authoring path: DataSource/DataSet open the wizard, every
    // other kind runs a schema-driven guided form and is created via the
    // AuthoringClient (the Go create path), then the new file opens.
    if (indexer && wizardManager) {
        const addElement = new AddElementCommand(
            indexer,
            wizardManager,
            kindIcon,
            context.extensionPath,
            (kind: string) => runInTerminal(`add ${kind.toLowerCase()}`),
            () => listAddSubcommands(indexer?.getProjectRootForUri()),
        );
        context.subscriptions.push(
            vscode.commands.registerCommand('bino.addElement', () => addElement.run())
        );
    }

    // DataSource/DataSet wizard commands
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.newDataSourceFromFile', (uri?: vscode.Uri) => {
            const target = uri ?? vscode.window.activeTextEditor?.document.uri;
            if (!target) {
                vscode.window.showErrorMessage('Bino: no file selected for the DataSource wizard.');
                return;
            }
            wizardManager?.openForFile(target);
        }),
        vscode.commands.registerCommand('bino.newDataSourceFromDatabase', () => {
            wizardManager?.openForDatabase();
        }),
        vscode.commands.registerCommand('bino.restartDaemon', async () => {
            const root = daemonClient?.getProjectRoot() ?? indexer?.getProjectRootForUri();
            if (!daemonClient || !root) {
                vscode.window.showWarningMessage('Bino: daemon is not enabled for this workspace.');
                return;
            }
            const ok = await daemonClient.restart(root);
            // On success the status listener restarts the language server for
            // the new daemon; without a daemon it must run standalone.
            if (!ok) {
                await restartLanguageClient();
            }
            vscode.window.showInformationMessage(ok ? 'Bino daemon restarted.' : 'Bino daemon failed to restart - check the output channel.');
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

    // Cache clean commands (run via child_process, not terminal)
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.cacheClean', async () => {
            try {
                await runBino(['cache', 'clean'], { cwd: indexer?.getProjectRootForUri() });
                vscode.window.showInformationMessage('Bino cache cleaned');
            } catch (err) {
                vscode.window.showErrorMessage(`Failed to clean cache: ${err}`);
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.cacheCleanGlobal', async () => {
            try {
                await runBino(['cache', 'clean', '--global'], { cwd: indexer?.getProjectRootForUri() });
                vscode.window.showInformationMessage('Bino global cache cleaned');
            } catch (err) {
                vscode.window.showErrorMessage(`Failed to clean global cache: ${err}`);
            }
        })
    );

    // Build specific artefact - QuickPick from indexed ReportArtefact names
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.buildArtefact', async () => {
            const artefacts = indexer?.getDocuments(BUILD_KINDS) ?? [];
            if (artefacts.length === 0) {
                vscode.window.showInformationMessage('No artefacts found in workspace');
                return;
            }
            const items = artefacts.map(a => ({ label: a.name, description: a.kind }));
            const picked = await vscode.window.showQuickPick(items, {
                placeHolder: 'Select an artefact to build',
                title: 'Bino: Build Artefact',
            });
            if (picked) {
                await taskProvider.runBuild(picked.label);
            }
        })
    );

    // Validate with queries
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.validateWithQueries', () => {
            runInTerminal('lint --execute-queries');
        })
    );

    // Graph commands (terminal)
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showGraphTree', () => {
            runInTerminal('graph --view tree');
        })
    );
    context.subscriptions.push(
        vscode.commands.registerCommand('bino.showGraphFlat', () => {
            runInTerminal('graph --view flat');
        })
    );

    // Initial index on activation
    await indexer.refreshIndex();

    // Restart what holds on to these settings; everything else reads them per call.
    const applySettingsChange = async (binPathChanged: boolean, previewPortChanged: boolean) => {
        if (binPathChanged) {
            outputChannel.appendLine('[Settings] bino.binPath changed');
            versionPromptedPid = undefined;
            environmentTreeProvider.refresh();
            const root = indexer?.getProjectRootForUri();
            // A new connection restarts the language server through the status listener.
            const connected = daemonClient && root && !daemonClient.isConnected && await daemonClient.connect(root);
            if (!connected) {
                void checkDaemonVersion();
                await restartLanguageClient();
            }
        }
        const preview = previewManager;
        if (preview?.getStatus() === 'running' && (previewPortChanged || (binPathChanged && preview.runsOwnProcess))) {
            outputChannel.appendLine('[Settings] Restarting preview for the new settings');
            await preview.restartPreview();
        }
    };
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration(e => {
            if (e.affectsConfiguration('bino.daemon.enabled')) {
                // The daemon is wired into every feature at activation.
                void vscode.window.showInformationMessage(
                    'Reload the window to apply the "bino.daemon.enabled" setting.',
                    'Reload Window'
                ).then(choice => {
                    if (choice === 'Reload Window') {
                        void vscode.commands.executeCommand('workbench.action.reloadWindow');
                    }
                });
            }
            const binPathChanged = e.affectsConfiguration('bino.binPath');
            const previewPortChanged = e.affectsConfiguration('bino.previewPort');
            if (binPathChanged || previewPortChanged) {
                void applySettingsChange(binPathChanged, previewPortChanged);
            }
        })
    );

    console.log('Bino Reports extension activated');
}

// The daemon pid we last warned about, so each daemon gets one message.
let versionPromptedPid: number | undefined;

/** Warn once when the connected daemon was started by another bino version than the installed one. */
async function checkDaemonVersion(): Promise<void> {
    const client = daemonClient;
    if (!client?.isConnected || client.pid === undefined || client.pid === versionPromptedPid) {
        return;
    }
    const installed = await installedBinoVersion();
    if (!daemonVersionMismatch(client.version, installed) || client.pid === versionPromptedPid) {
        return;
    }
    versionPromptedPid = client.pid;
    const running = client.version ? `bino ${client.version}` : 'an older bino';
    const choice = await vscode.window.showWarningMessage(
        `A daemon from ${running} is running, but bino ${installed} is installed. Restart Daemon?`,
        'Restart Daemon'
    );
    if (choice === 'Restart Daemon') {
        await vscode.commands.executeCommand('bino.restartDaemon');
    }
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
            label: `$(${kindIcon(node.kind)}) ${node.name}`,
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

/** Update the daemon status bar item */
export function deactivate(): void {
    // Disconnect only: other windows and MCP clients may share the daemon, and
    // it exits by itself once no client is connected.
    daemonClient?.shutdown();
    designerPanel?.dispose();
    treeTableEditorManager?.dispose();
    previewManager?.dispose();
    validator?.dispose();
    daemonClient = undefined;
    indexer = undefined;
    validator = undefined;
    designerPanel = undefined;
    styleEditor?.dispose();
    ruleSetEditor?.dispose();
    styleEditor = undefined;
    ruleSetEditor = undefined;
    annotationForm?.dispose();
    annotationForm = undefined;
    treeTableEditorManager = undefined;
    previewManager = undefined;
}
