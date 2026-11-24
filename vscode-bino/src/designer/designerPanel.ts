import * as vscode from 'vscode';
import { parseAllDocuments, isMap, isScalar, Document, Scalar, YAMLMap } from 'yaml';
import { WorkspaceIndexer, LSPDocument } from '../indexer';
import { SchemaResolver, FieldDef } from '../schemaResolver';
import { AuthoringClient, formatEditDiagnostics, EditResult } from '../authoringClient';
import { BinoPreviewManager } from '../preview';
import { isEmbeddableKind, getEmbeddableKinds } from '../embeddable';
import { getNonce } from '../webviewNonce';
import { WidgetRegistry, WidgetContext, Binding, StandardColumn } from './widgets/registry';
import { enumSelectWidget } from './widgets/enumSelect';
import { scenarioWidget } from './widgets/scenario';
import { varianceWidget } from './widgets/variance';
import { stackWidget } from './widgets/stack';
import { aggregateWidget } from './widgets/aggregate';
import { edgesWidget } from './widgets/edges';
import { getDesignerHtml, FormRow, DesignerDocRef } from './designerHtml';

/** An embeddable component document found in a YAML file. */
export interface ComponentDoc extends DesignerDocRef {
    /** 1-based document ordinal within the file. */
    position: number;
    /** Offset of the document start in the file text. */
    start: number;
    doc: Document;
}

/** One open Designer editor, bound to one TextDocument. */
interface DesignerSession {
    document: vscode.TextDocument;
    panel: vscode.WebviewPanel;
    /** The shown component. Tracked by kind and name, since positions shift. */
    target?: DesignerDocRef;
    binding?: Binding;
    refreshTimer?: ReturnType<typeof setTimeout>;
    suppressForwardSync: boolean;
}

/**
 * The component designer: a schema-driven property form for one embeddable
 * manifest, as a custom text editor on the YAML file. VS Code restores it after
 * a reload, and every write goes through the AuthoringClient as a WorkspaceEdit
 * on the open TextDocument, so undo works on the document. A file with several
 * components shows a picker; custom widgets resolve through the widget registry.
 */
export class DesignerEditorProvider implements vscode.CustomTextEditorProvider {
    static readonly viewType = 'bino.designer';

    private readonly schema: SchemaResolver;
    private readonly authoring: AuthoringClient;
    private readonly registry = new WidgetRegistry();
    private readonly sessions = new Set<DesignerSession>();
    /** Component to show when the editor for a URI opens (set by `open`). */
    private readonly pendingTargets = new Map<string, DesignerDocRef>();

    constructor(
        private readonly indexer: WorkspaceIndexer,
        private readonly preview: BinoPreviewManager,
        extensionPath: string
    ) {
        this.schema = new SchemaResolver(extensionPath);
        this.schema.load();
        this.authoring = new AuthoringClient(indexer);
        // Bespoke IBCS widgets (brief 04) claim their specific fields first; the
        // reference enum-select widget is the generic fallback for any enum field.
        this.registry.register(scenarioWidget);
        this.registry.register(varianceWidget);
        this.registry.register(stackWidget);
        this.registry.register(aggregateWidget);
        this.registry.register(edgesWidget);
        this.registry.register(enumSelectWidget);
    }

    /**
     * Open the designer for an embeddable. With no target, resolves the
     * component at the active editor's cursor, else prompts. Non-embeddable
     * kinds are rejected.
     */
    async open(target?: Pick<LSPDocument, 'file' | 'kind' | 'name'>): Promise<void> {
        const doc = target ?? this.resolveActiveEmbeddable() ?? (await this.pickEmbeddable());
        if (!doc) {
            return; // user cancelled / nothing embeddable
        }
        if (!isEmbeddableKind(doc.kind)) {
            vscode.window.showInformationMessage(`Bino: ${doc.kind} is not an embeddable component.`);
            return;
        }
        const uri = vscode.Uri.file(doc.file);
        const ref = { kind: doc.kind, name: doc.name };
        const session = [...this.sessions].find(s => s.document.uri.toString() === uri.toString());
        if (session) {
            await this.bindTarget(session, ref);
            session.panel.reveal();
            return;
        }
        this.pendingTargets.set(uri.toString(), ref);
        // The embedded preview is its own panel with its own CodeLens; the
        // designer intentionally does NOT open it here (it only refreshes an
        // already-open preview after an edit - see reloadCanvas).
        await vscode.commands.executeCommand('vscode.openWith', uri, DesignerEditorProvider.viewType, vscode.ViewColumn.Beside);
    }

