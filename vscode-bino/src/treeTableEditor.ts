import * as vscode from 'vscode';
import { WorkspaceIndexer } from './indexer';
import { SchemaResolver, FieldDef } from './schemaResolver';
import { parseYamlDocuments, resolveLineNumbers, applyEdit, removeField, addField } from './yamlModel';
import { getTreeTableHtml, getErrorHtml, getPlaceholderHtml } from './treeTableHtml';

/**
 * Manages the tree-table editor webview panel.
 * Follows the RowsPreviewManager pattern: single panel, synced to the active text editor.
 */
export class TreeTableEditorManager {
    private panel: vscode.WebviewPanel | undefined;
    private indexer: WorkspaceIndexer;
    private schema: SchemaResolver;
    private currentEditor: vscode.TextEditor | undefined;
    private debounceTimer: ReturnType<typeof setTimeout> | undefined;
    private suppressForwardSync = false;
    private disposables: vscode.Disposable[] = [];

    constructor(
        indexer: WorkspaceIndexer,
        extensionPath: string
    ) {
        this.indexer = indexer;
        this.schema = new SchemaResolver(extensionPath);
        this.schema.load();
    }

    /** Open or reveal the tree-table editor panel */
    openPanel(): void {
        if (this.panel) {
            this.panel.reveal(vscode.ViewColumn.Beside);
            this.syncToActiveEditor();
            return;
        }

        this.panel = vscode.window.createWebviewPanel(
            'binoTreeEditor',
            'Bino Tree Editor',
            vscode.ViewColumn.Beside,
            {
                enableScripts: true,
                retainContextWhenHidden: true,
            }
        );

        // Panel dispose
        this.panel.onDidDispose(() => {
            this.panel = undefined;
            this.currentEditor = undefined;
            this.disposeListeners();
        });

        // Handle messages from webview
        this.panel.webview.onDidReceiveMessage(msg => this.handleMessage(msg));

        // Set up listeners
        this.setupListeners();

        // Initial sync
        this.syncToActiveEditor();
    }

    /** Set up VS Code event listeners */
    private setupListeners(): void {
        // Forward sync: text document changes -> update tree
        this.disposables.push(
            vscode.workspace.onDidChangeTextDocument(e => {
                if (this.suppressForwardSync) { return; }
                if (this.currentEditor && e.document === this.currentEditor.document) {
                    this.debouncedSync();
                }
            })
        );

        // Active editor change -> re-parse if bino YAML
        this.disposables.push(
            vscode.window.onDidChangeActiveTextEditor(editor => {
                // Don't react to our own webview getting focus
                if (!editor) { return; }
                if (editor.document.uri.scheme === 'output') { return; }
                this.syncToActiveEditor(editor);
            })
        );
    }

    private disposeListeners(): void {
        for (const d of this.disposables) { d.dispose(); }
        this.disposables = [];
    }

    /** Debounce forward sync to avoid thrashing on fast typing */
    private debouncedSync(): void {
        if (this.debounceTimer) { clearTimeout(this.debounceTimer); }
        this.debounceTimer = setTimeout(() => {
            this.forwardSync();
        }, 300);
    }

    /** Sync the tree to the current active text editor */
    private syncToActiveEditor(editor?: vscode.TextEditor): void {
        const target = editor || vscode.window.activeTextEditor;
        if (!target || !this.panel) { return; }

        // Check if it's a YAML file
        if (target.document.languageId !== 'yaml') {
            if (this.currentEditor?.document.languageId === 'yaml') {
                // Switched away from a YAML file — show placeholder
                this.currentEditor = undefined;
                this.panel.webview.html = getPlaceholderHtml();
            }
            return;
        }

        // Check if it's a bino document
        const text = target.document.getText();
        if (!text.includes('apiVersion: bino.bi')) {
            this.currentEditor = undefined;
            this.panel.webview.html = getPlaceholderHtml();
            return;
        }

        this.currentEditor = target;
        this.forwardSync();
    }

