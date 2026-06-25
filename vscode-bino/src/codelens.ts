import * as vscode from 'vscode';
import { WorkspaceIndexer, LSPDocument } from './indexer';
import { getEmbeddableKinds } from './embeddable';

/**
 * CodeLens provider for Bino YAML manifests.
 * Shows "Preview (100 rows)" button above DataSource and DataSet documents.
 */
export class BinoCodeLensProvider implements vscode.CodeLensProvider {
    private indexer: WorkspaceIndexer;

    private _onDidChangeCodeLenses: vscode.EventEmitter<void> = new vscode.EventEmitter<void>();
    readonly onDidChangeCodeLenses: vscode.Event<void> = this._onDidChangeCodeLenses.event;

    constructor(indexer: WorkspaceIndexer) {
        this.indexer = indexer;

        // Refresh CodeLenses when index updates
        indexer.onDidUpdateIndex(() => {
            this._onDidChangeCodeLenses.fire();
        });
    }

    async provideCodeLenses(
        document: vscode.TextDocument,
        token: vscode.CancellationToken
    ): Promise<vscode.CodeLens[]> {
        const codeLenses: vscode.CodeLens[] = [];

        // Quick check: is this a YAML file with Bino content?
        const text = document.getText();
        if (!text.includes('apiVersion: bino.bi') &&
            !text.includes('apiVersion: "bino.bi') &&
            !text.includes("apiVersion: 'bino.bi")) {
            return codeLenses;
        }

        const filePath = document.uri.fsPath;

        // Get all DataSource/DataSet documents from the index
        const allDocs = this.indexer.getDocuments(['DataSource', 'DataSet']);

        // Filter to documents in this file (normalize paths for comparison)
        const fileDocs = allDocs.filter(doc => {
            // Normalize both paths for comparison
            const docPath = doc.file.replace(/\\/g, '/');
            const currentPath = filePath.replace(/\\/g, '/');
            return docPath === currentPath;
        });

        const lines = text.split('\n');

        // If no indexed datasets, fall back to parsing them inline from the document.
        if (fileDocs.length === 0) {
            codeLenses.push(...this.parseCodeLensesFromDocument(document, text));
        }

        // For each DataSource/DataSet in this file, find the apiVersion line and add CodeLens
        for (const doc of fileDocs) {
            if (token.isCancellationRequested) {
                break;
            }

            const lineNumber = this.findApiVersionLine(lines, doc.position);
            if (lineNumber === -1) {
                continue;
            }

            const range = new vscode.Range(lineNumber, 0, lineNumber, 0);

            const codeLens = new vscode.CodeLens(range, {
                title: '$(table) Preview (100 rows)',
                command: 'bino.previewRows',
                arguments: [doc],
                tooltip: `Preview first 100 rows of ${doc.kind} "${doc.name}"`
            });

            codeLenses.push(codeLens);

            // A DataSet is form-editable in the Designer (declarative transforms);
            // it has no render canvas, so only this lens (not the embedded-preview
            // one below, which is keyed on getEmbeddableKinds()).
            if (doc.kind === 'DataSet') {
                codeLenses.push(new vscode.CodeLens(range, {
                    title: '$(edit) Designer',
                    command: 'bino.openDesigner',
                    arguments: [doc],
                    tooltip: `Open DataSet "${doc.name}" in the Bino Designer`
                }));
            }
        }

        // For each embeddable document in this file, add an embedded-preview CodeLens
        const artefactDocs = this.indexer
            .getDocuments(getEmbeddableKinds())
            .filter(doc => doc.file.replace(/\\/g, '/') === filePath.replace(/\\/g, '/'));

        for (const doc of artefactDocs) {
            if (token.isCancellationRequested) {
                break;
            }

            const lineNumber = this.findApiVersionLine(lines, doc.position);
            if (lineNumber === -1) {
                continue;
            }

            const range = new vscode.Range(lineNumber, 0, lineNumber, 0);

            codeLenses.push(new vscode.CodeLens(range, {
                title: '$(preview) Preview (Embedded)',
                command: 'bino.previewArtefactEmbedded',
                arguments: [doc],
                tooltip: `Open embedded preview of ${doc.kind} "${doc.name}"`
            }));

            // Sibling lens on the same range: open this manifest in the visual
            // designer (schema-driven form + live canvas). Renders next to the
            // embedded-preview lens.
            codeLenses.push(new vscode.CodeLens(range, {
                title: '$(edit) Designer',
                command: 'bino.openDesigner',
                arguments: [doc],
                tooltip: `Open ${doc.kind} "${doc.name}" in the Bino Designer`
            }));
        }

        return codeLenses;
    }

