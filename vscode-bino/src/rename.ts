import * as vscode from 'vscode';
import { WorkspaceIndexer, LSPDocument } from './indexer';

/**
 * Reference field patterns and their corresponding document kinds.
 * Mirrors the patterns from definition.ts for consistency.
 */
interface ReferencePattern {
    /** Regex to match the field key */
    keyPattern: RegExp;
    /** Document kinds this pattern can reference */
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
];

/**
 * Rename provider for Bino YAML manifests.
 * Provides rename functionality for document identifiers like datasets, datasources,
 * signing profiles, assets, etc.
 */
export class BinoRenameProvider implements vscode.RenameProvider {
    private indexer: WorkspaceIndexer;

    constructor(indexer: WorkspaceIndexer) {
        this.indexer = indexer;
    }

    /**
     * Called when the user initiates a rename (F2 or context menu).
     * Returns the range of the symbol to rename and validates that rename is possible.
     */
    async prepareRename(
        document: vscode.TextDocument,
        position: vscode.Position,
        token: vscode.CancellationToken
    ): Promise<vscode.Range | { range: vscode.Range; placeholder: string } | undefined> {
        // Only allow rename in Bino documents
        if (!await this.indexer.isBinoDocument(document)) {
            throw new Error('Rename is only available in Bino documents');
        }

        const line = document.lineAt(position.line).text;
        const wordRange = document.getWordRangeAtPosition(position, /[\w$-]+/);

        if (!wordRange) {
            throw new Error('No symbol found at cursor position');
        }

        const word = document.getText(wordRange);

        // Check if we're on a document name definition (metadata.name field)
        const nameDefinition = this.findNameDefinition(document, position, word);
        if (nameDefinition) {
            return {
                range: wordRange,
                placeholder: word
            };
        }

        // Check if we're on a reference to a known document
        const referencedDoc = await this.findReferencedDocument(document, position, line, word);
        if (referencedDoc) {
            return {
                range: wordRange,
                placeholder: word
            };
        }

        throw new Error('Rename is only supported for document identifiers (datasets, datasources, signing profiles, assets, etc.)');
    }

    /**
     * Provides the actual rename edits.
     * Finds all references to the symbol and returns a WorkspaceEdit to update them.
     */
    async provideRenameEdits(
        document: vscode.TextDocument,
        position: vscode.Position,
        newName: string,
        token: vscode.CancellationToken
    ): Promise<vscode.WorkspaceEdit | undefined> {
        // Validate new name
        if (!newName || newName.trim().length === 0) {
            throw new Error('New name cannot be empty');
        }

        // Check for invalid characters in new name
        if (!/^[\w$-]+$/.test(newName)) {
            throw new Error('Name can only contain letters, numbers, hyphens, underscores, and $ prefix');
        }

        const line = document.lineAt(position.line).text;
        const wordRange = document.getWordRangeAtPosition(position, /[\w$-]+/);

        if (!wordRange) {
            return undefined;
        }

        const oldName = document.getText(wordRange);

        // Find the document being renamed
        const targetDoc = await this.findTargetDocument(document, position, line, oldName);
        if (!targetDoc) {
            throw new Error(`Could not find document definition for "${oldName}"`);
        }

        // Determine if we need to handle $ prefix
        // If old name starts with $ and new name doesn't (or vice versa), adjust
        const oldHasPrefix = oldName.startsWith('$');
        const newHasPrefix = newName.startsWith('$');

        // For DataSource references, $ prefix indicates the reference, not the actual name
        // The actual document name doesn't have the $ prefix
        let actualOldName = oldHasPrefix ? oldName.substring(1) : oldName;
        let actualNewName = newHasPrefix ? newName.substring(1) : newName;

        // If the target is a DataSource and we're renaming via a $ reference,
        // use the name without the prefix
        if (targetDoc.kind === 'DataSource') {
            actualOldName = targetDoc.name;
            // Keep the new name without $ for the actual document name
        }

        // Collect all locations to edit
        const workspaceEdit = new vscode.WorkspaceEdit();
        const locations = await this.findAllReferences(targetDoc, actualOldName);

        // Add edits for each location
        for (const loc of locations) {
            const uri = loc.uri;
            const range = loc.range;
            const existingText = await this.getTextAtRange(uri, range);

            // Determine what text to use based on context
            let replacementText = actualNewName;

            // If the existing reference has a $ prefix (DataSource reference in dataset field),
            // keep the $ prefix in the replacement
            if (existingText.startsWith('$') && targetDoc.kind === 'DataSource') {
                replacementText = '$' + actualNewName;
            }

            workspaceEdit.replace(uri, range, replacementText);
        }

        return workspaceEdit;
    }

