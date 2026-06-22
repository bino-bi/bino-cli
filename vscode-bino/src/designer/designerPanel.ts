import * as vscode from 'vscode';
import * as fs from 'fs';
import { parseAllDocuments, isMap, isScalar, Scalar, YAMLMap } from 'yaml';
import { WorkspaceIndexer, LSPDocument } from '../indexer';
import { SchemaResolver, FieldDef } from '../schemaResolver';
import { AuthoringClient, formatEditDiagnostics, EditResult } from '../authoringClient';
import { BinoPreviewManager } from '../preview';
import { isEmbeddableKind, getEmbeddableKinds } from '../embeddable';
import { WidgetRegistry, WidgetContext, Binding, StandardColumn } from './widgets/registry';
import { enumSelectWidget } from './widgets/enumSelect';
import { scenarioWidget } from './widgets/scenario';
import { varianceWidget } from './widgets/variance';
import { stackWidget } from './widgets/stack';
import { aggregateWidget } from './widgets/aggregate';
import { edgesWidget } from './widgets/edges';
import { getDesignerHtml, FormRow } from './designerHtml';

/** The component currently shown in the designer. */
interface DesignerTarget {
    file: string;
    /** 1-based document ordinal within the file. */
    docIndex: number;
    kind: string;
    name: string;
}

/**
 * The component designer: a schema-driven property panel for one embeddable
 * manifest with a live canvas. It generalizes the tree-table editor's webview
 * infra (singleton panel, lifecycle/guards, message plumbing) but renders a form
 * — generic controls per `FieldDef`, with custom widgets resolved through the
 * widget registry — instead of a raw property grid. Edits write through the
 * brief-01 AuthoringClient and the canvas re-renders on the next SSE tick.
 */
export class DesignerPanel {
    private panel: vscode.WebviewPanel | undefined;
    private readonly schema: SchemaResolver;
    private readonly authoring: AuthoringClient;
    private readonly registry = new WidgetRegistry();
    private target: DesignerTarget | undefined;
    private binding: Binding | undefined;
    private disposables: vscode.Disposable[] = [];
    private refreshTimer: ReturnType<typeof setTimeout> | undefined;
    private suppressForwardSync = false;

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
     * Open or reveal the designer for an embeddable. With no target, resolves the
     * artefact at the active editor's cursor, else prompts. Non-embeddable kinds
     * are rejected.
     */
    async open(target?: LSPDocument): Promise<void> {
        const doc = target ?? this.resolveActiveEmbeddable() ?? (await this.pickEmbeddable());
        if (!doc) {
            return; // user cancelled / nothing embeddable
        }
        if (!isEmbeddableKind(doc.kind)) {
            vscode.window.showInformationMessage(`Bino: ${doc.kind} is not an embeddable component.`);
            return;
        }

        this.ensurePanel();
        await this.bindTarget(doc);

        // Embed the live canvas next to the form (reuses the preview seam).
        await this.preview.previewArtefactEmbedded({
            kind: doc.kind,
            name: doc.name,
            file: doc.file,
            position: doc.position,
        });

        this.panel!.reveal(vscode.ViewColumn.Beside);
    }

    /** Create the singleton webview panel and wire its lifecycle. */
    private ensurePanel(): void {
        if (this.panel) {
            return;
        }
        this.panel = vscode.window.createWebviewPanel(
            'binoDesigner',
            'Bino Designer',
            vscode.ViewColumn.Beside,
            { enableScripts: true, retainContextWhenHidden: true }
        );
        this.panel.webview.html = getDesignerHtml();
        this.panel.webview.onDidReceiveMessage(msg => this.handleMessage(msg));
        this.panel.onDidDispose(() => {
            this.panel = undefined;
            this.target = undefined;
            this.binding = undefined;
            this.disposeListeners();
        });
        this.setupListeners();
    }

    /** Re-render the form when the bound file changes on disk/in the buffer. */
    private setupListeners(): void {
        this.disposables.push(
            vscode.workspace.onDidChangeTextDocument(e => {
                if (this.suppressForwardSync) { return; }
                if (this.target && e.document.uri.fsPath === this.target.file) {
                    this.debouncedRenderForm();
                }
            })
        );
    }

    /** Debounce form re-renders so direct typing in the YAML doesn't thrash. */
    private debouncedRenderForm(): void {
        if (this.refreshTimer) { clearTimeout(this.refreshTimer); }
        this.refreshTimer = setTimeout(() => { void this.renderForm(); }, 300);
    }

