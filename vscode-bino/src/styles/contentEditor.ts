import * as vscode from 'vscode';
import { parseAllDocuments, isMap, Document, YAMLMap } from 'yaml';
import { WorkspaceIndexer, LSPDocument } from '../indexer';
import { AuthoringClient, formatEditDiagnostics, EditResult } from '../authoringClient';
import { BinoPreviewManager } from '../preview';
import { getNonce } from '../webviewNonce';
import { getIn, isObject, removalPath } from './styleModel';

/** A document of the edited kind in a YAML file. */
export interface ContentDoc {
    kind: string;
    name: string;
    /** 1-based document ordinal within the file. */
    position: number;
    /** `spec.content` as plain JSON: an object, a JSON string, or undefined. */
    content: unknown;
    /** Offset of the `metadata.name` value, for reference lookups. */
    nameOffset?: number;
}

/** A top-level manifest that references the edited document. */
export interface UsedByEntry {
    kind: string;
    name: string;
    file: string;
    position: number;
    /** Line (0-based) of the reference. */
    line: number;
}

export interface EditorSession {
    document: vscode.TextDocument;
    panel: vscode.WebviewPanel;
    /** The shown document, by name: positions shift. */
    target?: string;
    /** The manifest the embedded preview shows after an edit. */
    previewTarget?: UsedByEntry;
    refreshTimer?: ReturnType<typeof setTimeout>;
    suppressForwardSync: boolean;
    /** Editor-specific choice kept across re-renders (the RuleSet editor's style). */
    choice?: string;
    /** Messages run one after the other: each write computes from the text the previous one left. */
    queue: Promise<void>;
    /** Number of the latest render; an older one that finishes later is dropped. */
    renderSeq: number;
}

/** A message from the webview. */
export interface EditorMessage {
    type?: string;
    [k: string]: unknown;
}

/**
 * The common part of the ComponentStyle and RuleSet editors: a custom text
 * editor on the YAML file that edits `spec.content` of one document of its
 * kind. Every write goes through the AuthoringClient on the open TextDocument,
 * so undo, comments and validation work as in the text editor.
 */
export abstract class ContentEditorProvider implements vscode.CustomTextEditorProvider {
    protected readonly authoring: AuthoringClient;
    protected readonly sessions = new Set<EditorSession>();
    private readonly pendingTargets = new Map<string, string>();

    constructor(
        protected readonly indexer: WorkspaceIndexer,
        protected readonly preview: BinoPreviewManager,
        protected readonly kind: string,
        protected readonly viewType: string,
    ) {
        this.authoring = new AuthoringClient(indexer);
    }

    /** The page of the editor. */
    protected abstract html(nonce: string): string;

    /** The state the page renders for the shown document. */
    protected abstract buildState(s: EditorSession, doc: ContentDoc, docs: ContentDoc[]): Promise<Record<string, unknown>>;

    /** Editor-specific messages. Returns false when the message is not one of them. */
    protected async handleOwnMessage(_s: EditorSession, _msg: EditorMessage): Promise<boolean> {
        return false;
    }

    /** Open the editor for a document of this kind: the given one, one in the given file, the one at the cursor, or a picked one. */
    async open(target?: Pick<LSPDocument, 'file' | 'kind' | 'name'> | vscode.Uri): Promise<void> {
        const doc = target instanceof vscode.Uri ? await this.firstIn(target)
            : target ?? this.resolveActive() ?? (await this.pick());
        if (!doc) { return; }
        if (doc.kind !== this.kind) {
            vscode.window.showInformationMessage(`Bino: ${doc.name} is a ${doc.kind}, not a ${this.kind}.`);
            return;
        }
        const uri = vscode.Uri.file(doc.file);
        const session = [...this.sessions].find(s => s.document.uri.toString() === uri.toString());
        if (session) {
            session.target = doc.name;
            await this.render(session);
            session.panel.reveal();
            return;
        }
        this.pendingTargets.set(uri.toString(), doc.name);
        await vscode.commands.executeCommand('vscode.openWith', uri, this.viewType, vscode.ViewColumn.Active);
    }

