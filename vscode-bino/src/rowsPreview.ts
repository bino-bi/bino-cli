import * as vscode from 'vscode';
import { WorkspaceIndexer, LSPDocument, LSPRowsResult } from './indexer';

/**
 * Manages the rows preview webview panel for DataSource/DataSet previews.
 */
export class RowsPreviewManager {
    private panel: vscode.WebviewPanel | undefined;
    private indexer: WorkspaceIndexer;
    private outputChannel: vscode.OutputChannel;
    private currentDoc: LSPDocument | undefined;

    constructor(indexer: WorkspaceIndexer, outputChannel: vscode.OutputChannel) {
        this.indexer = indexer;
        this.outputChannel = outputChannel;
    }

    /**
     * Show preview for a DataSource or DataSet document.
     */
    async showPreview(doc: LSPDocument): Promise<void> {
        this.currentDoc = doc;

        // Create or reveal panel
        if (this.panel) {
            this.panel.reveal(vscode.ViewColumn.Beside);
        } else {
            this.panel = vscode.window.createWebviewPanel(
                'binoRowsPreview',
                `Preview: ${doc.name}`,
                vscode.ViewColumn.Beside,
                {
                    enableScripts: true,
                    retainContextWhenHidden: true
                }
            );

            this.panel.onDidDispose(() => {
                this.panel = undefined;
            });
        }

        // Update title
        this.panel.title = `Preview: ${doc.name}`;

        // Show loading state
        this.panel.webview.html = this.getLoadingHtml(doc);

        // Fetch rows
        const result = await this.indexer.getRows(doc.name, 100);

        if (!result) {
            this.panel.webview.html = this.getErrorHtml(doc, 'Failed to fetch rows. Check the output channel for details.');
            return;
        }

        if (result.error) {
            this.panel.webview.html = this.getErrorHtml(doc, result.error);
            return;
        }

        // Show results
        this.panel.webview.html = this.getResultHtml(doc, result);
    }

    private getLoadingHtml(doc: LSPDocument): string {
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Loading...</title>
    <style>
        ${this.getBaseStyles()}
        .loading {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            height: 100vh;
            gap: 16px;
        }
        .spinner {
            width: 40px;
            height: 40px;
            border: 3px solid var(--vscode-foreground);
            border-top-color: transparent;
            border-radius: 50%;
            animation: spin 1s linear infinite;
        }
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <div class="loading">
        <div class="spinner"></div>
        <div>Loading rows from ${this.escapeHtml(doc.kind)} "${this.escapeHtml(doc.name)}"...</div>
    </div>
</body>
</html>`;
    }

    private getErrorHtml(doc: LSPDocument, error: string): string {
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Error</title>
    <style>
        ${this.getBaseStyles()}
        .error-container {
            padding: 20px;
        }
        .error-title {
            color: var(--vscode-errorForeground);
            font-size: 1.2em;
            margin-bottom: 16px;
        }
        .error-message {
            background: var(--vscode-inputValidation-errorBackground);
            border: 1px solid var(--vscode-inputValidation-errorBorder);
            padding: 12px;
            border-radius: 4px;
            font-family: var(--vscode-editor-font-family), monospace;
            white-space: pre-wrap;
            word-break: break-word;
        }
    </style>
</head>
<body>
    <div class="error-container">
        <div class="error-title">Failed to preview ${this.escapeHtml(doc.kind)} "${this.escapeHtml(doc.name)}"</div>
        <div class="error-message">${this.escapeHtml(error)}</div>
    </div>
</body>
</html>`;
    }