    /**
     * Fallback: parse DataSource/DataSet documents directly from document text
     * when the index hasn't been populated yet.
     */
    private parseCodeLensesFromDocument(
        document: vscode.TextDocument,
        text: string
    ): vscode.CodeLens[] {
        const codeLenses: vscode.CodeLens[] = [];
        const lines = text.split('\n');
        const filePath = document.uri.fsPath;

        let currentDocStart = -1;
        let currentDocPosition = 0;
        let inBinoDoc = false;
        let currentKind = '';
        let currentName = '';
        let apiVersionLine = -1;

        for (let lineNum = 0; lineNum < lines.length; lineNum++) {
            const line = lines[lineNum];
            const trimmed = line.trim();

            // Check for document separator or start
            if (trimmed === '---' || (lineNum === 0 && trimmed && !trimmed.startsWith('#'))) {
                // Save previous doc if it was a DataSource/DataSet
                if (inBinoDoc && (currentKind === 'DataSource' || currentKind === 'DataSet') && apiVersionLine !== -1) {
                    const doc: LSPDocument = {
                        kind: currentKind,
                        name: currentName || 'unknown',
                        file: filePath,
                        position: currentDocPosition
                    };
                    const range = new vscode.Range(apiVersionLine, 0, apiVersionLine, 0);
                    codeLenses.push(new vscode.CodeLens(range, {
                        title: '$(table) Preview (100 rows)',
                        command: 'bino.previewRows',
                        arguments: [doc],
                        tooltip: `Preview first 100 rows of ${currentKind} "${currentName}"`
                    }));
                }

                // Reset for new document
                if (trimmed === '---') {
                    currentDocPosition++;
                    currentDocStart = lineNum + 1;
                } else {
                    currentDocPosition = 1;
                    currentDocStart = lineNum;
                }
                inBinoDoc = false;
                currentKind = '';
                currentName = '';
                apiVersionLine = -1;
            }

            // Look for apiVersion (marks start of Bino doc)
            if (trimmed.startsWith('apiVersion:') && trimmed.includes('bino.bi')) {
                inBinoDoc = true;
                apiVersionLine = lineNum;
                if (currentDocPosition === 0) {
                    currentDocPosition = 1;
                }
            }

            // Look for kind
            if (inBinoDoc && trimmed.startsWith('kind:')) {
                const match = trimmed.match(/^kind:\s*(.+)$/);
                if (match) {
                    currentKind = match[1].trim();
                }
            }

            // Look for metadata.name
            if (inBinoDoc && trimmed.startsWith('name:')) {
                // Simple heuristic: if we're in the metadata section
                const match = trimmed.match(/^name:\s*(.+)$/);
                if (match && !currentName) {
                    currentName = match[1].trim();
                }
            }
        }

        // Don't forget the last document
        if (inBinoDoc && (currentKind === 'DataSource' || currentKind === 'DataSet') && apiVersionLine !== -1) {
            const doc: LSPDocument = {
                kind: currentKind,
                name: currentName || 'unknown',
                file: filePath,
                position: currentDocPosition
            };
            const range = new vscode.Range(apiVersionLine, 0, apiVersionLine, 0);
            codeLenses.push(new vscode.CodeLens(range, {
                title: '$(table) Preview (100 rows)',
                command: 'bino.previewRows',
                arguments: [doc],
                tooltip: `Preview first 100 rows of ${currentKind} "${currentName}"`
            }));
        }

        return codeLenses;
    }

    /**
     * Find the line number of the apiVersion field for a specific document.
     * @param lines All lines in the file
     * @param docPosition 1-based document index in multi-doc YAML
     * @returns 0-based line number, or -1 if not found
     */
    private findApiVersionLine(lines: string[], docPosition: number): number {
        let currentDocIndex = 0;
        let inTargetDoc = false;

        for (let lineNum = 0; lineNum < lines.length; lineNum++) {
            const line = lines[lineNum];
            const trimmed = line.trim();

            // Skip empty lines and comments when not in a document yet
            if (currentDocIndex === 0 && (!trimmed || trimmed.startsWith('#'))) {
                continue;
            }

            // Check for document separator
            if (trimmed === '---') {
                // If we were in target doc, we've passed it without finding apiVersion
                if (inTargetDoc) {
                    return -1;
                }
                currentDocIndex++;
                inTargetDoc = currentDocIndex === docPosition;
                continue;
            }

            // First non-comment, non-empty line starts doc 1 if we haven't started yet
            if (currentDocIndex === 0 && trimmed && !trimmed.startsWith('#')) {
                currentDocIndex = 1;
                inTargetDoc = currentDocIndex === docPosition;
            }

            // Look for apiVersion in the target document
            if (inTargetDoc && trimmed.startsWith('apiVersion:')) {
                return lineNum;
            }
        }

        return -1;
    }

    dispose(): void {
        this._onDidChangeCodeLenses.dispose();
    }
}