    resolveCustomTextEditor(document: vscode.TextDocument, panel: vscode.WebviewPanel): void {
        const session: EditorSession = { document, panel, suppressForwardSync: false, queue: Promise.resolve(), renderSeq: 0 };
        this.sessions.add(session);
        panel.webview.options = { enableScripts: true };
        panel.webview.html = this.html(getNonce());

        const listeners = [
            panel.webview.onDidReceiveMessage(msg => {
                const run = () => this.handleMessage(session, msg).catch(err => {
                    vscode.window.showErrorMessage(`Bino: ${err instanceof Error ? err.message : String(err)}`);
                });
                // Navigation can wait for a preview server: it must not hold up writes.
                if (msg?.type === 'openUsedBy' || msg?.type === 'revealSource') { void run(); return; }
                session.queue = session.queue.then(run);
            }),
            vscode.workspace.onDidChangeTextDocument(e => {
                if (e.contentChanges.length === 0 || e.document.languageId !== 'yaml') { return; }
                // Our own file at once; other files can change what is inherited or who uses it.
                const own = e.document.uri.toString() === document.uri.toString();
                if (session.refreshTimer) { clearTimeout(session.refreshTimer); }
                session.refreshTimer = setTimeout(() => { void this.render(session); }, own ? 300 : 1000);
            }),
        ];
        panel.onDidDispose(() => {
            if (session.refreshTimer) { clearTimeout(session.refreshTimer); }
            for (const l of listeners) { l.dispose(); }
            this.sessions.delete(session);
        });
    }

    /** Re-render every open editor, e.g. after the backend schema arrived. */
    refreshAll(): void {
        for (const s of this.sessions) { void this.render(s); }
    }

    dispose(): void {
        for (const s of this.sessions) { s.panel.dispose(); }
    }

    /** Push the state of the shown document to the page. */
    protected async render(s: EditorSession): Promise<void> {
        const seq = ++s.renderSeq;
        const docs = contentDocs(s.document.getText(), this.kind);
        const doc = docs.find(d => d.name === s.target) ?? docs[0];
        s.target = doc?.name;
        if (!doc) {
            void s.panel.webview.postMessage({ type: 'state', empty: `No ${this.kind} in this file.` });
            return;
        }
        const state = await this.buildState(s, doc, docs);
        if (seq !== s.renderSeq) { return; }
        void s.panel.webview.postMessage({
            type: 'state',
            docs: docs.map(d => d.name),
            current: doc.name,
            jsonString: typeof doc.content === 'string',
            ...state,
        });
    }

    /** The shown document in the current text. */
    protected current(s: EditorSession): ContentDoc | undefined {
        const docs = contentDocs(s.document.getText(), this.kind);
        return docs.find(d => d.name === s.target) ?? docs[0];
    }

    private async handleMessage(s: EditorSession, msg: EditorMessage): Promise<void> {
        switch (msg.type) {
            case 'ready': {
                const key = s.document.uri.toString();
                s.target = this.pendingTargets.get(key) ?? (msg.target as string | undefined) ?? s.target;
                this.pendingTargets.delete(key);
                await this.render(s);
                return;
            }
            case 'select':
                s.target = String(msg.name);
                await this.render(s);
                return;
            case 'edit':
                await this.editContent(s, { [joinPath(msg.path as string[])]: msg.value });
                return;
            case 'remove':
                await this.removeContent(s, [msg.path as string[]]);
                return;
            case 'convert':
                await this.convertJsonString(s);
                return;
            case 'openUsedBy':
                await this.openUsedBy(s, msg.entry as UsedByEntry);
                return;
            case 'revealSource': {
                const doc = this.current(s);
                if (doc) { await revealDocument(s.document.uri, doc.nameOffset ?? 0); }
                return;
            }
        }
        await this.handleOwnMessage(s, msg);
    }

    /** Write keys below `spec.content`, given as content-relative dotted paths. */
    protected async editContent(s: EditorSession, patch: Record<string, unknown>): Promise<boolean> {
        const doc = this.current(s);
        if (!doc || typeof doc.content === 'string' || Object.keys(patch).length === 0) { return false; }
        const full: Record<string, unknown> = {};
        for (const [path, value] of Object.entries(patch)) { full[`spec.content.${path}`] = value; }
        return this.apply(s, this.authoring.edit({ file: s.document.uri.fsPath, position: doc.position, patch: full }));
    }