    private disposeListeners(): void {
        for (const d of this.disposables) { d.dispose(); }
        this.disposables = [];
    }

    /** Switch the designer to a component and (re)derive its binding. */
    private async bindTarget(doc: LSPDocument): Promise<void> {
        this.target = { file: doc.file, docIndex: doc.position, kind: doc.kind, name: doc.name };
        this.binding = undefined;
        const dataset = this.readDataset();
        if (dataset) {
            await this.refreshBinding(dataset);
        }
        await this.renderForm();
        if (this.panel?.title !== undefined) {
            this.panel.title = `Designer: ${doc.name}`;
        }
    }

    /** Build the form rows from the live schema and push them to the webview. */
    private async renderForm(): Promise<void> {
        if (!this.panel || !this.target) { return; }
        const { kind, name } = this.target;
        const fields = this.schema.getFieldsForKind(kind);
        const spec = this.readSpecMap();
        // Plain-JSON view of the spec so a widget can read a sibling field
        // (e.g. variance constrains its slots to spec.scenarios).
        const specJson = (spec ? toJSON(spec) : undefined) as Record<string, unknown> | undefined;

        const rows: FormRow[] = [];
        for (const field of fields) {
            // The dataset field is surfaced by the dedicated binding control.
            if (field.key === 'dataset' && this.kindHasDataset()) { continue; }
            rows.push(this.buildRow(kind, field, spec, specJson));
        }

        this.panel.webview.postMessage({
            type: 'setForm',
            kind,
            name,
            bindingHtml: this.kindHasDataset() ? this.renderBindingControl() : '',
            rows,
        });

        // Hand columns to any column-aware widget after the form mounts.
        if (this.binding) {
            this.panel.webview.postMessage({
                type: 'columns',
                dataset: this.binding.dataset,
                columns: this.binding.columns,
                standardColumns: this.binding.standardColumns,
            });
        }
    }

