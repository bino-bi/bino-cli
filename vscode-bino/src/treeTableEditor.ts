import * as vscode from 'vscode';
import { parseAllDocuments, Document, Scalar, YAMLMap, YAMLSeq, Pair, isScalar, isMap, isSeq, isPair } from 'yaml';
import { WorkspaceIndexer } from './indexer';
import { SchemaResolver, FieldDef } from './schemaResolver';
import { AuthoringClient, formatEditDiagnostics, EditResult } from './authoringClient';
import { getNonce } from './webviewNonce';
import { getTreeTableHtml, renderTreeContent, renderTreeMessage, TreeDocument, TreeNode } from './treeTableHtml';

/** One open Tree editor, bound to one TextDocument. */
interface TreeSession {
    document: vscode.TextDocument;
    panel: vscode.WebviewPanel;
    debounceTimer?: ReturnType<typeof setTimeout>;
    suppressForwardSync: boolean;
}

/**
 * The tree-table editor as a custom text editor on a YAML file. The webview
 * page loads once; each change posts the new tree to it. Writes go through the
 * AuthoringClient as a WorkspaceEdit on the open TextDocument, so undo works.
 */
export class TreeEditorProvider implements vscode.CustomTextEditorProvider {
    static readonly viewType = 'bino.treeEditor';

    private indexer: WorkspaceIndexer;
    private authoring: AuthoringClient;
    private schema: SchemaResolver;
    private readonly sessions = new Set<TreeSession>();

    constructor(
        indexer: WorkspaceIndexer,
        extensionPath: string
    ) {
        this.indexer = indexer;
        this.authoring = new AuthoringClient(indexer);
        this.schema = new SchemaResolver(extensionPath);
        this.schema.load();
    }

    /** Open the tree editor for the active YAML text editor, beside it. */
    async openForActiveEditor(): Promise<void> {
        const editor = vscode.window.activeTextEditor;
        if (!editor || editor.document.languageId !== 'yaml') {
            vscode.window.showInformationMessage('Bino: open a Bino YAML file to use the Tree Editor.');
            return;
        }
        await vscode.commands.executeCommand('vscode.openWith', editor.document.uri, TreeEditorProvider.viewType, vscode.ViewColumn.Beside);
    }

    resolveCustomTextEditor(document: vscode.TextDocument, panel: vscode.WebviewPanel): void {
        const session: TreeSession = { document, panel, suppressForwardSync: false };
        this.sessions.add(session);
        panel.webview.options = { enableScripts: true };
        panel.webview.html = getTreeTableHtml(getNonce());

        const listeners = [
            panel.webview.onDidReceiveMessage(msg => this.handleMessage(session, msg)),
            // Forward sync: document changes -> update tree, debounced for fast typing.
            vscode.workspace.onDidChangeTextDocument(e => {
                if (session.suppressForwardSync || e.document.uri.toString() !== document.uri.toString()) { return; }
                if (session.debounceTimer) { clearTimeout(session.debounceTimer); }
                session.debounceTimer = setTimeout(() => void this.forwardSync(session), 300);
            }),
        ];
        panel.onDidDispose(() => {
            if (session.debounceTimer) { clearTimeout(session.debounceTimer); }
            for (const l of listeners) { l.dispose(); }
            this.sessions.delete(session);
        });
    }

