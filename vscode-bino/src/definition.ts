import * as vscode from 'vscode';
import { WorkspaceIndexer, LSPDocument } from './indexer';

/**
 * Reference field patterns and their corresponding document kinds.
 */
interface ReferencePattern {
    /** Regex to match the field key */
    keyPattern: RegExp;
    /** Document kinds to search for */
    kinds: string[];
    /** Whether the value may have a prefix (like $ for DataSource) */
    stripPrefix?: string;
}

const REFERENCE_PATTERNS: ReferencePattern[] = [
    // dataset: can reference DataSet names or $DataSource names
    {
        keyPattern: /^\s*dataset:\s*/,
        kinds: ['DataSet', 'DataSource'],
        stripPrefix: '$'
    },
    // signingProfile: references SigningProfile names
    {
        keyPattern: /^\s*signingProfile:\s*/,
        kinds: ['SigningProfile']
    },
    // secret: references ConnectionSecret names
    {
        keyPattern: /^\s*secret:\s*/,
        kinds: ['ConnectionSecret']
    },
    // dependencies array items
    {
        keyPattern: /^\s*-\s+/,
        kinds: ['DataSource', 'DataSet']
    },
    // layout references (in children)
    {
        keyPattern: /^\s*layout:\s*/,
        kinds: ['LayoutPage', 'LayoutCard']
    },
    // report references
    {
        keyPattern: /^\s*report:\s*/,
        kinds: ['ReportArtefact']
    }
    // Note: ref: is handled specially in provideDefinition to use child's kind field
];

/**
 * Definition provider for Bino YAML manifests.
 * Provides go-to-definition for references like dataset, signingProfile, etc.
 */
export class BinoDefinitionProvider implements vscode.DefinitionProvider {
    private indexer: WorkspaceIndexer;

    constructor(indexer: WorkspaceIndexer) {
        this.indexer = indexer;
    }

    async provideDefinition(
        document: vscode.TextDocument,
        position: vscode.Position,
        token: vscode.CancellationToken
    ): Promise<vscode.Definition | undefined> {
        // Only provide definitions for Bino documents
        if (!await this.indexer.isBinoDocument(document)) {
            return undefined;
        }

        const line = document.lineAt(position.line).text;
        const wordRange = document.getWordRangeAtPosition(position, /[\w$-]+/);

        if (!wordRange) {
            return undefined;
        }

        const word = document.getText(wordRange);

        // Check if we're in a dependencies array
        if (this.isInDependenciesArray(document, position)) {
            return this.findDefinition(word, ['DataSource', 'DataSet']);
        }

        // Special handling for ref: field - determine kinds from layout child's kind field
        if (/^\s*ref:\s*/.test(line)) {
            const childKind = this.findLayoutChildKind(document, position);
            if (childKind && childKind !== 'LayoutPage') {
                return this.findDefinition(word, [childKind]);
            }
            // Fall back to all referenceable kinds
            return this.findDefinition(word, ['Text', 'Table', 'ChartStructure', 'ChartTime', 'LayoutCard', 'Image']);
        }

        // Check each reference pattern
        for (const pattern of REFERENCE_PATTERNS) {
            if (pattern.keyPattern.test(line)) {
                let searchName = word;

                // Handle prefixed references (like $DataSourceName)
                if (pattern.stripPrefix && word.startsWith(pattern.stripPrefix)) {
                    searchName = word.substring(pattern.stripPrefix.length);
                }

                // For dataset field, if it starts with $, only search DataSource
                if (pattern.keyPattern.source.includes('dataset:') && word.startsWith('$')) {
                    return this.findDefinition(searchName, ['DataSource']);
                }

                return await this.findDefinition(searchName, pattern.kinds);
            }
        }

        // Check for asset: references (e.g., messageImage: asset:logo)
        if (line.includes('asset:')) {
            const assetMatch = line.match(/asset:(\w+)/);
            if (assetMatch && word === assetMatch[1]) {
                return this.findDefinition(word, ['Asset']);
            }
        }

        return undefined;
    }