    /**
     * Check if the cursor is on a document name definition (metadata.name field).
     */
    private findNameDefinition(
        document: vscode.TextDocument,
        position: vscode.Position,
        word: string
    ): LSPDocument | undefined {
        const line = document.lineAt(position.line).text;

        // Check if this line is a `name:` field
        if (!line.trim().startsWith('name:')) {
            return undefined;
        }

        // Check if we're in a metadata block
        const isInMetadata = this.isInMetadataBlock(document, position);
        if (!isInMetadata) {
            return undefined;
        }

        // Find the document that this name defines
        const documents = this.indexer.getDocuments();
        return documents.find(doc =>
            doc.name === word &&
            doc.file === document.uri.fsPath
        );
    }

    /**
     * Check if the current position is inside a metadata block.
     */
    private isInMetadataBlock(document: vscode.TextDocument, position: vscode.Position): boolean {
        const currentLine = document.lineAt(position.line).text;
        const currentIndent = this.getIndentation(currentLine);

        // Look backwards to find metadata:
        for (let lineNum = position.line - 1; lineNum >= 0 && lineNum > position.line - 10; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();
            const lineIndent = this.getIndentation(line);

            if (trimmed === 'metadata:' && lineIndent < currentIndent) {
                return true;
            }

            // If we hit a key at same or lower indentation, stop
            if (trimmed.endsWith(':') && !trimmed.startsWith('-') && lineIndent <= currentIndent) {
                if (trimmed !== 'metadata:') {
                    return false;
                }
            }
        }

        return false;
    }

