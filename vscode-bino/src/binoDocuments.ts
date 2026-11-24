import * as vscode from 'vscode';
import { parseAllDocuments, isMap } from 'yaml';
import { getLanguageClient } from './languageClient';

/** JSON-RPC "method not found": the server predates a custom request. */
export const METHOD_NOT_FOUND = -32601;

/** Tell the user once per session that a feature needs a newer bino. */
let warnedOutdated = false;
export function warnOutdatedBino(feature: string): void {
    if (warnedOutdated) {
        return;
    }
    warnedOutdated = true;
    void vscode.window.showInformationMessage(
        `${feature} needs a newer bino. Update bino to enable it.`
    );
}

/** LSP request that outlines an open buffer (needs a bino with this request). */
export const DOCUMENTS_REQUEST = 'bino/documents';

interface LspPosition { line: number; character: number }
interface LspRange { start: LspPosition; end: LspPosition }

/** Inline text of a DataSet's spec.query or spec.prql, as located by the server. */
export interface QueryBlock {
    field: 'query' | 'prql';
    language: 'sql' | 'prql';
    /** Source span of the query text. For a block scalar, start.character is the indent. */
    range: LspRange;
}

/** One bino manifest of a buffer. */
export interface BinoDocumentInfo {
    kind: string;
    name: string;
    /** 1-based document ordinal within the file, as the project index counts it. */
    position: number;
    range: LspRange;
    blocks: QueryBlock[];
}

/** Answer of bino/documents for one buffer. */
export interface BinoDocumentsResult {
    isBino: boolean;
    version: number;
    documents: BinoDocumentInfo[];
}

const cache = new Map<string, { version: number; result: BinoDocumentsResult }>();

/**
 * Outline of the buffer's current (unsaved) text from the language server.
 * Undefined when the server is not running or is too old for the request.
 * Results are cached per document version.
 */
export async function getBinoDocuments(document: vscode.TextDocument): Promise<BinoDocumentsResult | undefined> {
    const key = document.uri.toString();
    const version = document.version;
    const hit = cache.get(key);
    if (hit && hit.version === version) {
        return hit.result;
    }
    const client = getLanguageClient();
    if (!client) {
        return undefined;
    }
    let result: BinoDocumentsResult;
    try {
        result = await client.sendRequest<BinoDocumentsResult>(DOCUMENTS_REQUEST, { textDocument: { uri: key } });
    } catch (err) {
        if ((err as { code?: number }).code === METHOD_NOT_FOUND) {
            warnOutdatedBino('CodeLens and the PRQL actions');
        }
        return undefined;
    }
    // The buffer may have moved on while the request was in flight.
    if (document.version === version) {
        cache.set(key, { version, result });
    }
    return result;
}

/**
 * Local guess for when the language server cannot answer: some document of the
 * text has an apiVersion starting with bino.bi/.
 */
export function hasBinoApiVersion(text: string): boolean {
    try {
        return parseAllDocuments(text).some(doc => {
            if (!isMap(doc.contents)) {
                return false;
            }
            const apiVersion: unknown = doc.contents.get('apiVersion');
            return typeof apiVersion === 'string' && apiVersion.startsWith('bino.bi/');
        });
    } catch {
        return false;
    }
}

/** The cached outline when it matches the buffer's current version. */
export function getCachedBinoDocuments(document: vscode.TextDocument): BinoDocumentsResult | undefined {
    const hit = cache.get(document.uri.toString());
    return hit && hit.version === document.version ? hit.result : undefined;
}

/** Drop cached outlines of closed buffers. */
export function registerBinoDocumentsCache(context: vscode.ExtensionContext): void {
    context.subscriptions.push(
        vscode.workspace.onDidCloseTextDocument(doc => cache.delete(doc.uri.toString()))
    );
}

export function toVsRange(r: LspRange): vscode.Range {
    return new vscode.Range(r.start.line, r.start.character, r.end.line, r.end.character);
}

/** True when a position lies on the lines a block covers. */
export function blockContainsLine(block: QueryBlock, line: number): boolean {
    return line >= block.range.start.line && line <= block.range.end.line;
}

/**
 * The block's text as written: each covered line from the start column on
 * (lines shorter than the indent are blank), the last one cut at the end column.
 */
export function blockText(text: string, block: QueryBlock): string {
    const lines = text.split('\n').map(l => l.replace(/\r$/, ''));
    const { start, end } = block.range;
    const out: string[] = [];
    for (let i = start.line; i <= end.line && i < lines.length; i++) {
        const line = i === end.line ? lines[i].substring(0, end.character) : lines[i];
        out.push(line.length > start.character ? line.substring(start.character) : '');
    }
    return out.join('\n');
}