    resolveCustomTextEditor(document: vscode.TextDocument, panel: vscode.WebviewPanel): void {
        const session: DesignerSession = { document, panel, suppressForwardSync: false };
        this.sessions.add(session);
        panel.webview.options = { enableScripts: true };
        panel.webview.html = getDesignerHtml(getNonce());

        const listeners = [
            panel.webview.onDidReceiveMessage(msg => this.handleMessage(session, msg)),
            // Re-render when the document changes, except for our own edits.
            vscode.workspace.onDidChangeTextDocument(e => {
                if (session.suppressForwardSync || e.document.uri.toString() !== document.uri.toString()) { return; }
                if (session.refreshTimer) { clearTimeout(session.refreshTimer); }
                session.refreshTimer = setTimeout(() => { void this.bindTarget(session, session.target); }, 300);
            }),
        ];
        panel.onDidDispose(() => {
            if (session.refreshTimer) { clearTimeout(session.refreshTimer); }
            for (const l of listeners) { l.dispose(); }
            this.sessions.delete(session);
        });
    }

    /**
     * Point the session at a component by kind and name, falling back to the
     * file's first component, and render it.
     */
    private async bindTarget(s: DesignerSession, wanted?: DesignerDocRef): Promise<void> {
        const docs = componentDocs(s.document.getText());
        const chosen = (wanted && docs.find(d => d.kind === wanted.kind && d.name === wanted.name)) ?? docs[0];
        s.target = chosen ? { kind: chosen.kind, name: chosen.name } : undefined;
        void s.panel.webview.postMessage({
            type: 'setDocs',
            docs: docs.map(d => ({ kind: d.kind, name: d.name })),
            current: s.target,
        });
        const dataset = this.readDataset(s);
        if (dataset !== s.binding?.dataset) {
            await this.refreshBinding(s, dataset);
        }
        await this.renderForm(s);
    }

    /** Build the form rows from the live schema and push them to the webview. */
    private async renderForm(s: DesignerSession): Promise<void> {
        if (!s.target) { return; }
        const { kind, name } = s.target;
        const fields = this.schema.getFieldsForKind(kind);
        const spec = this.readSpecMap(s);
        // Plain-JSON view of the spec so a widget can read a sibling field
        // (e.g. variance constrains its slots to spec.scenarios).
        const specJson = (spec ? toJSON(spec) : undefined) as Record<string, unknown> | undefined;
        const hasDataset = this.kindHasDataset(kind);

        const rows: FormRow[] = [];
        for (const field of fields) {
            // The dataset field is surfaced by the dedicated binding control.
            if (field.key === 'dataset' && hasDataset) { continue; }
            rows.push(this.buildRow(kind, field, spec, s.binding, specJson));
        }

        void s.panel.webview.postMessage({
            type: 'setForm',
            kind,
            name,
            bindingHtml: hasDataset ? this.renderBindingControl(s.binding) : '',
            rows,
        });

        // Hand columns to any column-aware widget after the form mounts.
        if (s.binding) {
            void s.panel.webview.postMessage({
                type: 'columns',
                dataset: s.binding.dataset,
                columns: s.binding.columns,
                standardColumns: s.binding.standardColumns,
            });
        }
    }

