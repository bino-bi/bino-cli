import * as path from 'path';
import * as vscode from 'vscode';
import { WorkspaceIndexer } from '../indexer';
import { AuthoringClient, formatEditDiagnostics } from '../authoringClient';
import { getNonce } from '../webviewNonce';
import { readStyleContent } from '../styles/styleEditor';
import { DEFAULT_STYLE, getIn, isObject, SYSTEM_STYLE } from '../styles/styleModel';
import {
    cleanSpec, distinctValues, fixedValues, isScenarioColumn, ManifestJson, MARK_FIELDS, MARKS, pointFieldsFor,
    smallMultipleInner, specChanges, TARGET_KINDS, TO_REQUIRED,
} from './annotationModel';
import { AnnotationEntry, annotationEntries, loadManifests, LoadedManifest } from './annotationsView';
import { getAnnotationFormHtml } from './annotationFormHtml';

/** The most values a point field offers; the input filters them as you type. */
const VALUE_CAP = 500;
/** Rows read to collect the values of a point field. */
const ROW_LIMIT = 5000;

/** One end of an annotation (target or to) as the form fills it. */
export interface FormTarget {
    kind?: string;
    name?: string;
    panel?: string;
    point?: Record<string, string>;
}

/** Values the form starts with, e.g. from a click in the preview. */
export interface FormPrefill {
    target?: FormTarget;
    to?: FormTarget;
}

/** A component a target can name. */
interface TargetComponent {
    kind: string;
    name: string;
    /** The kind of the panels, for a SmallMultiple. */
    inner?: string;
    /** File of the manifest that defines it: a new annotation goes there. */
    file: string;
    spec?: Record<string, unknown>;
}

/**
 * The annotation form: one webview panel that creates a new Annotation or
 * edits one. The fields follow the mark, and the target picker offers the
 * components of the project and the raw values of their data.
 */
export class AnnotationFormPanel {
    private panel?: vscode.WebviewPanel;
    private entry?: AnnotationEntry;
    private components: TargetComponent[] = [];
    private readonly authoring: AuthoringClient;
    /** The last state sent: a page that loads later asks for it with 'ready'. */
    private state?: Record<string, unknown>;
    /** Messages run one after the other, so a save never races another write. */
    private queue: Promise<void> = Promise.resolve();
    /** Whether a preview can deliver picked points; set by the preview. */
    private canPick = false;
    /** Called with the end the form waits for, or undefined when it waits for none. */
    onWaitingForPick?: (end: 'target' | 'to' | undefined) => void;

    constructor(private readonly indexer: WorkspaceIndexer, private readonly onSaved: () => void) {
        this.authoring = new AuthoringClient(indexer);
    }

    /** Open the form for an annotation, or for a new one. */
    async open(entry?: AnnotationEntry, prefill?: FormPrefill): Promise<void> {
        const manifests = await loadManifests(this.indexer);
        this.components = targetComponents(manifests);
        this.entry = entry;
        if (!this.panel) {
            this.panel = vscode.window.createWebviewPanel('binoAnnotationForm', 'Annotation', vscode.ViewColumn.Beside, { enableScripts: true, retainContextWhenHidden: true });
            this.panel.webview.html = getAnnotationFormHtml(getNonce());
            this.panel.webview.onDidReceiveMessage(msg => {
                this.queue = this.queue.then(() => this.handle(msg)).catch(err => {
                    vscode.window.showErrorMessage(`Bino: ${err instanceof Error ? err.message : String(err)}`);
                });
            });
            this.panel.onDidDispose(() => { this.panel = undefined; this.onWaitingForPick?.(undefined); });
        }
        this.panel.title = entry ? `Annotation: ${entry.name}` : 'New Annotation';
        this.panel.reveal(vscode.ViewColumn.Beside, true);
        const spec: Record<string, unknown> = entry ? { ...entry.spec } : {};
        if (prefill?.target) { spec.target = prefill.target; }
        if (prefill?.to) { spec.to = prefill.to; }
        this.state = {
            type: 'state',
            mode: entry ? 'edit' : 'create',
            name: entry?.name ?? suggestName(manifests, spec.target as FormTarget | undefined),
            spec,
            marks: MARKS,
            markFields: MARK_FIELDS,
            toRequired: TO_REQUIRED,
            components: this.components.map(c => ({ kind: c.kind, name: c.name, inner: c.inner, fields: pointFieldsFor(c.kind, c.inner) })),
            targetKinds: TARGET_KINDS,
            colors: await this.colorNames(),
            canPick: this.canPick,
        };
        void this.panel.webview.postMessage(this.state);
    }

