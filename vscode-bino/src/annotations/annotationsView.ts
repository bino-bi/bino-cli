import * as vscode from 'vscode';
import { parseAllDocuments } from 'yaml';
import { WorkspaceIndexer } from '../indexer';
import { collectPages, ManifestJson } from './annotationModel';

/** An Annotation manifest with where it is and what it points at. */
export interface AnnotationEntry {
    name: string;
    file: string;
    /** 1-based document ordinal within the file. */
    position: number;
    /** 0-based lines of the document in its file. */
    startLine: number;
    endLine: number;
    spec: Record<string, unknown>;
    target?: { kind: string; name: string; panel?: string };
}

/** A manifest of the project, parsed, with its place in the file. */
export interface LoadedManifest {
    json: ManifestJson;
    file: string;
    position: number;
    startLine: number;
    endLine: number;
}

/** Lint rules about annotations whose findings the view shows on an annotation. */
const ANNOTATION_RULE = /^annotation-/;

/**
 * Every manifest of the indexed files, read from open buffers first. While
 * the index is empty (still loading, or no language server) the YAML files of
 * the project root are read instead.
 */
export async function loadManifests(indexer: WorkspaceIndexer, alsoFiles: string[] = []): Promise<LoadedManifest[]> {
    let files = [...new Set(indexer.getDocuments().map(d => d.file))];
    if (files.length === 0) {
        const root = indexer.getProjectRootForUri();
        if (root) {
            const found = await vscode.workspace.findFiles(new vscode.RelativePattern(root, '**/*.{yaml,yml}'), '{**/node_modules/**,**/.bino/**,**/dist/**}', 2000);
            files = found.map(u => u.fsPath);
        }
    }
    // files written a moment ago are not in the index yet
    files = [...new Set([...files, ...alsoFiles])];
    const out: LoadedManifest[] = [];
    for (const file of files) {
        const uri = vscode.Uri.file(file);
        // a clean buffer can lag behind a write to disk that just happened
        const open = vscode.workspace.textDocuments.find(d => d.uri.fsPath === uri.fsPath && d.isDirty);
        let text: string;
        try {
            text = open ? open.getText() : Buffer.from(await vscode.workspace.fs.readFile(uri)).toString('utf8');
        } catch {
            continue;
        }
        let docs;
        try {
            docs = parseAllDocuments(text);
        } catch {
            continue;
        }
        if (!Array.isArray(docs)) { continue; }
        docs.forEach((doc, i) => {
            const json = doc.toJSON() as ManifestJson | null;
            if (!json || typeof json !== 'object') { return; }
            const range = doc.range ?? [0, 0, text.length];
            out.push({ json, file, position: i + 1, startLine: lineOf(text, range[0]), endLine: lineOf(text, range[2]) });
        });
    }
    return out;
}

function lineOf(text: string, offset: number): number {
    let line = 0;
    for (let i = 0; i < offset && i < text.length; i++) { if (text.charCodeAt(i) === 10) { line++; } }
    return line;
}

/** The Annotation manifests among loaded manifests. */
export function annotationEntries(manifests: LoadedManifest[]): AnnotationEntry[] {
    return manifests
        .filter(m => m.json.kind === 'Annotation' && typeof m.json.metadata?.name === 'string')
        .map(m => {
            const spec = (m.json.spec ?? {}) as Record<string, unknown>;
            const t = spec.target as { kind?: unknown; name?: unknown; panel?: unknown } | undefined;
            const target = typeof t?.kind === 'string' && typeof t?.name === 'string'
                ? { kind: t.kind, name: t.name, panel: typeof t.panel === 'string' ? t.panel : undefined }
                : undefined;
            return { name: m.json.metadata!.name as string, file: m.file, position: m.position, startLine: m.startLine, endLine: m.endLine, spec, target };
        });
}

type Node =
    | { type: 'artefact'; name: string; children: Node[] }
    | { type: 'page'; name: string; children: Node[] }
    | { type: 'component'; kind: string; name: string; children: Node[] }
    | { type: 'annotation'; entry: AnnotationEntry; problems: vscode.Diagnostic[]; label?: AnnotationLabel }
    | { type: 'info'; text: string };

/** The number of an annotation on one page, from a rendered preview. */
export interface AnnotationLabel {
    label?: string;
    placed: boolean;
}

/** The tree of the Annotations view, built from the manifests. Exported for tests. */
export function buildAnnotationTree(manifests: LoadedManifest[], diagnosticsFor: (entry: AnnotationEntry) => vscode.Diagnostic[], labels?: Map<string, AnnotationLabel>): Node[] {
    const entries = annotationEntries(manifests);
    if (entries.length === 0) { return [{ type: 'info', text: 'No Annotation in the project.' }]; }
    const pages = collectPages(manifests.map(m => m.json));
    const annotationNode = (entry: AnnotationEntry): Node => ({ type: 'annotation', entry, problems: diagnosticsFor(entry), label: labels?.get(entry.name) });
    const placed = new Set<AnnotationEntry>();

    const pageNode = (page: { name: string; components: { kind: string; name: string }[] }): Node | undefined => {
        const components: Node[] = [];
        const seen = new Set<string>();
        for (const c of page.components) {
            const key = `${c.kind}:${c.name}`;
            if (seen.has(key)) { continue; }
            seen.add(key);
            const notes = entries.filter(e => e.target?.kind === c.kind && e.target.name === c.name);
            if (notes.length === 0) { continue; }
            notes.forEach(n => placed.add(n));
            components.push({ type: 'component', kind: c.kind, name: c.name, children: notes.map(annotationNode) });
        }
        return components.length ? { type: 'page', name: page.name, children: components } : undefined;
    };

    const roots: Node[] = [];
    const artefacts = [...new Set(pages.flatMap(p => p.artefacts))].sort();
    for (const artefact of artefacts) {
        const children = pages.filter(p => p.artefacts.includes(artefact)).map(pageNode).filter((n): n is Node => !!n);
        if (children.length) { roots.push({ type: 'artefact', name: artefact, children }); }
    }
    const loose = pages.filter(p => p.artefacts.length === 0).map(pageNode).filter((n): n is Node => !!n);
    if (loose.length) { roots.push({ type: 'artefact', name: '(no artefact)', children: loose }); }
    const nowhere = entries.filter(e => !placed.has(e));
    if (nowhere.length) {
        roots.push({ type: 'page', name: '(target on no page)', children: nowhere.map(annotationNode) });
    }
    return roots;
}