    /**
     * Render one field: a registry widget if matched, a collapsible group for a
     * nested object (recursing into its scalar children), else a generic control.
     */
    private buildRow(kind: string, field: FieldDef, spec: YAMLMap | undefined, binding: Binding | undefined, specJson?: Record<string, unknown>): FormRow {
        const value = valueAt(spec, field.path);
        const base = {
            // FieldDef.path is spec-relative (resolver roots at the spec def), so
            // the edit patch path is prefixed with `spec`.
            path: ['spec', ...field.path],
            label: field.key,
            description: field.description,
            required: field.required,
            valueType: field.type,
        };

        // A registered widget claims the whole field (top priority).
        const widget = this.registry.resolve(kind, field);
        if (widget) {
            const ctx: WidgetContext = {
                field,
                value,
                binding,
                spec: specJson,
                // onChange is realized by the webview posting editField for the
                // control; the host need not be called directly here.
                onChange: () => undefined,
            };
            return { ...base, controlHtml: widget.render(ctx).html, widget: widget.id };
        }

        // Nested object → expandable group with its scalar children.
        if (field.type === 'object' && field.children && field.children.length > 0) {
            const count = value && typeof value === 'object' ? Object.keys(value as object).length : 0;
            return {
                ...base,
                controlHtml: count > 0 ? `{${count}}` : '(empty)',
                children: field.children.map(child => this.buildRow(kind, child, spec, binding, specJson)),
            };
        }

        return { ...base, controlHtml: this.renderGenericControl(field, value) };
    }

    /** The generic value control inferred from the field's type. */
    private renderGenericControl(field: FieldDef, value: unknown): string {
        const type = field.type;
        if (type === 'boolean') {
            const checked = value === true ? ' checked' : '';
            return `<label class="designer-checkbox"><input type="checkbox" data-designer-edit data-value-type="boolean"${checked}><span>${value ? 'true' : 'false'}</span></label>`;
        }
        if (type === 'object' || type === 'array') {
            // Arrays (and objects with no child schema) are summarized read-only;
            // their rich editing belongs to the IBCS widgets (brief 04).
            const summary = type === 'array'
                ? `[${Array.isArray(value) ? value.length : 0}]`
                : value && typeof value === 'object'
                    ? `{${Object.keys(value as object).length}}`
                    : '(empty)';
            return `<span class="value-summary">${escapeHtml(summary)}</span>`;
        }
        const inputType = type === 'number' || type === 'integer' ? 'number' : 'text';
        const valueStr = value === null || value === undefined ? '' : String(value);
        return `<input class="designer-input" type="${inputType}" data-designer-edit data-value-type="${escapeHtml(type)}" value="${escapeHtml(valueStr)}">`;
    }

    /** The data-binding control: shows the bound dataset and a re-bind action. */
    private renderBindingControl(binding: Binding | undefined): string {
        const ds = binding?.dataset;
        const value = ds
            ? `<span class="binding-value">${escapeHtml(ds)}</span>`
            : `<span class="binding-empty">none</span>`;
        const action = ds ? 'Change' : 'Bind dataset';
        return `<div class="binding-row">
            <span class="binding-label">Data</span>
            ${value}
            <button class="link-btn" data-designer-pick-dataset data-current="${escapeHtml(ds ?? '')}">${action}</button>
        </div>`;
    }

    /** Handle messages from the webview. */
    private async handleMessage(s: DesignerSession, msg: { type?: string;[k: string]: unknown }): Promise<void> {
        switch (msg.type) {
            case 'ready': {
                // The webview (re)loaded: show the requested component, else the
                // one it showed before a reload, else the file's first.
                const key = s.document.uri.toString();
                const wanted = this.pendingTargets.get(key) ?? (msg.target as DesignerDocRef | undefined);
                this.pendingTargets.delete(key);
                s.binding = undefined;
                await this.bindTarget(s, wanted);
                break;
            }
            case 'selectTarget':
                await this.bindTarget(s, msg.target as DesignerDocRef);
                break;
            case 'editField':
                if (msg.remove === true) {
                    await this.handleRemoveField(s, msg.path as string[]);
                } else {
                    await this.handleEditField(s, msg.path as string[], msg.value);
                }
                break;
            case 'pickDataset':
                await this.handlePickDataset(s);
                break;
        }
    }