    /** Show or hide the "Pick in preview" buttons. */
    setCanPick(on: boolean): void {
        this.canPick = on;
        void this.panel?.webview.postMessage({ type: 'canPick', on });
    }

    /** Whether the form is open. */
    get isOpen(): boolean {
        return this.panel !== undefined;
    }

    /** A point picked in the preview: fills the end the form waits for. */
    pick(end: 'target' | 'to', target: FormTarget): void {
        void this.panel?.webview.postMessage({ type: 'picked', end, target });
    }

    dispose(): void {
        this.panel?.dispose();
    }

    private async handle(msg: { type?: string; [k: string]: unknown }): Promise<void> {
        switch (msg.type) {
            case 'ready':
                if (this.state) { void this.panel?.webview.postMessage({ ...this.state, canPick: this.canPick }); }
                return;
            case 'values': {
                const result = await this.valuesFor(String(msg.kind), String(msg.name), String(msg.field));
                void this.panel?.webview.postMessage({ type: 'values', key: msg.key, ...result });
                return;
            }
            case 'save':
                await this.save(String(msg.name ?? ''), (msg.spec ?? {}) as Record<string, unknown>);
                return;
            case 'reveal':
                if (this.entry) { await vscode.commands.executeCommand('bino.revealAnnotation', this.entry); }
                return;
            case 'waiting':
                this.onWaitingForPick?.(msg.end === 'target' || msg.end === 'to' ? msg.end : undefined);
                return;
        }
    }

    /** The colour names of the `bn-annotation` style: the engine's and those of `_default`. */
    private async colorNames(): Promise<string[]> {
        const own = getIn(await readStyleContent(this.indexer, DEFAULT_STYLE), ['bn-annotation', 'colors']);
        const system = getIn(SYSTEM_STYLE, ['bn-annotation', 'colors']);
        return [...new Set([...Object.keys(isObject(system) ? system : {}), ...Object.keys(isObject(own) ? own : {})])].sort();
    }

    /** The values a point field of a component can take, from its data. */
    private async valuesFor(kind: string, name: string, field: string): Promise<{ values: string[]; truncated?: boolean; error?: string }> {
        const fixed = fixedValues(field, kind);
        if (fixed) { return { values: fixed }; }
        const component = this.components.find(c => c.kind === kind && c.name === name);
        if (!component) { return { values: [], error: `${kind} ${name} is not in the project.` }; }
        if (field === 'scenario') {
            const listed = component.spec?.scenarios;
            if (Array.isArray(listed) && listed.length) { return { values: listed.map(String) }; }
        }
        const dataset = datasetOf(component.spec);
        if (!dataset) { return { values: [], error: `${kind} ${name} has no dataset.` }; }
        const rows = await this.indexer.getRows(dataset, ROW_LIMIT);
        if (!rows || rows.error) { return { values: [], error: rows?.error ?? `No rows for ${dataset}.` }; }
        if (field === 'scenario') { return { values: rows.columns.filter(isScenarioColumn) }; }
        const found = distinctValues(rows.rows, field, VALUE_CAP);
        return { values: found.values, truncated: found.truncated || rows.truncated };
    }