    /** Parse the current editor and update the webview */
    private forwardSync(): void {
        if (!this.panel || !this.currentEditor) { return; }

        const text = this.currentEditor.document.getText();

        try {
            const docs = parseYamlDocuments(text);
            resolveLineNumbers(text, docs);

            // Build field defs for each kind
            const kindFieldsMap = new Map<string, FieldDef[]>();
            const metadataFieldsMap = new Map<string, FieldDef[]>();

            for (const doc of docs) {
                if (doc.kind && !kindFieldsMap.has(doc.kind)) {
                    kindFieldsMap.set(doc.kind, this.schema.getFieldsForKind(doc.kind));
                    metadataFieldsMap.set(doc.kind, this.schema.getMetadataFields(doc.kind));
                }
            }

            // Also add default metadata fields
            metadataFieldsMap.set('_default', this.schema.getMetadataFields());

            const html = getTreeTableHtml(docs, kindFieldsMap, metadataFieldsMap);

            // Use postMessage for incremental updates (faster than replacing entire HTML)
            // For the initial render, set the full HTML
            if (!this.panel.webview.html || this.panel.webview.html.includes('empty-state') || this.panel.webview.html.includes('placeholder')) {
                this.panel.webview.html = html;
            } else {
                // Extract just the tree-table content for incremental update
                const match = html.match(/<div id="tree-table">([\s\S]*)<\/div>\s*<div id="completion-dropdown"/);
                if (match) {
                    this.panel.webview.postMessage({ type: 'setTree', html: match[1] });
                } else {
                    this.panel.webview.html = html;
                }
            }
        } catch {
            this.panel.webview.html = getErrorHtml('Failed to parse YAML. Fix syntax errors and try again.');
        }
    }

    /** Handle messages from the webview */
    private async handleMessage(msg: any): Promise<void> {
        switch (msg.type) {
            case 'goToLine':
                await this.handleGoToLine(msg.docIndex, msg.line);
                break;
            case 'editValue':
                await this.handleEditValue(msg.docIndex, msg.path, msg.newValue);
                break;
            case 'removeField':
                await this.handleRemoveField(msg.docIndex, msg.path);
                break;
            case 'addField':
                await this.handleAddField(msg.docIndex, msg.parentPath, msg.key, msg.fieldType, msg.defaultValue);
                break;
            case 'addArrayItem':
                await this.handleAddArrayItem(msg.docIndex, msg.path, msg.itemIsObject);
                break;
            case 'addTypedArrayItem':
                await this.handleAddTypedArrayItem(msg.docIndex, msg.path, msg.kindEnum);
                break;
            case 'requestCompletions':
                await this.handleRequestCompletions(msg.docIndex, msg.path, msg.fieldType, msg.currentValue);
                break;
        }
    }

    /** Navigate the text editor to a specific line */
    private async handleGoToLine(docIndex: number, line: number): Promise<void> {
        if (!this.currentEditor) { return; }

        // Reveal the text editor (it might be behind the webview)
        await vscode.window.showTextDocument(this.currentEditor.document, {
            viewColumn: this.currentEditor.viewColumn,
            preserveFocus: false,
        });

        const position = new vscode.Position(line, 0);
        this.currentEditor.selection = new vscode.Selection(position, position);
        this.currentEditor.revealRange(
            new vscode.Range(position, position),
            vscode.TextEditorRevealType.InCenter
        );
    }