    /**
     * Find a referenced document at the cursor position.
     */
    private async findReferencedDocument(
        document: vscode.TextDocument,
        position: vscode.Position,
        line: string,
        word: string
    ): Promise<LSPDocument | undefined> {
        // Check if we're in a dependencies array
        if (this.isInDependenciesArray(document, position)) {
            return this.findDocumentByName(word, ['DataSource', 'DataSet']);
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
                    return this.findDocumentByName(searchName, ['DataSource']);
                }

                return this.findDocumentByName(searchName, pattern.kinds);
            }
        }

        // Check for asset: references (e.g., messageImage: asset:logo)
        if (line.includes('asset:')) {
            const assetMatch = line.match(/asset:(\w+)/);
            if (assetMatch && word === assetMatch[1]) {
                return this.findDocumentByName(word, ['Asset']);
            }
        }

        return undefined;
    }

    /**
     * Find target document for rename (either definition or reference).
     */
    private async findTargetDocument(
        document: vscode.TextDocument,
        position: vscode.Position,
        line: string,
        word: string
    ): Promise<LSPDocument | undefined> {
        // First check if we're on a name definition
        const nameDefinition = this.findNameDefinition(document, position, word);
        if (nameDefinition) {
            return nameDefinition;
        }

        // Otherwise check if we're on a reference
        return this.findReferencedDocument(document, position, line, word);
    }

    /**
     * Check if the cursor is within a dependencies array.
     */
    private isInDependenciesArray(document: vscode.TextDocument, position: vscode.Position): boolean {
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 15; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();

            if (trimmed.startsWith('dependencies:')) {
                const currentIndent = this.getIndentation(document.lineAt(position.line).text);
                const parentIndent = this.getIndentation(line);
                if (currentIndent > parentIndent) {
                    return true;
                }
            }

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

    /**
     * Find a document by name among the given kinds.
     */
    private findDocumentByName(name: string, kinds: string[]): LSPDocument | undefined {
        const documents = this.indexer.getDocuments(kinds);
        return documents.find(doc => doc.name === name);
    }

    /**
     * Find all references to a document across the workspace.
     */
    private async findAllReferences(
        targetDoc: LSPDocument,
        name: string
    ): Promise<vscode.Location[]> {
        const locations: vscode.Location[] = [];
        const seenLocations = new Set<string>();

        // Helper to add location with deduplication
        const addLocation = (uri: vscode.Uri, range: vscode.Range) => {
            const key = `${uri.toString()}:${range.start.line}:${range.start.character}`;
            if (!seenLocations.has(key)) {
                seenLocations.add(key);
                locations.push(new vscode.Location(uri, range));
            }
        };

        // 1. Add the definition location (metadata.name field)
        const definitionLocation = await this.findDefinitionLocation(targetDoc);
        if (definitionLocation) {
            addLocation(definitionLocation.uri, definitionLocation.range);
        }

        // 2. Search all YAML files in the workspace for references
        const yamlFiles = await vscode.workspace.findFiles('**/*.{yaml,yml}', '**/node_modules/**');

        for (const fileUri of yamlFiles) {
            try {
                const document = await vscode.workspace.openTextDocument(fileUri);

                // Only search in Bino documents
                if (!await this.indexer.isBinoDocument(document)) {
                    continue;
                }

                const text = document.getText();
                const lines = text.split('\n');

                for (let lineNum = 0; lineNum < lines.length; lineNum++) {
                    const line = lines[lineNum];

                    // Find references to this document name
                    const refs = this.findReferencesInLine(line, lineNum, name, targetDoc.kind);
                    for (const ref of refs) {
                        addLocation(fileUri, ref);
                    }
                }
            } catch (err) {
                // Skip files that can't be read
                continue;
            }
        }

        return locations;
    }

    /**
     * Find references to a document name within a single line.
     */
    private findReferencesInLine(
        line: string,
        lineNum: number,
        name: string,
        kind: string
    ): vscode.Range[] {
        const ranges: vscode.Range[] = [];

        // Pattern-based search depending on the document kind
        switch (kind) {
            case 'DataSet':
                // dataset: <name>
                this.findFieldValueReference(line, lineNum, 'dataset', name, ranges);
                // dependencies array: - <name>
                this.findArrayItemReference(line, lineNum, name, ranges);
                break;

            case 'DataSource':
                // dataset: $<name> (with $ prefix)
                this.findFieldValueReference(line, lineNum, 'dataset', `$${name}`, ranges);
                // dependencies array: - <name> or - $<name>
                this.findArrayItemReference(line, lineNum, name, ranges);
                this.findArrayItemReference(line, lineNum, `$${name}`, ranges);
                break;

            case 'SigningProfile':
                // signingProfile: <name>
                this.findFieldValueReference(line, lineNum, 'signingProfile', name, ranges);
                break;

            case 'ConnectionSecret':
                // secret: <name>
                this.findFieldValueReference(line, lineNum, 'secret', name, ranges);
                break;

            case 'LayoutPage':
            case 'LayoutCard':
                // layout: <name>
                this.findFieldValueReference(line, lineNum, 'layout', name, ranges);
                break;

            case 'ReportArtefact':
                // report: <name>
                this.findFieldValueReference(line, lineNum, 'report', name, ranges);
                break;

            case 'Asset':
                // asset:<name> pattern
                this.findAssetReference(line, lineNum, name, ranges);
                break;
        }

        // Also check for metadata.name definition
        this.findMetadataNameDefinition(line, lineNum, name, ranges);

        return ranges;
    }

    /**
     * Find a field value reference like `field: value`.
     */
    private findFieldValueReference(
        line: string,
        lineNum: number,
        fieldName: string,
        value: string,
        ranges: vscode.Range[]
    ): void {
        const pattern = new RegExp(`^(\\s*${fieldName}:\\s*)${this.escapeRegExp(value)}(\\s*)$`);
        const match = line.match(pattern);
        if (match) {
            const startCol = match[1].length;
            const endCol = startCol + value.length;
            ranges.push(new vscode.Range(lineNum, startCol, lineNum, endCol));
        }
    }

    /**
     * Find an array item reference like `- value`.
     */
    private findArrayItemReference(
        line: string,
        lineNum: number,
        value: string,
        ranges: vscode.Range[]
    ): void {
        const pattern = new RegExp(`^(\\s*-\\s+)${this.escapeRegExp(value)}(\\s*)$`);
        const match = line.match(pattern);
        if (match) {
            const startCol = match[1].length;
            const endCol = startCol + value.length;
            ranges.push(new vscode.Range(lineNum, startCol, lineNum, endCol));
        }
    }

    /**
     * Find an asset reference like `asset:name`.
     */
    private findAssetReference(
        line: string,
        lineNum: number,
        name: string,
        ranges: vscode.Range[]
    ): void {
        const pattern = new RegExp(`asset:${this.escapeRegExp(name)}`, 'g');
        let match;
        while ((match = pattern.exec(line)) !== null) {
            // Start after 'asset:'
            const startCol = match.index + 6;
            const endCol = startCol + name.length;
            ranges.push(new vscode.Range(lineNum, startCol, lineNum, endCol));
        }
    }

    /**
     * Find a metadata.name definition like `name: value`.
     */
    private findMetadataNameDefinition(
        line: string,
        lineNum: number,
        value: string,
        ranges: vscode.Range[]
    ): void {
        const pattern = new RegExp(`^(\\s*name:\\s*)${this.escapeRegExp(value)}(\\s*)$`);
        const match = line.match(pattern);
        if (match) {
            const startCol = match[1].length;
            const endCol = startCol + value.length;
            ranges.push(new vscode.Range(lineNum, startCol, lineNum, endCol));
        }
    }

    /**
     * Find the definition location for a document (its metadata.name field).
     */
    private async findDefinitionLocation(doc: LSPDocument): Promise<vscode.Location | undefined> {
        try {
            const uri = vscode.Uri.file(doc.file);
            const document = await vscode.workspace.openTextDocument(uri);
            const text = document.getText();
            const lines = text.split('\n');

            // Find the document start line
            const docStartLine = this.findDocumentStartLine(lines, doc.position);

            // Search for the name field in this document
            for (let lineNum = docStartLine; lineNum < lines.length; lineNum++) {
                const line = lines[lineNum];
                const trimmed = line.trim();

                // Stop at next document separator
                if (lineNum > docStartLine && trimmed === '---') {
                    break;
                }

                // Look for `name: <doc.name>`
                const nameMatch = line.match(new RegExp(`^(\\s*name:\\s*)${this.escapeRegExp(doc.name)}(\\s*)$`));
                if (nameMatch) {
                    const startCol = nameMatch[1].length;
                    const endCol = startCol + doc.name.length;
                    const range = new vscode.Range(lineNum, startCol, lineNum, endCol);
                    return new vscode.Location(uri, range);
                }
            }
        } catch {
            // Ignore errors
        }

        return undefined;
    }

    /**
     * Find the line number where a document starts in a multi-doc YAML file.
     */
    private findDocumentStartLine(lines: string[], docIndex: number): number {
        let currentDocIndex = 0;

        for (let lineNum = 0; lineNum < lines.length; lineNum++) {
            const line = lines[lineNum].trim();

            if (lineNum === 0) {
                if (line === '---') {
                    continue;
                } else if (line && !line.startsWith('#')) {
                    currentDocIndex = 1;
                    if (currentDocIndex === docIndex) {
                        return 0;
                    }
                }
            } else if (line === '---') {
                currentDocIndex++;
                if (currentDocIndex === docIndex) {
                    return lineNum + 1;
                }
            } else if (currentDocIndex === 0 && line && !line.startsWith('#')) {
                currentDocIndex = 1;
                if (currentDocIndex === docIndex) {
                    return lineNum;
                }
            }
        }

        return 0;
    }

    /**
     * Get text at a specific range in a document.
     */
    private async getTextAtRange(uri: vscode.Uri, range: vscode.Range): Promise<string> {
        try {
            const document = await vscode.workspace.openTextDocument(uri);
            return document.getText(range);
        } catch {
            return '';
        }
    }

    /**
     * Escape special regex characters in a string.
     */
    private escapeRegExp(string: string): string {
        return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    /**
     * Get the indentation of a line.
     */
    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }
}

/**
 * Register the rename provider for Bino YAML documents.
 */
export function registerRenameProvider(
    context: vscode.ExtensionContext,
    indexer: WorkspaceIndexer
): void {
    const renameProvider = new BinoRenameProvider(indexer);

    const yamlSelector: vscode.DocumentSelector = [
        { language: 'yaml', scheme: 'file' },
        { language: 'yaml', scheme: 'untitled' }
    ];

    context.subscriptions.push(
        vscode.languages.registerRenameProvider(
            yamlSelector,
            renameProvider
        )
    );
}