    private getResultHtml(doc: LSPDocument, result: LSPRowsResult): string {
        const rowCount = result.rows.length;
        const truncatedNote = result.truncated
            ? `<span class="truncated">(showing first ${result.limit} rows)</span>`
            : '';

        let tableHtml = '';
        if (result.columns.length > 0 && result.rows.length > 0) {
            // Build header
            const headerCells = result.columns.map(col =>
                `<th>${this.escapeHtml(col)}</th>`
            ).join('');

            // Build rows
            const rowsHtml = result.rows.map((row, idx) => {
                const cells = result.columns.map(col => {
                    const value = row[col];
                    const displayValue = this.formatValue(value);
                    return `<td>${displayValue}</td>`;
                }).join('');
                return `<tr><td class="row-num">${idx + 1}</td>${cells}</tr>`;
            }).join('');

            tableHtml = `
                <div class="table-container">
                    <table>
                        <thead>
                            <tr>
                                <th class="row-num-header">#</th>
                                ${headerCells}
                            </tr>
                        </thead>
                        <tbody>
                            ${rowsHtml}
                        </tbody>
                    </table>
                </div>
            `;
        } else if (result.columns.length > 0) {
            tableHtml = '<div class="no-data">No rows returned</div>';
        } else {
            tableHtml = '<div class="no-data">No columns found</div>';
        }

        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Preview: ${this.escapeHtml(doc.name)}</title>
    <style>
        ${this.getBaseStyles()}
        .header {
            padding: 12px 16px;
            border-bottom: 1px solid var(--vscode-panel-border);
            display: flex;
            align-items: center;
            gap: 12px;
            flex-wrap: wrap;
        }
        .header h1 {
            font-size: 1.1em;
            margin: 0;
            font-weight: 500;
        }
        .kind-badge {
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
            padding: 2px 8px;
            border-radius: 10px;
            font-size: 0.85em;
        }
        .row-count {
            color: var(--vscode-descriptionForeground);
            font-size: 0.9em;
        }
        .truncated {
            color: var(--vscode-editorWarning-foreground);
            font-size: 0.85em;
        }
        .table-container {
            overflow: auto;
            max-height: calc(100vh - 60px);
        }
        table {
            border-collapse: collapse;
            width: 100%;
            font-size: 0.9em;
        }
        th, td {
            padding: 6px 12px;
            text-align: left;
            border-bottom: 1px solid var(--vscode-panel-border);
            max-width: 300px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        th {
            background: var(--vscode-editor-background);
            position: sticky;
            top: 0;
            font-weight: 600;
            border-bottom: 2px solid var(--vscode-panel-border);
        }
        tr:hover td {
            background: var(--vscode-list-hoverBackground);
        }
        .row-num, .row-num-header {
            color: var(--vscode-descriptionForeground);
            text-align: right;
            width: 40px;
            min-width: 40px;
        }
        .null-value {
            color: var(--vscode-descriptionForeground);
            font-style: italic;
        }
        .number-value {
            font-family: var(--vscode-editor-font-family), monospace;
        }
        .no-data {
            padding: 40px;
            text-align: center;
            color: var(--vscode-descriptionForeground);
        }
    </style>
</head>
<body>
    <div class="header">
        <span class="kind-badge">${this.escapeHtml(doc.kind)}</span>
        <h1>${this.escapeHtml(doc.name)}</h1>
        <span class="row-count">${rowCount} row${rowCount !== 1 ? 's' : ''}</span>
        ${truncatedNote}
    </div>
    ${tableHtml}
</body>
</html>`;
    }

    private getBaseStyles(): string {
        return `
            body {
                margin: 0;
                padding: 0;
                font-family: var(--vscode-font-family);
                font-size: var(--vscode-font-size);
                color: var(--vscode-foreground);
                background: var(--vscode-editor-background);
            }
        `;
    }

    private escapeHtml(str: string): string {
        return str
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }

    private formatValue(value: unknown): string {
        if (value === null || value === undefined) {
            return '<span class="null-value">null</span>';
        }
        if (typeof value === 'number') {
            return `<span class="number-value">${value}</span>`;
        }
        if (typeof value === 'boolean') {
            return value ? 'true' : 'false';
        }
        if (typeof value === 'object') {
            try {
                const json = JSON.stringify(value);
                // Truncate long JSON
                if (json.length > 100) {
                    return this.escapeHtml(json.substring(0, 100) + '...');
                }
                return this.escapeHtml(json);
            } catch {
                return this.escapeHtml(String(value));
            }
        }
        const str = String(value);
        // Truncate long strings
        if (str.length > 200) {
            return this.escapeHtml(str.substring(0, 200) + '...');
        }
        return this.escapeHtml(str);
    }

    dispose(): void {
        if (this.panel) {
            this.panel.dispose();
        }
    }
}