    /** Parse the document and post the new tree to the webview */
    private async forwardSync(session: TreeSession): Promise<void> {
        const text = session.document.getText();
        const version = session.document.version;
        const isBino = await this.indexer.isBinoDocument(session.document);
        if (session.document.version !== version) {
            return; // a newer sync follows
        }
        if (!isBino) {
            this.postTree(session, renderTreeMessage('This file is not a Bino manifest.', false));
            return;
        }

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

            this.postTree(session, renderTreeContent(docs, kindFieldsMap, metadataFieldsMap));
        } catch {
            this.postTree(session, renderTreeMessage('Failed to parse YAML. Fix syntax errors and try again.', true));
        }
    }

    private postTree(session: TreeSession, html: string): void {
        // A delayed re-sync after an edit can land after the editor closed.
        if (!this.sessions.has(session)) { return; }
        void session.panel.webview.postMessage({ type: 'setTree', html });
    }

    /** Handle messages from the webview */
    private async handleMessage(session: TreeSession, msg: any): Promise<void> {
        switch (msg.type) {
            case 'ready':
                void this.forwardSync(session);
                break;
            case 'goToLine':
                await this.handleGoToLine(session, msg.line);
                break;
            case 'editValue':
                await this.handleEditValue(session, msg.docIndex, msg.path, msg.newValue);
                break;
            case 'removeField':
                await this.handleRemoveField(session, msg.docIndex, msg.path);
                break;
            case 'addField':
                await this.handleAddField(session, msg.docIndex, msg.parentPath, msg.key, msg.fieldType, msg.defaultValue);
                break;
            case 'addArrayItem':
                await this.handleAddArrayItem(session, msg.docIndex, msg.path, msg.itemIsObject);
                break;
            case 'addTypedArrayItem':
                await this.handleAddTypedArrayItem(session, msg.docIndex, msg.path, msg.kindEnum);
                break;
            case 'requestCompletions':
                await this.handleRequestCompletions(session, msg.docIndex, msg.path, msg.fieldType, msg.currentValue);
                break;
        }
    }

    /** Show the document in a text editor (beside the tree) at a line */
    private async handleGoToLine(session: TreeSession, line: number): Promise<void> {
        const position = new vscode.Position(line, 0);
        const visible = vscode.window.visibleTextEditors.find(e => e.document === session.document);
        const editor = await vscode.window.showTextDocument(session.document, {
            viewColumn: visible?.viewColumn ?? vscode.ViewColumn.Beside,
            preserveFocus: false,
            selection: new vscode.Range(position, position),
        });
        editor.revealRange(new vscode.Range(position, position), vscode.TextEditorRevealType.InCenter);
    }

    /** Apply a value edit from the webview to the text document */
    private async handleEditValue(session: TreeSession, docIndex: number, path: string[], newValue: unknown): Promise<void> {
        await this.applyAuthoringEdit(session,
            this.authoring.edit({
                file: session.document.uri.fsPath,
                position: docIndex + 1,
                patch: { [path.join('.')]: newValue },
            })
        );
    }

    /** Remove a field from the YAML document */
    private async handleRemoveField(session: TreeSession, docIndex: number, path: string[]): Promise<void> {
        await this.applyAuthoringEdit(session,
            this.authoring.remove({
                file: session.document.uri.fsPath,
                position: docIndex + 1,
                paths: [path.join('.')],
            })
        );
    }

    /** Add a new field to the YAML document */
    private async handleAddField(session: TreeSession, docIndex: number, parentPath: string[], key: string, fieldType: string, providedDefault?: unknown): Promise<void> {

        const defaultValue = providedDefault !== undefined && providedDefault !== null ? providedDefault : getDefaultValueForType(fieldType);
        const fullPath = [...parentPath, key];

        await this.applyAuthoringEdit(session,
            this.authoring.edit({
                file: session.document.uri.fsPath,
                position: docIndex + 1,
                patch: { [fullPath.join('.')]: defaultValue },
            })
        );
    }

    /**
     * Apply an authoring mutation that targets the session's open document.
     * The AuthoringClient merges it via a WorkspaceEdit (firing onDidChange), so
     * we hold the forward-sync guard across the apply and re-sync afterwards.
     * A rejected edit (schema diagnostics) is surfaced and the buffer untouched.
     */
    private async applyAuthoringEdit(session: TreeSession, pending: Promise<EditResult>): Promise<void> {
        session.suppressForwardSync = true;
        try {
            const result = await pending;
            if (!result.ok) {
                vscode.window.showErrorMessage(`Edit rejected: ${formatEditDiagnostics(result)}`);
            }
        } finally {
            // Allow forward sync after a short delay to let the edit settle, then
            // re-sync to pick up any normalization by the engine.
            setTimeout(() => {
                session.suppressForwardSync = false;
                void this.forwardSync(session);
            }, 100);
        }
    }

    /** Add a new item to an array */
    private async handleAddArrayItem(session: TreeSession, docIndex: number, path: string[], itemIsObject?: boolean): Promise<void> {

        // The Go engine appends to the end of the sequence (creating it if absent),
        // so there is no need to compute the current array length here.
        const itemDefault = itemIsObject ? {} : '';
        await this.applyAuthoringEdit(session,
            this.authoring.append({
                file: session.document.uri.fsPath,
                position: docIndex + 1,
                path: path.join('.'),
                value: itemDefault,
            })
        );
    }

    /**
     * Add a typed child to a children array (LayoutPage/LayoutCard/Grid).
     * Shows QuickPick for kind, then for ref vs inline.
     */
    private async handleAddTypedArrayItem(session: TreeSession, docIndex: number, path: string[], kindEnum: string[]): Promise<void> {

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

        // The Go engine appends the child, auto-vivifying the children array if it
        // does not exist yet, so both the create-first and append cases are one op.
        await this.applyAuthoringEdit(session,
            this.authoring.append({
                file: session.document.uri.fsPath,
                position: docIndex + 1,
                path: path.join('.'),
                value: childObj,
            })
        );
    }

    /** Handle completion requests from the webview */
    private async handleRequestCompletions(
        session: TreeSession,
        docIndex: number,
        path: string[],
        fieldType: string,
        currentValue: string
    ): Promise<void> {
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
                items = this.indexer.getDocumentNames(this.schema.getLayoutChildKinds());
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
                const columns = await this.getColumnsForDoc(session.document, docIndex);
                items = columns;
                break;
            }
        }

        // Filter by current value if the user has typed something
        if (currentValue) {
            const lower = currentValue.toLowerCase();
            items = items.filter(item => item.toLowerCase().includes(lower));
        }

        void session.panel.webview.postMessage({ type: 'completions', items });
    }

    /** Get column names for the dataset referenced in a document */
    private async getColumnsForDoc(document: vscode.TextDocument, docIndex: number): Promise<string[]> {
        const text = document.getText();
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
        for (const session of this.sessions) { session.panel.dispose(); }
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

// --- Read-only YAML parsing for the tree view ---
//
// The webview renders a CST-preserving *read* of the manifest into TreeDocument
// models; all *writes* go through the AuthoringClient / Go engine. This is not a
// second YAML-fidelity write engine - it only feeds the renderer and column
// lookups below.

/**
 * Parse a multi-document YAML string into TreeDocument models.
 * Uses the `yaml` library for CST-preserving parsing.
 */
function parseYamlDocuments(text: string): TreeDocument[] {
    const docs: TreeDocument[] = [];
    let parsed: Document[];

    try {
        parsed = parseAllDocuments(text, { keepSourceTokens: true });
    } catch {
        return [];
    }

    const lines = text.split('\n');

    for (let i = 0; i < parsed.length; i++) {
        const doc = parsed[i];
        if (!doc.contents) { continue; }
        if (doc.errors && doc.errors.length > 0) { continue; }

        const json = doc.toJSON();
        if (!json || typeof json !== 'object') { continue; }

        const kind = json.kind || '';
        const name = json.metadata?.name || '';

        // Compute start/end lines from document range
        const range = doc.range;
        let startLine = 0;
        let endLine = lines.length - 1;

        if (range) {
            startLine = offsetToLine(text, range[0]);
            endLine = offsetToLine(text, range[1]);
        }

        const nodes = mapContentsToNodes(doc.contents, []);
        docs.push({ docIndex: i, kind, name, nodes, startLine, endLine });
    }

    return docs;
}

/** Convert an offset in the text to a 0-based line number */
function offsetToLine(text: string, offset: number): number {
    let line = 0;
    for (let i = 0; i < offset && i < text.length; i++) {
        if (text[i] === '\n') { line++; }
    }
    return line;
}

/** Convert an offset to a 0-based column number */
function offsetToColumn(text: string, offset: number): number {
    let col = 0;
    for (let i = offset - 1; i >= 0; i--) {
        if (text[i] === '\n') { break; }
        col++;
    }
    return col;
}

/** Map yaml library node to TreeNode array */
function mapContentsToNodes(node: unknown, parentPath: string[]): TreeNode[] {
    if (isMap(node)) {
        return mapMapNode(node as YAMLMap, parentPath);
    }
    return [];
}

/** Map a YAMLMap's pairs to TreeNode array */
function mapMapNode(map: YAMLMap, parentPath: string[]): TreeNode[] {
    const nodes: TreeNode[] = [];

    for (const item of map.items) {
        if (!isPair(item)) { continue; }
        const pair = item as Pair;

        const key = isScalar(pair.key) ? String((pair.key as Scalar).value) : String(pair.key);
        const path = [...parentPath, key];

        const treeNode = valueToTreeNode(key, pair.value, path, pair.key);
        if (treeNode) {
            nodes.push(treeNode);
        }
    }

    return nodes;
}

/** Convert a YAML value node to a TreeNode */
function valueToTreeNode(key: string, value: unknown, path: string[], keyNode?: unknown): TreeNode | undefined {
    // Get line/column from the key node's range
    let line = 0;
    let column = 0;
    if (isScalar(keyNode)) {
        const range = (keyNode as any).range;
        if (range) {
            // range is [start, valueEnd, nodeEnd] offsets - but we don't have the text here
            // We'll use a simplified approach and store the range offsets
            line = range[0]; // Will be converted later
            column = 0;
        }
    }

    if (isScalar(value)) {
        const scalar = value as Scalar;
        const rawValue = scalar.value;
        const type = inferScalarType(rawValue);
        const isMultiline = typeof rawValue === 'string' && rawValue.includes('\n');
        const valueRange = (scalar as any).range
            ? { start: (scalar as any).range[0], end: (scalar as any).range[1] }
            : undefined;

        return {
            key,
            value: rawValue,
            displayValue: formatDisplayValue(rawValue, isMultiline),
            type: isMultiline ? 'multiline' : type,
            path,
            line,
            column,
            valueRange,
        };
    }

    if (isMap(value)) {
        const children = mapMapNode(value as YAMLMap, path);
        return {
            key,
            value: undefined,
            displayValue: `{${children.length}}`,
            type: 'object',
            path,
            line,
            column,
            children,
        };
    }

    if (isSeq(value)) {
        const seq = value as YAMLSeq;
        const children: TreeNode[] = [];

        for (let i = 0; i < seq.items.length; i++) {
            const item = seq.items[i];
            const itemPath = [...path, String(i)];
            const itemKey = `[${i}]`;

            if (isMap(item)) {
                const mapChildren = mapMapNode(item as YAMLMap, itemPath);
                children.push({
                    key: itemKey,
                    value: undefined,
                    displayValue: `{${mapChildren.length}}`,
                    type: 'object',
                    path: itemPath,
                    line: 0,
                    column: 0,
                    children: mapChildren,
                });
            } else if (isScalar(item)) {
                const scalar = item as Scalar;
                const rawValue = scalar.value;
                const type = inferScalarType(rawValue);
                const valueRange = (scalar as any).range
                    ? { start: (scalar as any).range[0], end: (scalar as any).range[1] }
                    : undefined;
                children.push({
                    key: itemKey,
                    value: rawValue,
                    displayValue: formatDisplayValue(rawValue, false),
                    type,
                    path: itemPath,
                    line: 0,
                    column: 0,
                    valueRange,
                });
            } else if (isSeq(item)) {
                children.push({
                    key: itemKey,
                    value: undefined,
                    displayValue: `[${(item as YAMLSeq).items.length}]`,
                    type: 'array',
                    path: itemPath,
                    line: 0,
                    column: 0,
                });
            }
        }

        return {
            key,
            value: undefined,
            displayValue: `[${children.length}]`,
            type: 'array',
            path,
            line,
            column,
            children,
        };
    }

    // null / undefined
    return {
        key,
        value: null,
        displayValue: 'null',
        type: 'null',
        path,
        line,
        column,
    };
}

/** Infer the scalar type */
function inferScalarType(value: unknown): 'string' | 'number' | 'boolean' | 'null' {
    if (value === null || value === undefined) { return 'null'; }
    if (typeof value === 'boolean') { return 'boolean'; }
    if (typeof value === 'number') { return 'number'; }
    return 'string';
}

/** Format a value for display in the tree */
function formatDisplayValue(value: unknown, isMultiline: boolean): string {
    if (value === null || value === undefined) { return 'null'; }
    if (typeof value === 'boolean') { return value ? 'true' : 'false'; }
    if (typeof value === 'number') { return String(value); }
    if (isMultiline) { return '(multiline)'; }
    const str = String(value);
    if (str.length > 80) { return str.substring(0, 80) + '...'; }
    return str;
}

/**
 * Resolve line numbers from offsets.
 * Call this after parseYamlDocuments to convert offset-based line numbers
 * to actual 0-based line numbers.
 */
function resolveLineNumbers(text: string, docs: TreeDocument[]): void {
    for (const doc of docs) {
        resolveNodeLines(text, doc.nodes);
    }
}

/** Recursively resolve line numbers for a node tree */
function resolveNodeLines(text: string, nodes: TreeNode[]): void {
    for (const node of nodes) {
        if (node.line > 0) {
            // line currently holds the byte offset from the key node
            const offset = node.line;
            node.line = offsetToLine(text, offset);
            node.column = offsetToColumn(text, offset);
        }
        if (node.children) {
            resolveNodeLines(text, node.children);
        }
    }
}
