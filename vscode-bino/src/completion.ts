import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { WorkspaceIndexer } from './indexer';

/**
 * Completion provider for Bino YAML manifests.
 * Provides intelligent completions for:
 * - dataset: references (DataSet names + $DataSource names)
 * - scenarios/variances: column names from referenced datasets
 * - signingProfile: SigningProfile names
 */
export class BinoCompletionProvider implements vscode.CompletionItemProvider {
    private indexer: WorkspaceIndexer;

    constructor(indexer: WorkspaceIndexer) {
        this.indexer = indexer;
    }

    async provideCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
        token: vscode.CancellationToken,
        context: vscode.CompletionContext
    ): Promise<vscode.CompletionItem[] | undefined> {
        // Only provide completions for Bino documents
        if (!await this.indexer.isBinoDocument(document)) {
            return undefined;
        }

        const line = document.lineAt(position.line).text;
        const linePrefix = line.substring(0, position.character);

        // Determine what kind of completion is needed based on context
        const completions = await this.getContextualCompletions(document, position, linePrefix);
        return completions;
    }

    private async getContextualCompletions(
        document: vscode.TextDocument,
        position: vscode.Position,
        linePrefix: string
    ): Promise<vscode.CompletionItem[] | undefined> {
        // Check for $file field completion (external query files)
        if (this.isFileReferenceField(linePrefix)) {
            return this.getFileReferenceCompletions(document, linePrefix);
        }

        // Check for query/prql field completion to suggest $file syntax
        if (this.isQueryField(linePrefix)) {
            return this.getQueryFieldCompletions();
        }

        // Check for dataset field completion
        if (this.isDatasetField(linePrefix)) {
            return this.getDatasetCompletions();
        }

        // Check for scenarios/variances array item completion
        if (this.isScenariosOrVariancesField(document, position)) {
            return await this.getColumnCompletions(document, position);
        }

        // Check for signingProfile field completion
        if (this.isSigningProfileField(linePrefix)) {
            return this.getSigningProfileCompletions();
        }

        // Check for kind field completion
        if (this.isKindField(linePrefix)) {
            return this.getKindCompletions();
        }

        // Check for source field in Image component
        if (this.isSourceField(linePrefix)) {
            return this.getAssetCompletions();
        }

        // Check for ref field in layout children
        if (this.isRefField(linePrefix)) {
            return this.getRefCompletions(document, position);
        }

        return undefined;
    }

    private isDatasetField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'dataset:' ||
            trimmed.startsWith('dataset: ') ||
            trimmed === '- ' && this.isInDatasetArray(linePrefix);
    }

    private isInDatasetArray(linePrefix: string): boolean {
        // This is a simplified check - in practice we'd need to look at parent context
        return false; // Will be enhanced with proper YAML parsing
    }

    private isScenariosOrVariancesField(document: vscode.TextDocument, position: vscode.Position): boolean {
        // Look backwards to find if we're in a scenarios or variances array
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 10; lineNum--) {
            const line = document.lineAt(lineNum).text;
            const trimmed = line.trim();

            if (trimmed.startsWith('scenarios:') || trimmed.startsWith('variances:')) {
                // Check if current line is indented more than this line (is a child)
                const currentIndent = this.getIndentation(document.lineAt(position.line).text);
                const parentIndent = this.getIndentation(line);
                if (currentIndent > parentIndent) {
                    return true;
                }
            }

            // Stop if we hit a different top-level key at same or less indentation
            if (trimmed.endsWith(':') && !trimmed.startsWith('-') && !trimmed.startsWith('#')) {
                const currentIndent = this.getIndentation(document.lineAt(position.line).text);
                const thisIndent = this.getIndentation(line);
                if (thisIndent <= currentIndent && lineNum !== position.line) {
                    break;
                }
            }
        }
        return false;
    }

    private isSigningProfileField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'signingProfile:' || trimmed.startsWith('signingProfile: ');
    }

    private isKindField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'kind:' || trimmed.startsWith('kind: ');
    }

    private isSourceField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'source:' || trimmed.startsWith('source: ');
    }

    private isRefField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'ref:' || trimmed.startsWith('ref: ');
    }

    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }

    private getDatasetCompletions(): vscode.CompletionItem[] {
        const names = this.indexer.getDatasetCompletions();
        return names.map(name => {
            const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.Reference);
            item.detail = name.startsWith('$') ? 'DataSource' : 'DataSet';
            item.sortText = name.startsWith('$') ? `1_${name}` : `0_${name}`; // DataSets first
            return item;
        });
    }

    private async getColumnCompletions(
        document: vscode.TextDocument,
        position: vscode.Position
    ): Promise<vscode.CompletionItem[]> {
        // Find the dataset reference for this component
        const datasetName = this.findDatasetReference(document, position);
        if (!datasetName) {
            return [];
        }

        const columns = await this.indexer.getColumns(datasetName);
        return columns.map(col => {
            const item = new vscode.CompletionItem(col, vscode.CompletionItemKind.Field);
            item.detail = `Column from ${datasetName}`;
            return item;
        });
    }

    private findDatasetReference(document: vscode.TextDocument, position: vscode.Position): string | undefined {
        // Look backwards to find the dataset field in the current component
        const text = document.getText();
        const lines = text.split('\n');

        // Find current component's indentation level
        let componentIndent = -1;
        for (let lineNum = position.line; lineNum >= 0; lineNum--) {
            const line = lines[lineNum];
            const trimmed = line.trim();

            // Look for dataset field
            if (trimmed.startsWith('dataset:')) {
                const indent = this.getIndentation(line);
                if (componentIndent === -1 || indent >= componentIndent - 2) {
                    const match = trimmed.match(/^dataset:\s*(.+)$/);
                    if (match) {
                        return match[1].trim();
                    }
                }
            }

            // Track component boundaries (kind field usually indicates component start)
            if (trimmed.startsWith('kind:')) {
                componentIndent = this.getIndentation(line);
            }
        }

        return undefined;
    }

    private getSigningProfileCompletions(): vscode.CompletionItem[] {
        const names = this.indexer.getDocumentNames(['SigningProfile']);
        return names.map(name => {
            const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.Reference);
            item.detail = 'SigningProfile';
            return item;
        });
    }

    private getAssetCompletions(): vscode.CompletionItem[] {
        const names = this.indexer.getAssetCompletions();
        return names.map(name => {
            const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.File);
            item.detail = 'Asset';
            item.documentation = new vscode.MarkdownString(`Reference to Asset document \`${name}\``);
            return item;
        });
    }

    /**
     * Get completions for the ref field in layout children.
     * Suggests document names that match the kind of the current child.
     */
    private getRefCompletions(
        document: vscode.TextDocument,
        position: vscode.Position
    ): vscode.CompletionItem[] {
        // Find the kind of the current layout child
        const childKind = this.findLayoutChildKind(document, position);
        if (!childKind) {
            // If we can't determine the kind, show all referenceable kinds
            return this.getAllRefCompletions();
        }

        // Get documents of the matching kind (excluding LayoutPage which can't be referenced)
        if (childKind === 'LayoutPage') {
            return []; // LayoutPage cannot be referenced
        }

        const docs = this.indexer.getDocuments([childKind]);
        return docs.map(doc => {
            const item = new vscode.CompletionItem(doc.name, vscode.CompletionItemKind.Reference);
            item.detail = childKind;
            item.documentation = new vscode.MarkdownString(
                `Reference to ${childKind} document \`${doc.name}\`\n\nDefined in: ${doc.file}`
            );
            return item;
        });
    }

    /**
     * Get all documents that can be referenced in layout children.
     */
    private getAllRefCompletions(): vscode.CompletionItem[] {
        const referenceableKinds = ['Text', 'Table', 'ChartStructure', 'ChartTime', 'LayoutCard', 'Image'];
        const items: vscode.CompletionItem[] = [];

        for (const kind of referenceableKinds) {
            const docs = this.indexer.getDocuments([kind]);
            for (const doc of docs) {
                const item = new vscode.CompletionItem(doc.name, vscode.CompletionItemKind.Reference);
                item.detail = kind;
                item.documentation = new vscode.MarkdownString(
                    `Reference to ${kind} document \`${doc.name}\`\n\nDefined in: ${doc.file}`
                );
                // Sort by kind then name
                item.sortText = `${kind}_${doc.name}`;
                items.push(item);
            }
        }

        return items;
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
            if (trimmed.startsWith('kind:') && lineIndent <= currentIndent) {
                const match = trimmed.match(/^kind:\s*["']?(\w+)["']?/);
                if (match) {
                    return match[1];
                }
            }

            // Stop if we hit a different block (children array start or parent object)
            if (lineIndent < currentIndent - 4 && trimmed && !trimmed.startsWith('#')) {
                break;
            }
        }

        return undefined;
    }

    private getKindCompletions(): vscode.CompletionItem[] {
        const kinds = [
            { name: 'Asset', description: 'Fonts, images, CSS files' },
            { name: 'DataSource', description: 'Raw data inputs (inline, file, S3, SQL)' },
            { name: 'DataSet', description: 'Transformed/queried data' },
            { name: 'Text', description: 'Rich text component' },
            { name: 'LayoutPage', description: 'Top-level page layout' },
            { name: 'LayoutCard', description: 'Nested layout container' },
            { name: 'ChartStructure', description: 'Structural charts' },
            { name: 'ChartTime', description: 'Time-series charts' },
            { name: 'Table', description: 'Tabular data display' },
            { name: 'ComponentStyle', description: 'Design tokens/CSS variables' },
            { name: 'Internationalization', description: 'Locale-specific translations' },
            { name: 'ReportArtefact', description: 'Output PDF definition' },
            { name: 'LiveReportArtefact', description: 'Interactive web report with routes' },
            { name: 'SigningProfile', description: 'PDF digital signing config' }
        ];

        return kinds.map(k => {
            const item = new vscode.CompletionItem(k.name, vscode.CompletionItemKind.Class);
            item.detail = k.description;
            item.documentation = new vscode.MarkdownString(`**${k.name}**\n\n${k.description}`);
            return item;
        });
    }

    /**
     * Check if we're in the $file field of a query/prql block.
     */
    private isFileReferenceField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === '$file:' || trimmed.startsWith('$file: ');
    }

    /**
     * Check if we're at a query or prql field that could use $file syntax.
     */
    private isQueryField(linePrefix: string): boolean {
        const trimmed = linePrefix.trim();
        return trimmed === 'query:' || trimmed === 'prql:';
    }

    /**
     * Get completions for the $file field - suggests SQL and PRQL files.
     */
    private getFileReferenceCompletions(
        document: vscode.TextDocument,
        linePrefix: string
    ): vscode.CompletionItem[] {
        const items: vscode.CompletionItem[] = [];
        const documentDir = path.dirname(document.uri.fsPath);
        const workspaceFolder = vscode.workspace.getWorkspaceFolder(document.uri);

        if (!workspaceFolder) {
            return items;
        }

        // Determine which file types to suggest based on context
        const trimmed = linePrefix.trim();
        const isPrqlContext = this.isInPrqlContext(document, document.positionAt(document.getText().indexOf(trimmed)));

        // Get the partial path the user has typed
        const currentValue = trimmed.replace(/^\$file:\s*/, '');

        // Find SQL and PRQL files in the workspace
        const extensions = isPrqlContext ? ['.prql'] : ['.sql', '.prql'];

        try {
            const files = this.findQueryFiles(workspaceFolder.uri.fsPath, extensions);

            for (const file of files) {
                // Calculate relative path from the document's directory
                let relativePath = path.relative(documentDir, file);

                // Normalize to forward slashes and add ./ prefix if needed
                relativePath = relativePath.replace(/\\/g, '/');
                if (!relativePath.startsWith('.') && !relativePath.startsWith('/')) {
                    relativePath = './' + relativePath;
                }

                // Filter by current input
                if (currentValue && !relativePath.toLowerCase().includes(currentValue.toLowerCase())) {
                    continue;
                }

                const ext = path.extname(file).toLowerCase();
                const item = new vscode.CompletionItem(relativePath, vscode.CompletionItemKind.File);
                item.detail = ext === '.sql' ? 'SQL query file' : 'PRQL query file';
                item.documentation = new vscode.MarkdownString(
                    `External query file.\n\nFull path: \`${file}\``
                );
                item.sortText = `0_${relativePath}`; // Sort files before directories

                items.push(item);
            }
        } catch {
            // Ignore errors when searching for files
        }

        return items;
    }

    /**
     * Check if we're in a PRQL context (inside a prql: block).
     */
    private isInPrqlContext(document: vscode.TextDocument, position: vscode.Position): boolean {
        // Look backwards to find if we're under a prql: key
        for (let lineNum = position.line; lineNum >= 0 && lineNum > position.line - 10; lineNum--) {
            const line = document.lineAt(lineNum).text.trim();
            if (line.startsWith('prql:')) {
                return true;
            }
            if (line.startsWith('query:')) {
                return false;
            }
        }
        return false;
    }

    /**
     * Find SQL and PRQL files in a directory recursively.
     */
    private findQueryFiles(dir: string, extensions: string[]): string[] {
        const files: string[] = [];
        const maxDepth = 5; // Limit search depth

        const search = (currentDir: string, depth: number) => {
            if (depth > maxDepth) {
                return;
            }

            try {
                const entries = fs.readdirSync(currentDir, { withFileTypes: true });

                for (const entry of entries) {
                    const fullPath = path.join(currentDir, entry.name);

                    // Skip hidden directories and common non-project directories
                    if (entry.isDirectory()) {
                        if (entry.name.startsWith('.') ||
                            entry.name === 'node_modules' ||
                            entry.name === 'vendor' ||
                            entry.name === 'dist' ||
                            entry.name === 'build') {
                            continue;
                        }
                        search(fullPath, depth + 1);
                    } else if (entry.isFile()) {
                        const ext = path.extname(entry.name).toLowerCase();
                        if (extensions.includes(ext)) {
                            files.push(fullPath);
                        }
                    }
                }
            } catch {
                // Ignore permission errors
            }
        };

        search(dir, 0);
        return files;
    }

    /**
     * Get completions for query/prql fields - suggests $file syntax.
     */
    private getQueryFieldCompletions(): vscode.CompletionItem[] {
        const items: vscode.CompletionItem[] = [];

        // Suggest $file object syntax as a snippet
        const fileRefItem = new vscode.CompletionItem('$file', vscode.CompletionItemKind.Snippet);
        fileRefItem.insertText = new vscode.SnippetString('\n  $file: ${1:./queries/query.sql}');
        fileRefItem.detail = 'Reference external SQL/PRQL file';
        fileRefItem.documentation = new vscode.MarkdownString(
            `Load query from an external file.\n\n` +
            `**Example:**\n` +
            `\`\`\`yaml\n` +
            `query:\n` +
            `  $file: ./queries/sales.sql\n` +
            `\`\`\`\n\n` +
            `The file path is relative to the manifest file. ` +
            `Changes to the referenced file automatically trigger cache invalidation and hot-reload.`
        );
        fileRefItem.sortText = '0_$file'; // Sort first
        items.push(fileRefItem);

        return items;
    }
}
