import * as vscode from 'vscode';
import { WorkspaceIndexer, LSPDocument } from './indexer';
import { getEmbeddableKinds } from './embeddable';
import { getBinoDocuments } from './binoDocuments';

/**
 * CodeLens provider for Bino YAML manifests.
 * Shows "Preview (100 rows)" button above DataSource and DataSet documents.
 */
export class BinoCodeLensProvider implements vscode.CodeLensProvider {
    private _onDidChangeCodeLenses: vscode.EventEmitter<void> = new vscode.EventEmitter<void>();
    readonly onDidChangeCodeLenses: vscode.Event<void> = this._onDidChangeCodeLenses.event;

    constructor(indexer: WorkspaceIndexer) {
        // Refresh CodeLenses when index updates (the embeddable kinds may change)
        indexer.onDidUpdateIndex(() => {
            this._onDidChangeCodeLenses.fire();
        });
    }

    /** Re-request lenses, e.g. once the language server is up. */
    refresh(): void {
        this._onDidChangeCodeLenses.fire();
    }

    async provideCodeLenses(
        document: vscode.TextDocument,
        token: vscode.CancellationToken
    ): Promise<vscode.CodeLens[]> {
        // Positions come from the language server's outline of the live buffer.
        const outline = await getBinoDocuments(document);
        if (!outline?.isBino || token.isCancellationRequested) {
            return [];
        }
        const embeddable = getEmbeddableKinds();
        const codeLenses: vscode.CodeLens[] = [];
        for (const info of outline.documents) {
            const line = info.range.start.line;
            const range = new vscode.Range(line, 0, line, 0);
            const doc: LSPDocument = {
                kind: info.kind,
                name: info.name,
                file: document.uri.fsPath,
                position: info.position
            };

            if (info.kind === 'DataSource' || info.kind === 'DataSet') {
                codeLenses.push(new vscode.CodeLens(range, {
                    title: '$(table) Preview (100 rows)',
                    command: 'bino.previewRows',
                    arguments: [doc],
                    tooltip: `Preview first 100 rows of ${doc.kind} "${doc.name}"`
                }));
            }

            if (embeddable.includes(info.kind)) {
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
        }
        return codeLenses;
    }

    dispose(): void {
        this._onDidChangeCodeLenses.dispose();
    }
}