    /** Create the annotation, or write the changes to the open one. Public for tests: the page calls it with 'save'. */
    async save(name: string, raw: Record<string, unknown>): Promise<string | undefined> {
        const fail = (message: string) => { vscode.window.showErrorMessage(message); return message; };
        const spec = cleanSpec(raw);
        if (!spec.target) {
            return fail('Bino: an annotation needs a target component.');
        }
        if (this.entry) {
            const { patch, remove } = specChanges(this.entry.spec, spec);
            const file = this.entry.file;
            const position = this.entry.position;
            if (Object.keys(patch).length) {
                const result = await this.authoring.edit({ file, position, patch });
                if (!result.ok) { return fail(`Edit rejected: ${formatEditDiagnostics(result)}`); }
            }
            if (remove.length) {
                const result = await this.authoring.remove({ file, position, paths: remove });
                if (!result.ok) { return fail(`Edit rejected: ${formatEditDiagnostics(result)}`); }
            }
            this.entry = { ...this.entry, spec };
            this.onSaved();
            vscode.window.setStatusBarMessage(`Bino: saved annotation ${this.entry.name}`, 3000);
            return undefined;
        }
        if (!/^[A-Za-z0-9_][A-Za-z0-9_.-]*$/.test(name)) {
            return fail('Bino: enter a name for the annotation (letters, digits, _ . -).');
        }
        const target = spec.target as { kind: string; name: string };
        const home = this.components.find(c => c.kind === target.kind && c.name === target.name)?.file;
        const root = this.indexer.getProjectRootForUri(home ? vscode.Uri.file(home) : undefined);
        const result = await this.authoring.create({
            kind: 'Annotation', name, spec,
            ...(home && root ? { file: path.relative(root, home) } : {}),
        });
        if (!result.ok) {
            return fail(`Bino: annotation not created: ${formatEditDiagnostics(result)}`);
        }
        this.onSaved();
        // Edit it from now on: a second save must not create a twin.
        const manifests = await loadManifests(this.indexer);
        const created = annotationEntries(manifests).find(e => e.name === name);
        if (created) { await this.open(created); }
        vscode.window.setStatusBarMessage(`Bino: created annotation ${name} in ${vscode.workspace.asRelativePath(result.file)}`, 4000);
        return undefined;
    }
}

/** Every component a target can name: top-level manifests and named children of pages, cards and grids. */
export function targetComponents(manifests: LoadedManifest[]): TargetComponent[] {
    const out: TargetComponent[] = [];
    const add = (json: ManifestJson, file: string) => {
        if (typeof json.kind !== 'string' || typeof json.metadata?.name !== 'string') { return; }
        if (TARGET_KINDS.includes(json.kind) && !out.some(c => c.kind === json.kind && c.name === json.metadata!.name)) {
            out.push({ kind: json.kind, name: json.metadata.name as string, file, spec: json.spec, inner: json.kind === 'SmallMultiple' ? smallMultipleInner(json.spec) : undefined });
        }
    };
    const walk = (spec: Record<string, unknown> | undefined, file: string) => {
        const children = spec?.children;
        if (!Array.isArray(children)) { return; }
        for (const child of children) {
            if (!child || typeof child !== 'object') { continue; }
            const c = child as ManifestJson & { ref?: unknown };
            if (typeof c.ref === 'string') { continue; }
            add(c, file);
            walk(c.spec, file);
        }
    };
    for (const m of manifests) {
        add(m.json, m.file);
        walk(m.json.spec, m.file);
    }
    return out.sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));
}

/** The dataset name a component reads, without the `$` of a DataSource. */
function datasetOf(spec: Record<string, unknown> | undefined): string | undefined {
    const raw = spec?.dataset;
    const first = Array.isArray(raw) ? raw[0] : raw;
    return typeof first === 'string' && first ? first.replace(/^\$/, '') : undefined;
}

/** A free name for a new annotation: `<target>_note`, numbered when taken. */
function suggestName(manifests: LoadedManifest[], target: FormTarget | undefined): string {
    const taken = new Set(annotationEntries(manifests).map(e => e.name));
    const base = `${target?.name ?? 'annotation'}_note`;
    let name = base;
    for (let i = 2; taken.has(name); i++) { name = `${base}_${i}`; }
    return name;
}