    /** Remove keys below `spec.content`, and parents that would be left empty. */
    protected async removeContent(s: EditorSession, paths: string[][]): Promise<boolean> {
        const doc = this.current(s);
        if (!doc || typeof doc.content === 'string') { return false; }
        // One path after the other on a copy, so parents left empty by several removals go too.
        const content = JSON.parse(JSON.stringify(doc.content ?? {})) as unknown;
        const cut: string[] = [];
        for (const p of paths.filter(p => Array.isArray(p) && p.length > 0)) {
            if (getIn(content, p) === undefined) { continue; }
            const removed = removalPath(content, p);
            deleteIn(content, removed);
            cut.push(`spec.content.${joinPath(removed)}`);
        }
        if (cut.length === 0) { return false; }
        return this.apply(s, this.authoring.remove({ file: s.document.uri.fsPath, position: doc.position, paths: [...new Set(cut)] }));
    }

    /** Replace a JSON string content with the same value as a YAML object. */
    private async convertJsonString(s: EditorSession): Promise<void> {
        const doc = this.current(s);
        if (!doc || typeof doc.content !== 'string') { return; }
        let parsed: unknown;
        try {
            parsed = JSON.parse(doc.content);
        } catch (err) {
            vscode.window.showErrorMessage(`Bino: spec.content is not valid JSON: ${err instanceof Error ? err.message : String(err)}`);
            return;
        }
        await this.apply(s, this.authoring.edit({ file: s.document.uri.fsPath, position: doc.position, patch: { 'spec.content': parsed } }));
    }

    /** Apply a write, surface a rejection, and refresh the preview the user picked. */
    protected async apply(s: EditorSession, pending: Promise<EditResult>): Promise<boolean> {
        s.suppressForwardSync = true;
        try {
            const result = await pending;
            if (!result.ok) {
                vscode.window.showErrorMessage(`Edit rejected: ${formatEditDiagnostics(result)}`);
                return false;
            }
            this.reloadPreview(s);
            return true;
        } finally {
            setTimeout(() => { s.suppressForwardSync = false; }, 100);
        }
    }

    /** Show a manifest that uses the edited document in the embedded preview. */
    private async openUsedBy(s: EditorSession, entry: UsedByEntry | undefined): Promise<void> {
        if (!entry) { return; }
        s.previewTarget = entry;
        await this.preview.previewArtefactEmbedded({ kind: entry.kind, name: entry.name, file: entry.file, position: entry.position }, true);
    }

    /** After an edit, re-show the picked manifest; the preview reads the unsaved buffer. */
    private reloadPreview(s: EditorSession): void {
        const target = s.previewTarget;
        if (!target || !this.preview.isEmbeddedPreviewOpen()) { return; }
        void this.preview.previewArtefactEmbedded({ kind: target.kind, name: target.name, file: target.file, position: target.position }, true);
    }

    /** The top-level manifests that reference the shown document, from the language server. */
    protected async usedBy(s: EditorSession, doc: ContentDoc): Promise<UsedByEntry[]> {
        if (doc.nameOffset === undefined) { return []; }
        let locations: vscode.Location[] | undefined;
        try {
            locations = await vscode.commands.executeCommand<vscode.Location[]>(
                'vscode.executeReferenceProvider', s.document.uri, s.document.positionAt(doc.nameOffset));
        } catch {
            return [];
        }
        const entries: UsedByEntry[] = [];
        const texts = new Map<string, string>();
        for (const loc of locations ?? []) {
            const file = loc.uri.fsPath;
            if (!texts.has(file)) {
                const open = vscode.workspace.textDocuments.find(d => d.uri.fsPath === file);
                try {
                    texts.set(file, open ? open.getText() : Buffer.from(await vscode.workspace.fs.readFile(loc.uri)).toString('utf8'));
                } catch {
                    continue;
                }
            }
            const owner = topLevelDocAt(texts.get(file)!, offsetOf(texts.get(file)!, loc.range.start));
            if (!owner || owner.kind === this.kind) { continue; }
            if (entries.some(e => e.file === file && e.position === owner.position)) { continue; }
            entries.push({ ...owner, file, line: loc.range.start.line });
        }
        return entries.sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));
    }

    /** The document of this kind at the cursor of the file, or its first one. */
    private async firstIn(uri: vscode.Uri): Promise<Pick<LSPDocument, 'file' | 'kind' | 'name'> | undefined> {
        const active = vscode.window.activeTextEditor;
        if (active?.document.uri.toString() === uri.toString()) { return this.resolveActive(); }
        const text = (await vscode.workspace.openTextDocument(uri)).getText();
        const doc = contentDocs(text, this.kind)[0];
        return doc ? { file: uri.fsPath, kind: this.kind, name: doc.name } : undefined;
    }

    private resolveActive(): Pick<LSPDocument, 'file' | 'kind' | 'name'> | undefined {
        const editor = vscode.window.activeTextEditor;
        if (!editor) { return undefined; }
        const text = editor.document.getText();
        const owner = topLevelDocAt(text, editor.document.offsetAt(editor.selection.active));
        const doc = owner?.kind === this.kind ? owner : contentDocs(text, this.kind)[0];
        return doc ? { file: editor.document.uri.fsPath, kind: this.kind, name: doc.name } : undefined;
    }

    private async pick(): Promise<LSPDocument | undefined> {
        const docs = this.indexer.getDocuments([this.kind]);
        if (docs.length === 0) {
            vscode.window.showInformationMessage(`Bino: no ${this.kind} in the workspace.`);
            return undefined;
        }
        const picked = await vscode.window.showQuickPick(docs.map(d => ({ label: d.name, description: vscode.workspace.asRelativePath(d.file), doc: d })), {
            placeHolder: `Open a ${this.kind}`,
        });
        return picked?.doc;
    }
}

