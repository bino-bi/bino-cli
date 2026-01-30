import * as vscode from 'vscode';
import { WorkspaceIndexer } from './indexer';

/**
 * Hover provider for Bino YAML manifests.
 * Shows column information when hovering over dataset/datasource references.
 */
export class BinoHoverProvider implements vscode.HoverProvider {
    private indexer: WorkspaceIndexer;

    constructor(indexer: WorkspaceIndexer) {
        this.indexer = indexer;
    }

    async provideHover(
        document: vscode.TextDocument,
        position: vscode.Position,
        token: vscode.CancellationToken
    ): Promise<vscode.Hover | undefined> {
        // Only provide hovers for Bino documents
        if (!await this.indexer.isBinoDocument(document)) {
            return undefined;
        }

        const line = document.lineAt(position.line).text;

        // Check for layoutPages pattern hover
        const layoutPagesHover = this.getLayoutPagesHover(document, position, line);
        if (layoutPagesHover) {
            return layoutPagesHover;
        }

        // Try to extract dataset name from different patterns
        let datasetName: string | undefined;

        // Pattern 1: dataset: value (single value)
        const singleMatch = line.match(/^\s*dataset:\s*(.+?)\s*$/);
        if (singleMatch) {
            datasetName = singleMatch[1].trim();
        }

        // Pattern 2: array item under dataset: (e.g., "- ppl_ds")
        if (!datasetName) {
            const arrayItemMatch = line.match(/^\s*-\s+(.+?)\s*$/);
            if (arrayItemMatch) {
                // Check if we're in a dataset array by looking backwards
                if (this.isInDatasetArray(document, position)) {
                    datasetName = arrayItemMatch[1].trim();
                }
            }
        }

        if (!datasetName) {
            return undefined;
        }

        // Get the word range to ensure we're hovering over the value, not the key
        const wordRange = document.getWordRangeAtPosition(position, /[\w$-]+/);
        if (!wordRange) {
            return undefined;
        }

        const hoveredWord = document.getText(wordRange);

        // Check if we're hovering over the dataset name (not the "dataset" key)
        if (hoveredWord === 'dataset') {
            return undefined;
        }

        // Verify the hovered word is part of the dataset name
        if (!datasetName.includes(hoveredWord)) {
            return undefined;
        }

        // Fetch columns for this dataset/datasource
        const columns = await this.indexer.getColumns(datasetName);

        if (columns.length === 0) {
            const kind = datasetName.startsWith('$') ? 'DataSource' : 'DataSet';
            return new vscode.Hover(
                new vscode.MarkdownString(`**${kind}:** \`${datasetName}\`\n\n_No columns found_`)
            );
        }

        // Build markdown content
        const kind = datasetName.startsWith('$') ? 'DataSource' : 'DataSet';
        const md = new vscode.MarkdownString();
        md.isTrusted = true;

        md.appendMarkdown(`**${kind}:** \`${datasetName}\`\n\n`);
        md.appendMarkdown(`**Columns** (${columns.length}):\n\n`);

        // Format columns as a table or list
        if (columns.length <= 20) {
            // For smaller datasets, show as a simple list
            md.appendMarkdown('| Column |\n|---|\n');
            for (const col of columns) {
                md.appendMarkdown(`| \`${col}\` |\n`);
            }
        } else {
            // For larger datasets, show as comma-separated with truncation
            const displayCols = columns.slice(0, 30);
            md.appendMarkdown(displayCols.map(c => `\`${c}\``).join(', '));
            if (columns.length > 30) {
                md.appendMarkdown(`\n\n_...and ${columns.length - 30} more columns_`);
            }
        }

        return new vscode.Hover(md, wordRange);
    }