    /**
     * Render one field: a registry widget if matched, a collapsible group for a
     * nested object (recursing into its scalar children), else a generic control.
     */
    private buildRow(kind: string, field: FieldDef, spec: YAMLMap | undefined, specJson?: Record<string, unknown>): FormRow {
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
                binding: this.binding,
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
                children: field.children.map(child => this.buildRow(kind, child, spec, specJson)),
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
    private renderBindingControl(): string {
        const ds = this.binding?.dataset;
        const value = ds
            ? `<span class="binding-value">${escapeHtml(ds)}</span>`
            : `<span class="binding-empty">none</span>`;
        const action = ds ? 'Change' : 'Bind dataset';
        return `<div class="binding-row">
            <span class="binding-label">Data</span>
            ${value}
            <button class="link-btn" onclick="pickDataset(${ds ? JSON.stringify(JSON.stringify(ds)) : 'undefined'})">${action}</button>
        </div>`;
    }

    /** Handle messages from the webview. */
    private async handleMessage(msg: { type?: string;[k: string]: unknown }): Promise<void> {
        switch (msg.type) {
            case 'editField':
                await this.handleEditField(msg.path as string[], msg.value);
                break;
            case 'pickDataset':
                await this.handlePickDataset();
                break;
        }
    }

    /** Apply a field edit through the AuthoringClient, then reload the canvas. */
    private async handleEditField(path: string[], value: unknown): Promise<void> {
        if (!this.target || !Array.isArray(path) || path.length === 0) { return; }
        const ok = await this.applyEdit(
            this.authoring.edit({
                file: this.target.file,
                position: this.target.docIndex,
                patch: { [path.join('.')]: value },
            })
        );
        if (ok) {
            // If the dataset changed, refresh column-dependent widgets.
            if (path.length === 2 && path[0] === 'spec' && path[1] === 'dataset') {
                await this.refreshBinding(String(value));
                await this.renderForm();
            }
            this.reloadCanvas();
        }
    }

    /** Prompt for a dataset and bind it (writes spec.dataset, refreshes columns). */
    private async handlePickDataset(): Promise<void> {
        if (!this.target) { return; }
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
        await this.handleEditField(['spec', 'dataset'], picked.label);
    }

    /**
     * Apply an authoring mutation and surface a rejection. Returns true on
     * success so the caller can reload the canvas only on a real write.
     * The AuthoringClient merges it via a WorkspaceEdit (firing onDidChange), so
     * we hold the forward-sync guard across the apply to keep the designer's own
     * edits from re-rendering the form (and stealing focus); external edits still
     * refresh. The guard clears after a short delay to let the edit settle.
     */
    private async applyEdit(pending: Promise<EditResult>): Promise<boolean> {
        this.suppressForwardSync = true;
        try {
            const result = await pending;
            if (!result.ok) {
                vscode.window.showErrorMessage(`Edit rejected: ${formatEditDiagnostics(result)}`);
                return false;
            }
            return true;
        } finally {
            setTimeout(() => { this.suppressForwardSync = false; }, 100);
        }
    }

    /**
     * Ask the embedded preview to re-show the current component. The canvas also
     * auto-reloads on the SSE refresh tick; this keeps it pinned to our target.
     */
    private reloadCanvas(): void {
        if (!this.target) { return; }
        void this.preview.previewArtefactEmbedded({
            kind: this.target.kind,
            name: this.target.name,
            file: this.target.file,
            position: this.target.docIndex,
        });
    }

    /** Fetch live columns + the canonical standard columns for the binding. */
    private async refreshBinding(dataset: string): Promise<void> {
        if (!dataset) { this.binding = undefined; return; }
        const [columns, standard] = await Promise.all([
            this.indexer.getColumns(dataset),
            this.indexer.datasetSchema(),
        ]);
        this.binding = {
            dataset,
            columns,
            standardColumns: standard as StandardColumn[],
        };
    }

    /** True when the bound kind exposes a `spec.dataset` field. */
    private kindHasDataset(): boolean {
        if (!this.target) { return false; }
        return this.schema.getFieldsForKind(this.target.kind).some(f => f.key === 'dataset');
    }

    /** Read the current `spec.dataset` value from the bound document, if any. */
    private readDataset(): string | undefined {
        const spec = this.readSpecMap();
        if (!spec) { return undefined; }
        const ds = spec.get('dataset');
        return typeof ds === 'string' && ds ? ds : undefined;
    }

    /** Resolve the bound document's `spec` mapping from a read-only YAML parse. */
    private readSpecMap(): YAMLMap | undefined {
        if (!this.target) { return undefined; }
        const text = this.currentText(this.target.file);
        if (text === undefined) { return undefined; }
        let docs;
        try {
            docs = parseAllDocuments(text);
        } catch {
            return undefined;
        }
        const doc = docs[this.target.docIndex - 1];
        if (!doc || !isMap(doc.contents)) { return undefined; }
        const spec = (doc.contents as YAMLMap).get('spec');
        return isMap(spec) ? (spec as YAMLMap) : undefined;
    }

    /** The live buffer text if the file is open, else its on-disk text. */
    private currentText(file: string): string | undefined {
        const open = vscode.workspace.textDocuments.find(d => d.uri.fsPath === file);
        if (open) { return open.getText(); }
        try {
            return fs.readFileSync(file, 'utf8');
        } catch {
            return undefined;
        }
    }

    /** Resolve the embeddable at the active editor's cursor (closest doc start). */
    private resolveActiveEmbeddable(): LSPDocument | undefined {
        const editor = vscode.window.activeTextEditor;
        if (!editor) { return undefined; }
        const filePath = editor.document.uri.fsPath.replace(/\\/g, '/');
        const cursorLine = editor.selection.active.line;
        const candidates = this.indexer
            .getDocuments(getEmbeddableKinds())
            .filter(d => d.file.replace(/\\/g, '/') === filePath);
        if (candidates.length === 0) { return undefined; }

        const text = editor.document.getText();
        const withLines = candidates.map(d => ({ d, startLine: documentStartLine(text, d.position) }));
        let chosen = withLines[0];
        for (const c of withLines) {
            if (c.startLine <= cursorLine && c.startLine >= chosen.startLine) {
                chosen = c;
            }
        }
        return chosen.d;
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
        if (this.refreshTimer) { clearTimeout(this.refreshTimer); }
        this.disposeListeners();
        this.panel?.dispose();
    }
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

/** 0-based start line of the 1-based Nth document in a multi-doc YAML string. */
function documentStartLine(text: string, docIndex: number): number {
    const lines = text.split('\n');
    let current = 0;
    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        const line = lines[lineNum].trim();
        if (lineNum === 0) {
            if (line === '---') { continue; }
            if (line && !line.startsWith('#')) {
                current = 1;
                if (current === docIndex) { return 0; }
            }
        } else if (line === '---') {
            current++;
            if (current === docIndex) { return lineNum + 1; }
        } else if (current === 0 && line && !line.startsWith('#')) {
            current = 1;
            if (current === docIndex) { return lineNum; }
        }
    }
    return 0;
}