/** Delete the key at a path of a plain object, in place. */
function deleteIn(root: unknown, path: string[]): void {
    const parent = getIn(root, path.slice(0, -1));
    if (isObject(parent)) { delete parent[path[path.length - 1]]; }
}

/** Content-relative key path as the dotted path of the edit helper. */
export function joinPath(path: string[]): string {
    return path.join('.');
}

/** The documents of one kind in a YAML file, in file order. */
export function contentDocs(text: string, kind: string): ContentDoc[] {
    const out: ContentDoc[] = [];
    forEachDoc(text, (_doc, contents, position) => {
        if (contents.get('kind') !== kind) { return; }
        const metadata = contents.get('metadata');
        const nameNode = isMap(metadata) ? (metadata as YAMLMap).get('name', true) as { value?: unknown; range?: number[] } | undefined : undefined;
        if (typeof nameNode?.value !== 'string') { return; }
        const spec = contents.get('spec');
        const content = isMap(spec) ? (spec as YAMLMap).get('content') : undefined;
        out.push({
            kind,
            name: nameNode.value,
            position,
            content: content && typeof (content as { toJSON?: unknown }).toJSON === 'function'
                ? (content as { toJSON: () => unknown }).toJSON()
                : content,
            nameOffset: nameNode.range ? nameNode.range[0] : undefined,
        });
    });
    return out;
}

/** The top-level manifest whose text holds an offset. */
export function topLevelDocAt(text: string, offset: number): { kind: string; name: string; position: number } | undefined {
    let found: { kind: string; name: string; position: number } | undefined;
    forEachDoc(text, (doc, contents, position) => {
        const range = doc.range;
        if (!range || offset < range[0] || offset > range[2]) { return; }
        const kind = contents.get('kind');
        const metadata = contents.get('metadata');
        const name = isMap(metadata) ? (metadata as YAMLMap).get('name') : undefined;
        if (typeof kind === 'string' && typeof name === 'string') { found = { kind, name, position }; }
    });
    return found;
}

function forEachDoc(text: string, fn: (doc: Document, contents: YAMLMap, position: number) => void): void {
    let parsed: Document[];
    try {
        parsed = parseAllDocuments(text) as Document[];
    } catch {
        return;
    }
    parsed.forEach((doc, i) => {
        if (isMap(doc.contents)) { fn(doc, doc.contents as YAMLMap, i + 1); }
    });
}

function offsetOf(text: string, pos: vscode.Position): number {
    let offset = 0;
    for (let line = 0; line < pos.line; line++) {
        const next = text.indexOf('\n', offset);
        if (next < 0) { return text.length; }
        offset = next + 1;
    }
    return offset + pos.character;
}

async function revealDocument(uri: vscode.Uri, offset: number): Promise<void> {
    const doc = await vscode.workspace.openTextDocument(uri);
    const pos = doc.positionAt(offset);
    await vscode.window.showTextDocument(doc, { selection: new vscode.Range(pos, pos), viewColumn: vscode.ViewColumn.Beside });
}

/** Minimal HTML escaper for host-built strings. */
export function escapeHtml(str: string): string {
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}