    /**
     * Check if the current position is inside a dataset array.
     * Looks backwards to find "dataset:" followed by array items.
     */
    private isInDatasetArray(document: vscode.TextDocument, position: vscode.Position): boolean {
        const currentLine = document.lineAt(position.line).text;
        const currentIndent = this.getIndentation(currentLine);

        // Look backwards to find the parent key
        for (let lineNum = position.line - 1; lineNum >= 0 && lineNum > position.line - 15; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();
            const lineIndent = this.getIndentation(line);

            // If we find "dataset:" at a lower indentation level, we're in a dataset array
            if (trimmed === 'dataset:' && lineIndent < currentIndent) {
                return true;
            }

            // If we hit another key at the same or lower indentation, stop searching
            if (trimmed.endsWith(':') && !trimmed.startsWith('-') && lineIndent <= currentIndent) {
                if (!trimmed.startsWith('dataset:')) {
                    return false;
                }
            }

            // If we hit a line at lower indentation that's not dataset:, stop
            if (lineIndent < currentIndent && trimmed && !trimmed.startsWith('-') && !trimmed.startsWith('#')) {
                return false;
            }
        }

        return false;
    }

    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }

    /**
     * Get hover information for layoutPages patterns.
     * Shows which LayoutPages match the pattern.
     */
    private getLayoutPagesHover(
        document: vscode.TextDocument,
        position: vscode.Position,
        line: string
    ): vscode.Hover | undefined {
        // Check if we're in a layoutPages context
        if (!this.isInLayoutPagesContext(document, position)) {
            return undefined;
        }

        // Get the word range
        const wordRange = document.getWordRangeAtPosition(position, /[\w*?-]+/);
        if (!wordRange) {
            return undefined;
        }

        const pattern = document.getText(wordRange);

        // Skip if hovering over "layoutPages" key itself
        if (pattern === 'layoutPages') {
            return undefined;
        }

        // Get all LayoutPage documents
        const layoutPages = this.indexer.getDocuments(['LayoutPage']);

        // Find matching pages using glob-like matching
        const matchingPages = layoutPages.filter(page => this.matchPattern(pattern, page.name));

        const md = new vscode.MarkdownString();
        md.isTrusted = true;

        if (pattern === '*') {
            md.appendMarkdown(`**Pattern:** \`*\` (all pages)\n\n`);
            md.appendMarkdown(`**Matches ${matchingPages.length} LayoutPage(s):**\n\n`);
        } else if (/[*?\[\]]/.test(pattern)) {
            md.appendMarkdown(`**Glob Pattern:** \`${pattern}\`\n\n`);
            md.appendMarkdown(`**Matches ${matchingPages.length} LayoutPage(s):**\n\n`);
        } else {
            // Exact match
            const found = matchingPages.length > 0;
            md.appendMarkdown(`**LayoutPage:** \`${pattern}\`\n\n`);
            if (!found) {
                md.appendMarkdown(`⚠️ _No LayoutPage with this name found_`);
                return new vscode.Hover(md, wordRange);
            }
        }

        if (matchingPages.length > 0) {
            // Show matching pages
            const displayPages = matchingPages.slice(0, 15);
            for (const page of displayPages) {
                md.appendMarkdown(`- \`${page.name}\` _(${page.file.split('/').pop()})_\n`);
            }
            if (matchingPages.length > 15) {
                md.appendMarkdown(`\n_...and ${matchingPages.length - 15} more_`);
            }
        } else if (/[*?\[\]]/.test(pattern)) {
            md.appendMarkdown(`_No matching LayoutPages found_`);
        }

        return new vscode.Hover(md, wordRange);
    }

    /**
     * Simple glob-like pattern matching.
     * Supports: * (any chars), ? (single char), exact match
     */
    private matchPattern(pattern: string, name: string): boolean {
        // Convert glob pattern to regex
        const regexStr = pattern
            .replace(/[.+^${}()|[\]\\]/g, '\\$&') // Escape special regex chars (except * and ?)
            .replace(/\*/g, '.*')                  // * -> .*
            .replace(/\?/g, '.');                  // ? -> .

        try {
            const regex = new RegExp(`^${regexStr}$`);
            return regex.test(name);
        } catch {
            return pattern === name; // Fallback to exact match
        }
    }

    /**
     * Check if the current position is in a layoutPages context.
     */
    private isInLayoutPagesContext(document: vscode.TextDocument, position: vscode.Position): boolean {
        const line = document.lineAt(position.line).text;
        const trimmed = line.trim();

        // Direct layoutPages field
        if (trimmed.startsWith('layoutPages:')) {
            return true;
        }

        // Array item under layoutPages
        if (trimmed.startsWith('-')) {
            const currentIndent = this.getIndentation(line);

            // Look backwards to find layoutPages:
            for (let lineNum = position.line - 1; lineNum >= 0 && lineNum > position.line - 15; lineNum--) {
                const prevLine = document.lineAt(lineNum).text;
                const prevTrimmed = prevLine.trim();
                const prevIndent = this.getIndentation(prevLine);

                if (prevTrimmed.startsWith('layoutPages:') && prevIndent < currentIndent) {
                    return true;
                }

                // Stop if we hit another key at same or lower indentation
                if (prevTrimmed.endsWith(':') && !prevTrimmed.startsWith('-') && prevIndent <= currentIndent) {
                    if (!prevTrimmed.startsWith('layoutPages:')) {
                        return false;
                    }
                }
            }
        }

        return false;
    }
}