    /** Apply a value edit from the webview to the text document */
    private async handleEditValue(docIndex: number, path: string[], newValue: unknown): Promise<void> {
        if (!this.currentEditor) { return; }

        const text = this.currentEditor.document.getText();
        const result = applyEdit(text, docIndex, path, newValue);
        if (!result) { return; }

        this.suppressForwardSync = true;
        try {
            const fullRange = new vscode.Range(
                this.currentEditor.document.positionAt(0),
                this.currentEditor.document.positionAt(text.length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(this.currentEditor.document.uri, fullRange, result.newText);
            await vscode.workspace.applyEdit(edit);
        } finally {
            // Allow forward sync after a short delay to let the edit settle
            setTimeout(() => {
                this.suppressForwardSync = false;
                this.forwardSync(); // Re-sync to pick up any normalization by the yaml lib
            }, 100);
        }
    }

    /** Remove a field from the YAML document */
    private async handleRemoveField(docIndex: number, path: string[]): Promise<void> {
        if (!this.currentEditor) { return; }

        const text = this.currentEditor.document.getText();
        const result = removeField(text, docIndex, path);
        if (!result) { return; }

        this.suppressForwardSync = true;
        try {
            const fullRange = new vscode.Range(
                this.currentEditor.document.positionAt(0),
                this.currentEditor.document.positionAt(text.length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(this.currentEditor.document.uri, fullRange, result.newText);
            await vscode.workspace.applyEdit(edit);
        } finally {
            setTimeout(() => {
                this.suppressForwardSync = false;
                this.forwardSync();
            }, 100);
        }
    }

    /** Add a new field to the YAML document */
    private async handleAddField(docIndex: number, parentPath: string[], key: string, fieldType: string, providedDefault?: unknown): Promise<void> {
        if (!this.currentEditor) { return; }

        const defaultValue = providedDefault !== undefined && providedDefault !== null ? providedDefault : getDefaultValueForType(fieldType);
        const fullPath = [...parentPath, key];

        const text = this.currentEditor.document.getText();
        const result = addField(text, docIndex, fullPath, defaultValue);
        if (!result) { return; }

        this.suppressForwardSync = true;
        try {
            const fullRange = new vscode.Range(
                this.currentEditor.document.positionAt(0),
                this.currentEditor.document.positionAt(text.length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(this.currentEditor.document.uri, fullRange, result.newText);
            await vscode.workspace.applyEdit(edit);
        } finally {
            setTimeout(() => {
                this.suppressForwardSync = false;
                this.forwardSync();
            }, 100);
        }
    }

    /** Add a new item to an array */
    private async handleAddArrayItem(docIndex: number, path: string[], itemIsObject?: boolean): Promise<void> {
        if (!this.currentEditor) { return; }

        const text = this.currentEditor.document.getText();

        // Parse to find the current array length, then add at that index
        const docs = parseYamlDocuments(text);
        const doc = docs.find(d => d.docIndex === docIndex);
        if (!doc) { return; }

        // Walk the node tree to find the array node
        let arrayNode: { children?: { length: number } } | undefined;
        let current: typeof doc.nodes = doc.nodes;
        for (const segment of path) {
            const node = current.find(n => n.key === segment);
            if (!node) { return; }
            if (node.type === 'array' && node.children) {
                arrayNode = node;
            }
            current = node.children || [];
        }
        if (!arrayNode?.children) { return; }

        const newIndex = String(arrayNode.children.length);
        const itemDefault = itemIsObject ? {} : '';
        const result = addField(text, docIndex, [...path, newIndex], itemDefault);
        if (!result) { return; }

        this.suppressForwardSync = true;
        try {
            const fullRange = new vscode.Range(
                this.currentEditor.document.positionAt(0),
                this.currentEditor.document.positionAt(text.length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(this.currentEditor.document.uri, fullRange, result.newText);
            await vscode.workspace.applyEdit(edit);
        } finally {
            setTimeout(() => {
                this.suppressForwardSync = false;
                this.forwardSync();
            }, 100);
        }
    }

    /**
     * Add a typed child to a children array (LayoutPage/LayoutCard/Grid).
     * Shows QuickPick for kind, then for ref vs inline.
     */
    private async handleAddTypedArrayItem(docIndex: number, path: string[], kindEnum: string[]): Promise<void> {
        if (!this.currentEditor) { return; }

        // Step 1: Pick a kind
        const kindItems = kindEnum.map(k => ({ label: k }));
        const pickedKind = await vscode.window.showQuickPick(kindItems, {
            placeHolder: 'Select component kind',
            title: 'Add Child Component',
        });
        if (!pickedKind) { return; }
        const kind = pickedKind.label;

        // Step 2: Pick ref or inline
        // Get existing documents of this kind from the indexer
        const existingDocs = this.indexer.getDocumentNames([kind]);

        interface RefQuickPickItem extends vscode.QuickPickItem {
            isInline?: boolean;
            refName?: string;
        }

        const refItems: RefQuickPickItem[] = [
            { label: '$(add) New inline component', description: 'Create with empty spec', isInline: true },
        ];
        for (const name of existingDocs) {
            refItems.push({ label: name, description: `Reference existing ${kind}`, refName: name });
        }

        const pickedRef = await vscode.window.showQuickPick(refItems, {
            placeHolder: `New inline ${kind} or reference existing?`,
            title: `Add ${kind}`,
        });
        if (!pickedRef) { return; }

        // Build the child object
        let childObj: Record<string, unknown>;
        if (pickedRef.isInline) {
            childObj = { kind, spec: {} };
        } else {
            childObj = { kind, ref: pickedRef.refName };
        }

        // Find current array length
        const text = this.currentEditor.document.getText();
        const docs = parseYamlDocuments(text);
        const doc = docs.find(d => d.docIndex === docIndex);
        if (!doc) { return; }

        let arrayLen = -1; // -1 means array doesn't exist yet
        let current = doc.nodes;
        for (const segment of path) {
            const node = current.find(n => n.key === segment);
            if (!node) { arrayLen = -1; break; }
            if (node.type === 'array' && node.children) {
                arrayLen = node.children.length;
            }
            current = node.children || [];
        }

        // If array doesn't exist, create it with the child as first item
        // If it exists, append to it
        let result: { newText: string } | undefined;
        if (arrayLen < 0) {
            result = addField(text, docIndex, path, [childObj]);
        } else {
            result = addField(text, docIndex, [...path, String(arrayLen)], childObj);
        }
        if (!result) { return; }

        this.suppressForwardSync = true;
        try {
            const fullRange = new vscode.Range(
                this.currentEditor.document.positionAt(0),
                this.currentEditor.document.positionAt(text.length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(this.currentEditor.document.uri, fullRange, result.newText);
            await vscode.workspace.applyEdit(edit);
        } finally {
            setTimeout(() => {
                this.suppressForwardSync = false;
                this.forwardSync();
            }, 100);
        }
    }

    /** Handle completion requests from the webview */
    private async handleRequestCompletions(
        docIndex: number,
        path: string[],
        fieldType: string,
        currentValue: string
    ): Promise<void> {
        if (!this.panel) { return; }

        let items: string[] = [];

        switch (fieldType) {
            case 'dataset':
                items = this.indexer.getDatasetCompletions();
                break;
            case 'signingProfile':
                items = this.indexer.getDocumentNames(['SigningProfile']);
                break;
            case 'source':
                items = this.indexer.getAssetCompletions();
                break;
            case 'ref': {
                // Get all referenceable kinds
                const refKinds = ['Text', 'Table', 'ChartStructure', 'ChartTime', 'Tree', 'Grid', 'LayoutCard', 'Image'];
                items = this.indexer.getDocumentNames(refKinds);
                break;
            }
            case 'layoutPage':
                items = this.indexer.getDocumentNames(['LayoutPage']);
                break;
            case 'kind':
                items = this.schema.getKindEnum();
                break;
            case 'column': {
                // Find the dataset reference in the same document
                const columns = await this.getColumnsForDoc(docIndex);
                items = columns;
                break;
            }
        }

        // Filter by current value if the user has typed something
        if (currentValue) {
            const lower = currentValue.toLowerCase();
            items = items.filter(item => item.toLowerCase().includes(lower));
        }

        this.panel.webview.postMessage({ type: 'completions', items });
    }

    /** Get column names for the dataset referenced in a document */
    private async getColumnsForDoc(docIndex: number): Promise<string[]> {
        if (!this.currentEditor) { return []; }

        const text = this.currentEditor.document.getText();
        const docs = parseYamlDocuments(text);
        const doc = docs[docIndex];
        if (!doc) { return []; }

        // Find the dataset field in spec
        const specNode = doc.nodes.find(n => n.key === 'spec');
        if (!specNode?.children) { return []; }

        const datasetNode = specNode.children.find(n => n.key === 'dataset');
        if (!datasetNode || typeof datasetNode.value !== 'string') { return []; }

        try {
            return await this.indexer.getColumns(datasetNode.value);
        } catch {
            return [];
        }
    }

    dispose(): void {
        this.disposeListeners();
        if (this.debounceTimer) { clearTimeout(this.debounceTimer); }
        if (this.panel) { this.panel.dispose(); }
    }
}

/** Get a sensible default value for a field type */
function getDefaultValueForType(fieldType: string): unknown {
    switch (fieldType) {
        case 'string': return '';
        case 'number': case 'integer': return 0;
        case 'boolean': return false;
        case 'array': return [];
        case 'object': return {};
        default: return '';
    }
}