/**
 * The Annotations view: artefact, page, target component, annotation. Each
 * annotation shows the lint findings of its document. With a rendered
 * preview it also shows the number the annotation got, or that it was not
 * placed.
 */
export class AnnotationsViewProvider implements vscode.TreeDataProvider<Node> {
    private readonly changed = new vscode.EventEmitter<void>();
    readonly onDidChangeTreeData = this.changed.event;
    private roots: Node[] | undefined;
    private labels = new Map<string, AnnotationLabel>();
    private timer?: ReturnType<typeof setTimeout>;

    constructor(private readonly indexer: WorkspaceIndexer) { }

    /** Rebuild soon: several triggers often come together. */
    refresh(): void {
        if (this.timer) { clearTimeout(this.timer); }
        this.timer = setTimeout(() => { this.roots = undefined; this.changed.fire(); }, 200);
    }

    /** Numbers and placement from a rendered preview, by annotation name. */
    setLabels(labels: Map<string, AnnotationLabel>): void {
        this.labels = labels;
        this.refresh();
    }

    async getChildren(node?: Node): Promise<Node[]> {
        if (node) { return 'children' in node ? node.children : []; }
        if (!this.roots) {
            const manifests = await loadManifests(this.indexer);
            this.roots = buildAnnotationTree(manifests, entry => annotationDiagnostics(entry), this.labels);
        }
        return this.roots;
    }

    getTreeItem(node: Node): vscode.TreeItem {
        switch (node.type) {
            case 'info':
                return new vscode.TreeItem(node.text);
            case 'artefact': {
                const item = new vscode.TreeItem(node.name, vscode.TreeItemCollapsibleState.Expanded);
                item.iconPath = new vscode.ThemeIcon('file');
                return item;
            }
            case 'page': {
                const item = new vscode.TreeItem(node.name, vscode.TreeItemCollapsibleState.Expanded);
                item.iconPath = new vscode.ThemeIcon('layout');
                return item;
            }
            case 'component': {
                const item = new vscode.TreeItem(node.name, vscode.TreeItemCollapsibleState.Expanded);
                item.description = node.kind;
                item.iconPath = new vscode.ThemeIcon('graph');
                return item;
            }
            case 'annotation': {
                const { entry, problems, label } = node;
                const item = new vscode.TreeItem(entry.name, vscode.TreeItemCollapsibleState.None);
                const mark = typeof entry.spec.mark === 'string' ? entry.spec.mark : 'marker';
                const parts = [label?.label ? `${label.label}` : '', mark, label && !label.placed ? 'not placed' : ''].filter(Boolean);
                if (problems.length) { parts.push(problems.map(p => codeOf(p)).join(', ')); }
                item.description = parts.join(' · ');
                const worst = problems.reduce((w, p) => Math.min(w, p.severity), vscode.DiagnosticSeverity.Hint + 1);
                item.iconPath = worst === vscode.DiagnosticSeverity.Error ? new vscode.ThemeIcon('error', new vscode.ThemeColor('problemsErrorIcon.foreground'))
                    : worst === vscode.DiagnosticSeverity.Warning ? new vscode.ThemeIcon('warning', new vscode.ThemeColor('problemsWarningIcon.foreground'))
                        : label && !label.placed ? new vscode.ThemeIcon('circle-slash')
                            : new vscode.ThemeIcon('comment');
                const tip = new vscode.MarkdownString(`**${entry.name}** (${mark})`);
                if (typeof entry.spec.title === 'string') { tip.appendMarkdown(`\n\n${entry.spec.title}`); }
                for (const p of problems) { tip.appendMarkdown(`\n\n- \`${codeOf(p)}\`: ${p.message}`); }
                item.tooltip = tip;
                item.contextValue = 'binoAnnotation';
                item.command = { command: 'bino.revealAnnotation', title: 'Show Annotation', arguments: [entry] };
                return item;
            }
        }
    }
}

/** The annotation lint findings of the lines of an annotation's document. */
export function annotationDiagnostics(entry: AnnotationEntry): vscode.Diagnostic[] {
    return vscode.languages.getDiagnostics(vscode.Uri.file(entry.file)).filter(d =>
        d.source === 'bino' && ANNOTATION_RULE.test(codeOf(d))
        && d.range.start.line >= entry.startLine && d.range.start.line <= entry.endLine);
}

function codeOf(d: vscode.Diagnostic): string {
    const code = d.code;
    if (code === undefined) { return ''; }
    return typeof code === 'object' ? String(code.value) : String(code);
}
