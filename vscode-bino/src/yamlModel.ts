import { parseAllDocuments, Document, Scalar, YAMLMap, YAMLSeq, Pair, isScalar, isMap, isSeq, isPair } from 'yaml';

/** Represents a single YAML document in a multi-doc file */
export interface TreeDocument {
    docIndex: number;
    kind: string;
    name: string;
    nodes: TreeNode[];
    startLine: number;
    endLine: number;
}

/** Represents a node in the YAML tree */
export interface TreeNode {
    key: string;
    value: unknown;
    displayValue: string;
    type: 'string' | 'number' | 'boolean' | 'null' | 'object' | 'array' | 'multiline';
    path: string[];
    line: number;
    column: number;
    children?: TreeNode[];
    /** Source range in the text for reverse-sync edits */
    valueRange?: { start: number; end: number };
}

/**
 * Parse a multi-document YAML string into TreeDocument models.
 * Uses the `yaml` library for CST-preserving parsing.
 */
export function parseYamlDocuments(text: string): TreeDocument[] {
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
            // range is [start, valueEnd, nodeEnd] offsets — but we don't have the text here
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
export function resolveLineNumbers(text: string, docs: TreeDocument[]): void {
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

/**
 * Apply an edit to a YAML document.
 * Returns the new full text after the edit, or undefined on failure.
 *
 * @param text Current document text
 * @param docIndex 0-based document index
 * @param path Key path within the document (e.g. ['spec', 'type'])
 * @param newValue New value to set
 */
export function applyEdit(
    text: string,
    docIndex: number,
    path: string[],
    newValue: unknown
): { newText: string; editStart: number; editEnd: number } | undefined {
    try {
        const docs = parseAllDocuments(text, { keepSourceTokens: true });
        const doc = docs[docIndex];
        if (!doc || !doc.contents) { return undefined; }

        // Navigate to the parent and set the value
        doc.setIn(path, newValue);

        // Rebuild the full text from all documents
        const newText = docs.map((d, i) => {
            const str = d.toString();
            // Add document separator for documents after the first
            if (i > 0 && !str.startsWith('---')) {
                return '---\n' + str;
            }
            return str;
        }).join('');

        return { newText, editStart: 0, editEnd: text.length };
    } catch {
        return undefined;
    }
}

/**
 * Remove a field from a YAML document.
 */
export function removeField(
    text: string,
    docIndex: number,
    path: string[]
): { newText: string } | undefined {
    try {
        const docs = parseAllDocuments(text, { keepSourceTokens: true });
        const doc = docs[docIndex];
        if (!doc || !doc.contents) { return undefined; }

        doc.deleteIn(path);

        const newText = docs.map((d, i) => {
            const str = d.toString();
            if (i > 0 && !str.startsWith('---')) {
                return '---\n' + str;
            }
            return str;
        }).join('');

        return { newText };
    } catch {
        return undefined;
    }
}

/**
 * Add a new field to a YAML document.
 */
export function addField(
    text: string,
    docIndex: number,
    path: string[],
    value: unknown
): { newText: string } | undefined {
    try {
        const docs = parseAllDocuments(text, { keepSourceTokens: true });
        const doc = docs[docIndex];
        if (!doc || !doc.contents) { return undefined; }

        doc.setIn(path, value);

        const newText = docs.map((d, i) => {
            const str = d.toString();
            if (i > 0 && !str.startsWith('---')) {
                return '---\n' + str;
            }
            return str;
        }).join('');

        return { newText };
    } catch {
        return undefined;
    }
}