    /** Apply a field edit through the AuthoringClient, then reload the canvas. */
    private async handleEditField(s: DesignerSession, path: string[], value: unknown): Promise<void> {
        const position = this.findTarget(s)?.position;
        if (position === undefined || !Array.isArray(path) || path.length === 0) { return; }
        const ok = await this.applyEdit(s,
            this.authoring.edit({
                file: s.document.uri.fsPath,
                position,
                patch: { [path.join('.')]: value },
            })
        );
        if (ok) {
            // If the dataset changed, refresh column-dependent widgets.
            if (path.length === 2 && path[0] === 'spec' && path[1] === 'dataset') {
                await this.refreshBinding(s, String(value));
                await this.renderForm(s);
            }
            this.reloadCanvas(s);
        }
    }

    /** Delete a field's key (an emptied control), then reload the canvas. */
    private async handleRemoveField(s: DesignerSession, path: string[]): Promise<void> {
        const position = this.findTarget(s)?.position;
        if (position === undefined || !Array.isArray(path) || path.length === 0) { return; }
        const ok = await this.applyEdit(s,
            this.authoring.remove({
                file: s.document.uri.fsPath,
                position,
                paths: [path.join('.')],
            })
        );
        if (ok) { this.reloadCanvas(s); }
    }

    /** Prompt for a dataset and bind it (writes spec.dataset, refreshes columns). */
    private async handlePickDataset(s: DesignerSession): Promise<void> {
        if (!s.target) { return; }
        const items = this.indexer.getDatasetCompletions().map(name => ({ label: name }));
        if (items.length === 0) {
            vscode.window.showInformationMessage('Bino: no datasets or datasources in the workspace.');
            return;
        }
        const picked = await vscode.window.showQuickPick(items, {
            placeHolder: 'Bind a dataset/datasource',
            title: 'Bino Designer: Data Binding',
        });
        if (!picked) { return; }
        await this.handleEditField(s, ['spec', 'dataset'], picked.label);
    }

    /**
     * Apply an authoring mutation and surface a rejection. Returns true on
     * success so the caller can reload the canvas only on a real write.
     * The custom editor keeps the TextDocument open, so the AuthoringClient
     * merges the edit as a WorkspaceEdit (one undo step, firing onDidChange).
     * We hold the forward-sync guard across the apply to keep the designer's own
     * edits from re-rendering the form (and stealing focus); external edits still
     * refresh. The guard clears after a short delay to let the edit settle.
     */
    private async applyEdit(s: DesignerSession, pending: Promise<EditResult>): Promise<boolean> {
        s.suppressForwardSync = true;
        try {
            const result = await pending;
            if (!result.ok) {
                vscode.window.showErrorMessage(`Edit rejected: ${formatEditDiagnostics(result)}`);
                return false;
            }
            return true;
        } finally {
            setTimeout(() => { s.suppressForwardSync = false; }, 100);
        }
    }

    /**
     * Ask the embedded preview to re-show the current component. The canvas also
     * auto-reloads on the SSE refresh tick; this keeps it pinned to our target.
     */
    private reloadCanvas(s: DesignerSession): void {
        // Never open the preview from the designer; only refresh it if the user
        // already opened the embedded preview separately (its own CodeLens).
        const target = this.findTarget(s);
        if (!target || !this.preview.isEmbeddedPreviewOpen()) { return; }
        void this.preview.previewArtefactEmbedded({
            kind: target.kind,
            name: target.name,
            file: s.document.uri.fsPath,
            position: target.position,
        });
    }

    /** Fetch live columns + the canonical standard columns for the binding. */
    private async refreshBinding(s: DesignerSession, dataset: string | undefined): Promise<void> {
        if (!dataset) { s.binding = undefined; return; }
        const [columns, standard] = await Promise.all([
            this.indexer.getColumns(dataset),
            this.indexer.datasetSchema(),
        ]);
        s.binding = {
            dataset,
            columns,
            standardColumns: standard as StandardColumn[],
        };
    }

    /** True when the kind exposes a `spec.dataset` field. */
    private kindHasDataset(kind: string): boolean {
        return this.schema.getFieldsForKind(kind).some(f => f.key === 'dataset');
    }

    /** Read the current `spec.dataset` value from the bound document, if any. */
    private readDataset(s: DesignerSession): string | undefined {
        const spec = this.readSpecMap(s);
        if (!spec) { return undefined; }
        const ds = spec.get('dataset');
        return typeof ds === 'string' && ds ? ds : undefined;
    }

