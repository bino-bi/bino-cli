import * as vscode from 'vscode';
import { WorkspaceIndexer } from '../indexer';
import { BinoPreviewManager } from '../preview';
import { SchemaResolver } from '../schemaResolver';
import { ContentDoc, ContentEditorProvider, contentDocs, EditorSession } from './contentEditor';
import { buildStyleSections } from './styleForm';
import { DEFAULT_STYLE, ENGINE_DEFAULTS_VERSION } from './styleModel';
import { getStyleEditorHtml } from './styleEditorHtml';

/** The content of a ComponentStyle by name, read from the open buffer or the disk. */
export async function readStyleContent(indexer: WorkspaceIndexer, name: string): Promise<unknown> {
    const doc = indexer.getDocuments(['ComponentStyle']).find(d => d.name === name);
    if (!doc) { return undefined; }
    const uri = vscode.Uri.file(doc.file);
    const open = vscode.workspace.textDocuments.find(d => d.uri.fsPath === uri.fsPath);
    let text: string;
    try {
        text = open ? open.getText() : Buffer.from(await vscode.workspace.fs.readFile(uri)).toString('utf8');
    } catch {
        return undefined;
    }
    const content = contentDocs(text, 'ComponentStyle').find(d => d.name === name)?.content;
    if (typeof content === 'string') {
        try { return JSON.parse(content); } catch { return undefined; }
    }
    return content;
}

/**
 * The ComponentStyle editor: one tab per style section, fields from the
 * schema, and the value an empty field inherits as its placeholder. Only
 * overrides are written: clearing a field removes its key.
 */
export class StyleEditorProvider extends ContentEditorProvider {
    static readonly viewType = 'bino.styleEditor';
    private readonly schema: SchemaResolver;

    constructor(indexer: WorkspaceIndexer, preview: BinoPreviewManager, extensionPath: string) {
        super(indexer, preview, 'ComponentStyle', StyleEditorProvider.viewType);
        this.schema = new SchemaResolver(extensionPath);
        this.schema.load();
    }

    protected html(nonce: string): string {
        return getStyleEditorHtml(nonce);
    }

    protected async buildState(s: EditorSession, doc: ContentDoc): Promise<Record<string, unknown>> {
        const isDefault = doc.name === DEFAULT_STYLE;
        const defaultContent = isDefault ? undefined : await readStyleContent(this.indexer, DEFAULT_STYLE);
        const content = typeof doc.content === 'string' ? safeJson(doc.content) : doc.content;
        return {
            name: doc.name,
            isDefault,
            hasDefault: defaultContent !== undefined,
            engineVersion: ENGINE_DEFAULTS_VERSION,
            sections: buildStyleSections(this.schema.getFieldsForDef('styleContent'), content, doc.name, defaultContent),
            usedBy: await this.usedBy(s, doc),
        };
    }
}

function safeJson(text: string): unknown {
    try { return JSON.parse(text); } catch { return undefined; }
}