    /**
     * Find the kind of the layout child we're currently in.
     * Looks backwards for a 'kind:' field at the same or parent indentation level.
     */
    private findLayoutChildKind(document: vscode.TextDocument, position: vscode.Position): string | undefined {
        const currentIndent = this.getIndentation(document.lineAt(position.line).text);

        // Look backwards to find the kind field
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 20; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();
            const lineIndent = this.getIndentation(line);

            // Found kind field at same or parent level
            // Handle both "kind:" and "- kind:" (YAML array items)
            const kindMatch = trimmed.match(/^(?:-\s+)?kind:\s*["']?(\w+)["']?/);
            if (kindMatch && lineIndent <= currentIndent) {
                return kindMatch[1];
            }

            // Stop if we hit a different block (children array start or parent object)
            if (lineIndent < currentIndent - 4 && trimmed && !trimmed.startsWith('#')) {
                break;
            }
        }

        return undefined;
    }
    /**
     * Check if the cursor is within a dependencies array.
     */
    private isInDependenciesArray(document: vscode.TextDocument, position: vscode.Position): boolean {
        // Look backwards to find if we're in a dependencies array
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 15; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();

            if (trimmed.startsWith('dependencies:')) {
                // Check if current line is indented more than this line (is a child)
                const currentIndent = this.getIndentation(document.lineAt(position.line).text);
                const parentIndent = this.getIndentation(line);
                if (currentIndent > parentIndent) {
                    return true;
                }
            }

            // Stop if we hit a different top-level key at same or less indentation
            if (trimmed.endsWith(':') && !trimmed.startsWith('-') && !trimmed.startsWith('#')) {
                const thisIndent = this.getIndentation(line);
                const currentIndent = this.getIndentation(document.lineAt(position.line).text);
                if (thisIndent <= currentIndent && lineNum !== position.line) {
                    break;
                }
            }
        }
        return false;
    }

    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }

    /**
     * Find definition for a reference name among the given kinds.
     */
    private async findDefinition(name: string, kinds: string[]): Promise<vscode.Location | undefined> {
        const documents = this.indexer.getDocuments(kinds);
        const targetDoc = documents.find(doc => doc.name === name);

        if (!targetDoc) {
            return undefined;
        }

        return await this.createLocation(targetDoc);
    }

    /**
     * Create a VS Code Location from an LSPDocument.
     * Position is a 1-based document index within a multi-doc YAML file.
     * We need to find the actual line by counting YAML document separators (---).
     */
    private async createLocation(doc: LSPDocument): Promise<vscode.Location> {
        const uri = vscode.Uri.file(doc.file);
        const lineNumber = await this.findDocumentLine(doc.file, doc.position);
        const position = new vscode.Position(lineNumber, 0);
        return new vscode.Location(uri, position);
    }

    /**
     * Find the line number where the Nth document starts in a multi-doc YAML file.
     * @param filePath Path to the YAML file
     * @param docIndex 1-based document index
     * @returns 0-based line number
     */
    private async findDocumentLine(filePath: string, docIndex: number): Promise<number> {
        try {
            const uri = vscode.Uri.file(filePath);
            const document = await vscode.workspace.openTextDocument(uri);
            const text = document.getText();
            const lines = text.split('\n');

            let currentDocIndex = 0;

            for (let lineNum = 0; lineNum < lines.length; lineNum++) {
                const line = lines[lineNum].trim();

                // Check for document start: either start of file with content,
                // or a line starting with '---'
                if (lineNum === 0) {
                    // First document starts at line 0 (or after initial ---)
                    if (line === '---') {
                        // Explicit document start, don't count yet
                        continue;
                    } else if (line && !line.startsWith('#')) {
                        // Content starts immediately
                        currentDocIndex = 1;
                        if (currentDocIndex === docIndex) {
                            return 0;
                        }
                    }
                } else if (line === '---') {
                    // New document separator
                    currentDocIndex++;
                    if (currentDocIndex === docIndex) {
                        // Return the line after ---
                        return lineNum + 1;
                    }
                } else if (currentDocIndex === 0 && line && !line.startsWith('#')) {
                    // First content after potential leading comments/blank lines
                    currentDocIndex = 1;
                    if (currentDocIndex === docIndex) {
                        return lineNum;
                    }
                }
            }

            // Fallback to start of file
            return 0;
        } catch {
            return 0;
        }
    }
}