    /** The shown component's document in the current text, found by kind and name. */
    private findTarget(s: DesignerSession): ComponentDoc | undefined {
        const target = s.target;
        if (!target) { return undefined; }
        return componentDocs(s.document.getText()).find(d => d.kind === target.kind && d.name === target.name);
    }

    /** Resolve the bound document's `spec` mapping from a read-only YAML parse. */
    private readSpecMap(s: DesignerSession): YAMLMap | undefined {
        const contents = this.findTarget(s)?.doc.contents;
        if (!isMap(contents)) { return undefined; }
        const spec = (contents as YAMLMap).get('spec');
        return isMap(spec) ? (spec as YAMLMap) : undefined;
    }

    /** Resolve the embeddable at the active editor's cursor (closest doc start). */
    private resolveActiveEmbeddable(): Pick<LSPDocument, 'file' | 'kind' | 'name'> | undefined {
        const editor = vscode.window.activeTextEditor;
        if (!editor) { return undefined; }
        const cursor = editor.document.offsetAt(editor.selection.active);
        const docs = componentDocs(editor.document.getText());
        if (docs.length === 0) { return undefined; }
        let chosen = docs[0];
        for (const d of docs) {
            if (d.start <= cursor) { chosen = d; }
        }
        return { file: editor.document.uri.fsPath, kind: chosen.kind, name: chosen.name };
    }

    /** Prompt the user to choose an embeddable component. */
    private async pickEmbeddable(): Promise<LSPDocument | undefined> {
        const docs = this.indexer.getDocuments(getEmbeddableKinds());
        if (docs.length === 0) {
            vscode.window.showInformationMessage('Bino: no embeddable components in the workspace.');
            return undefined;
        }
        const items = docs.map(d => ({ label: d.name, description: d.kind, doc: d }));
        const picked = await vscode.window.showQuickPick(items, {
            placeHolder: 'Open a component in the designer',
            title: 'Bino Designer',
        });
        return picked?.doc;
    }

    dispose(): void {
        for (const s of this.sessions) { s.panel.dispose(); }
    }
}

/** The embeddable component documents of a YAML file, in file order. */
export function componentDocs(text: string): ComponentDoc[] {
    let parsed: Document[];
    try {
        parsed = parseAllDocuments(text) as Document[];
    } catch {
        return [];
    }
    const out: ComponentDoc[] = [];
    parsed.forEach((doc, i) => {
        if (!isMap(doc.contents)) { return; }
        const contents = doc.contents as YAMLMap;
        const kind = contents.get('kind');
        const metadata = contents.get('metadata');
        const name = isMap(metadata) ? (metadata as YAMLMap).get('name') : undefined;
        if (typeof kind === 'string' && typeof name === 'string' && isEmbeddableKind(kind)) {
            out.push({ kind, name, position: i + 1, start: doc.range?.[0] ?? 0, doc });
        }
    });
    return out;
}

/** Minimal HTML escaper for host-built control fragments. */
function escapeHtml(str: string): string {
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

/** Convert a yaml AST value node to a plain JSON value (best-effort). */
function toJSON(node: unknown): unknown {
    if (node && typeof (node as { toJSON?: unknown }).toJSON === 'function') {
        return (node as { toJSON: () => unknown }).toJSON();
    }
    if (isScalar(node)) { return (node as Scalar).value; }
    return node;
}

/**
 * Resolve a spec-relative field path to its current JSON value within the spec
 * map. Array-item segments ('[]') are never resolved here (arrays render as a
 * read-only summary), so such paths yield undefined. An explicit YAML `null` is
 * preserved as `null` (distinct from an absent key → `undefined`) so a widget can
 * tell `columnthereof: null` from an unset field.
 */
function valueAt(spec: YAMLMap | undefined, relPath: string[]): unknown {
    if (!spec || relPath.length === 0 || relPath.includes('[]')) { return undefined; }
    const node = spec.getIn(relPath, true);
    return node === undefined ? undefined : toJSON(node);
}
