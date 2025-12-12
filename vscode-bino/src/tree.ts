import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { WorkspaceIndexer, LSPDocument } from './indexer';
import { BinoValidator } from './validation';

/**
 * Represents an inline child component within a LayoutPage or LayoutCard.
 */
export interface InlineChild {
    kind: string;
    name: string;
    file: string;
    line: number;  // 0-based line number
    children?: InlineChild[];
}

/**
 * Tree item representing either a kind group, a document, or an inline child.
 */
export class BinoTreeItem extends vscode.TreeItem {
    public readonly inlineChild?: InlineChild;

    constructor(
        public readonly label: string,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState,
        public readonly kind?: string,
        public readonly document?: LSPDocument,
        inlineChild?: InlineChild
    ) {
        super(label, collapsibleState);
        this.inlineChild = inlineChild;

        if (inlineChild) {
            // This is an inline child component
            this.contextValue = 'binoInlineChild';
            this.description = inlineChild.kind;
            this.tooltip = new vscode.MarkdownString(
                `**${inlineChild.name}**\n\n` +
                `Kind: \`${inlineChild.kind}\`\n\n` +
                `Line: ${inlineChild.line + 1}`
            );
            this.iconPath = this.getDocumentIcon(inlineChild.kind);
            // Set resourceUri so VS Code shows Problems badges
            this.resourceUri = vscode.Uri.file(inlineChild.file);

            // Command to open file at line on click
            this.command = {
                command: 'bino.openInlineChild',
                title: 'Open Component',
                arguments: [inlineChild]
            };
        } else if (document) {
            // This is a document item
            this.contextValue = 'binoDocument';
            this.description = this.getRelativePath(document.file);
            this.tooltip = new vscode.MarkdownString(
                `**${document.name}**\n\n` +
                `Kind: \`${document.kind}\`\n\n` +
                `File: \`${document.file}\``
            );
            this.iconPath = this.getDocumentIcon(document.kind);
            // Set resourceUri so VS Code shows Problems badges
            this.resourceUri = vscode.Uri.file(document.file);

            // Command to open document on click
            this.command = {
                command: 'bino.openDocument',
                title: 'Open Document',
                arguments: [document]
            };
        } else if (kind) {
            // This is a kind group
            this.contextValue = 'binoKindGroup';
            this.iconPath = this.getKindIcon(kind);
            this.tooltip = `${kind} documents`;
        }
    }

    private getRelativePath(filePath: string): string {
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (workspaceFolders && workspaceFolders.length > 0) {
            const workspaceRoot = workspaceFolders[0].uri.fsPath;
            if (filePath.startsWith(workspaceRoot)) {
                return filePath.substring(workspaceRoot.length + 1);
            }
        }
        return path.basename(filePath);
    }

    private getDocumentIcon(kind: string): vscode.ThemeIcon {
        switch (kind) {
            case 'DataSource':
                return new vscode.ThemeIcon('database');
            case 'DataSet':
                return new vscode.ThemeIcon('table');
            case 'ReportArtefact':
                return new vscode.ThemeIcon('file-pdf');
            case 'SigningProfile':
                return new vscode.ThemeIcon('key');
            case 'Asset':
                return new vscode.ThemeIcon('file-media');
            case 'LayoutPage':
                return new vscode.ThemeIcon('layout');
            case 'LayoutCard':
                return new vscode.ThemeIcon('credit-card');
            case 'Table':
                return new vscode.ThemeIcon('list-flat');
            case 'Text':
                return new vscode.ThemeIcon('symbol-text');
            case 'ChartStructure':
            case 'ChartTime':
                return new vscode.ThemeIcon('graph');
            case 'ComponentStyle':
                return new vscode.ThemeIcon('paintcan');
            case 'Internationalization':
                return new vscode.ThemeIcon('globe');
            case 'ConnectionSecret':
                return new vscode.ThemeIcon('lock');
            default:
                return new vscode.ThemeIcon('file-code');
        }
    }

    private getKindIcon(kind: string): vscode.ThemeIcon {
        return this.getDocumentIcon(kind);
    }
}

/**
 * Tree data provider for the Bino Explorer view.
 * Groups documents by kind and allows navigation to definitions.
 */
export class BinoTreeProvider implements vscode.TreeDataProvider<BinoTreeItem> {
    private _onDidChangeTreeData: vscode.EventEmitter<BinoTreeItem | undefined | null | void> =
        new vscode.EventEmitter<BinoTreeItem | undefined | null | void>();
    readonly onDidChangeTreeData: vscode.Event<BinoTreeItem | undefined | null | void> =
        this._onDidChangeTreeData.event;

    private indexer: WorkspaceIndexer;
    private validator?: BinoValidator;

    constructor(indexer: WorkspaceIndexer, validator?: BinoValidator) {
        this.indexer = indexer;
        this.validator = validator;

        // Listen for index updates
        indexer.onDidUpdateIndex(() => {
            this.refresh();
        });

        // Listen for diagnostics changes to update badges
        if (validator) {
            validator.onDidChangeDiagnostics(() => {
                this.refresh();
            });
        }
    }

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(element: BinoTreeItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: BinoTreeItem): Promise<BinoTreeItem[]> {
        if (!element) {
            // Root level: return kind groups
            return this.getKindGroups();
        }

        if (element.kind && !element.document && !element.inlineChild) {
            // Kind group: return documents of this kind
            return this.getDocumentsForKind(element.kind);
        }

        if (element.document && (element.document.kind === 'LayoutPage' || element.document.kind === 'LayoutCard')) {
            // LayoutPage or LayoutCard: return inline children
            return await this.getInlineChildren(element.document);
        }

        if (element.inlineChild && element.inlineChild.children && element.inlineChild.children.length > 0) {
            // Inline child with its own children
            return element.inlineChild.children.map(child => {
                const hasChildren = child.children && child.children.length > 0;
                return new BinoTreeItem(
                    child.name,
                    hasChildren ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None,
                    undefined,
                    undefined,
                    child
                );
            });
        }

        return [];
    }

    private getKindGroups(): BinoTreeItem[] {
        const documents = this.indexer.getDocuments();

        if (documents.length === 0) {
            // Return a placeholder item when no documents indexed
            const placeholder = new BinoTreeItem(
                'No Bino documents found',
                vscode.TreeItemCollapsibleState.None
            );
            placeholder.description = 'Waiting for index...';
            placeholder.iconPath = new vscode.ThemeIcon('info');
            return [placeholder];
        }

        // Group documents by kind and count
        const kindCounts = new Map<string, number>();
        for (const doc of documents) {
            kindCounts.set(doc.kind, (kindCounts.get(doc.kind) || 0) + 1);
        }

        // Sort kinds by priority order for better UX
        const kindOrder = [
            'ReportArtefact',
            'DataSource',
            'DataSet',
            'LayoutPage',
            'LayoutCard',
            'Table',
            'Text',
            'ChartStructure',
            'ChartTime',
            'Asset',
            'ComponentStyle',
            'Internationalization',
            'SigningProfile',
            'ConnectionSecret'
        ];

        const sortedKinds = Array.from(kindCounts.keys()).sort((a, b) => {
            const aIndex = kindOrder.indexOf(a);
            const bIndex = kindOrder.indexOf(b);
            if (aIndex === -1 && bIndex === -1) return a.localeCompare(b);
            if (aIndex === -1) return 1;
            if (bIndex === -1) return -1;
            return aIndex - bIndex;
        });

        return sortedKinds.map(kind => {
            const count = kindCounts.get(kind) || 0;
            const item = new BinoTreeItem(
                kind,
                vscode.TreeItemCollapsibleState.Collapsed,
                kind
            );
            item.description = `${count}`;
            return item;
        });
    }

    private getDocumentsForKind(kind: string): BinoTreeItem[] {
        const documents = this.indexer.getDocuments([kind]);

        return documents
            .sort((a, b) => a.name.localeCompare(b.name))
            .map(doc => {
                // LayoutPage and LayoutCard can have children
                const canHaveChildren = doc.kind === 'LayoutPage' || doc.kind === 'LayoutCard';
                return new BinoTreeItem(
                    doc.name,
                    canHaveChildren ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None,
                    doc.kind,
                    doc
                );
            });
    }

    /**
     * Parse inline children from a LayoutPage or LayoutCard document.
     */
    private async getInlineChildren(doc: LSPDocument): Promise<BinoTreeItem[]> {
        try {
            const children = await this.parseInlineChildren(doc.file, doc.position);
            return children.map(child => {
                const hasChildren = child.children && child.children.length > 0;
                return new BinoTreeItem(
                    child.name,
                    hasChildren ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None,
                    undefined,
                    undefined,
                    child
                );
            });
        } catch (err) {
            console.error(`Failed to parse inline children: ${err}`);
            return [];
        }
    }

    /**
     * Parse a YAML file and extract inline children from the specified document.
     * This is a simplified parser that looks for `children:` arrays and extracts
     * `kind` and `metadata.name` from each child.
     */
    private async parseInlineChildren(filePath: string, docIndex: number): Promise<InlineChild[]> {
        const content = fs.readFileSync(filePath, 'utf8');
        const lines = content.split('\n');

        // Find the start line of this document
        let docStartLine = this.findDocumentStartLine(lines, docIndex);
        let docEndLine = this.findDocumentEndLine(lines, docStartLine);

        // Parse children from this document section
        return this.extractChildren(lines, docStartLine, docEndLine, filePath);
    }

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

    private findDocumentEndLine(lines: string[], startLine: number): number {
        for (let lineNum = startLine + 1; lineNum < lines.length; lineNum++) {
            if (lines[lineNum].trim() === '---') {
                return lineNum - 1;
            }
        }
        return lines.length - 1;
    }

    /**
     * Extract children from a section of YAML lines.
     * Only looks for the first `children:` array directly under `spec:`.
     */
    private extractChildren(lines: string[], startLine: number, endLine: number, filePath: string): InlineChild[] {
        const children: InlineChild[] = [];

        // First, find `spec:` and its indentation
        let specIndent = -1;
        let specLine = -1;

        for (let i = startLine; i <= endLine; i++) {
            const line = lines[i];
            const trimmed = line.trim();

            if (trimmed === 'spec:' || trimmed.startsWith('spec:')) {
                specIndent = this.getIndentation(line);
                specLine = i;
                break;
            }
        }

        if (specLine === -1) {
            return children;
        }

        // Now find `children:` that is a direct child of `spec:` (indent = specIndent + 2)
        const expectedChildrenIndent = specIndent + 2;

        for (let i = specLine + 1; i <= endLine; i++) {
            const line = lines[i];
            const trimmed = line.trim();
            const indent = this.getIndentation(line);

            // Stop if we've gone past spec block
            if (trimmed && !trimmed.startsWith('#') && indent <= specIndent) {
                break;
            }

            // Look for 'children:' at the expected indentation level
            if ((trimmed === 'children:' || trimmed.startsWith('children:')) && indent === expectedChildrenIndent) {
                i++;

                // Parse child items (they should be at indent > expectedChildrenIndent)
                while (i <= endLine) {
                    const childLine = lines[i];
                    const childTrimmed = childLine.trim();
                    const childIndent = this.getIndentation(childLine);

                    // Exit if we've dedented past the children array
                    if (childTrimmed && !childTrimmed.startsWith('#') && childIndent <= expectedChildrenIndent) {
                        i--;  // Back up so outer loop can process this line
                        break;
                    }

                    // Look for array item starting a child (- kind:)
                    if (childTrimmed.startsWith('- kind:')) {
                        const result = this.parseChildBlock(lines, i, endLine, filePath);
                        if (result.child) {
                            children.push(result.child);
                        }
                        i = result.nextLine;
                        continue;
                    }

                    i++;
                }

                // We only want the first children array under spec
                break;
            }
        }

        return children;
    }

    /**
     * Parse a single child block starting at the given line.
     * Returns the child and does NOT recurse into nested children here - 
     * nested children are parsed lazily when the tree item is expanded.
     */
    private parseChildBlock(
        lines: string[],
        startLine: number,
        endLine: number,
        filePath: string
    ): { child?: InlineChild; nextLine: number } {
        const firstLine = lines[startLine].trim();
        const kindMatch = firstLine.match(/^-\s*kind:\s*(\w+)/);

        if (!kindMatch) {
            return { nextLine: startLine + 1 };
        }

        const kind = kindMatch[1];
        let name = 'unnamed';
        const blockIndent = this.getIndentation(lines[startLine]);
        let hasNestedChildren = false;
        let nestedChildrenStartLine = -1;
        let nestedChildrenIndent = -1;

        // Scan the block for metadata.name and check if there are nested children
        let i = startLine + 1;
        let inMetadata = false;
        let metadataIndent = 0;

        while (i <= endLine) {
            const line = lines[i];
            const trimmed = line.trim();
            const indent = this.getIndentation(line);

            if (trimmed && !trimmed.startsWith('#') && indent <= blockIndent) {
                if (trimmed.startsWith('-')) {
                    break;
                }
                break;
            }

            // Track metadata block
            if (trimmed === 'metadata:' || trimmed.startsWith('metadata:')) {
                inMetadata = true;
                metadataIndent = indent;
            } else if (inMetadata && indent <= metadataIndent && trimmed && !trimmed.startsWith('#')) {
                inMetadata = false;
            }

            // Extract name from metadata
            if (inMetadata && trimmed.startsWith('name:')) {
                const nameMatch = trimmed.match(/^name:\s*(.+)$/);
                if (nameMatch) {
                    name = nameMatch[1].replace(/["']/g, '').trim();
                }
            }

            // Check for nested children (but don't parse them yet)
            if (trimmed === 'children:' || trimmed.startsWith('children:')) {
                hasNestedChildren = true;
                nestedChildrenStartLine = i;
                nestedChildrenIndent = indent;
                i++;
                while (i <= endLine) {
                    const nestedLine = lines[i];
                    const nestedTrimmed = nestedLine.trim();
                    const nestedIndent = this.getIndentation(nestedLine);

                    if (nestedTrimmed && !nestedTrimmed.startsWith('#') && nestedIndent <= nestedChildrenIndent) {
                        i--;
                        break;
                    }
                    i++;
                }
                break;
            }

            i++;
        }

        const nextLine = i;

        // Only parse nested children now if this is a LayoutCard (which can have children)
        const nestedChildren: InlineChild[] = [];
        if (hasNestedChildren && (kind === 'LayoutCard' || kind === 'LayoutPage')) {
            // Parse nested children
            let j = nestedChildrenStartLine + 1;
            while (j <= endLine) {
                const nestedLine = lines[j];
                const nestedTrimmed = nestedLine.trim();
                const nestedIndent = this.getIndentation(nestedLine);

                if (nestedTrimmed && !nestedTrimmed.startsWith('#') && nestedIndent <= nestedChildrenIndent) {
                    break;
                }

                if (nestedTrimmed.startsWith('- kind:')) {
                    const nestedResult = this.parseChildBlock(lines, j, endLine, filePath);
                    if (nestedResult.child) {
                        nestedChildren.push(nestedResult.child);
                    }
                    j = nestedResult.nextLine;
                    continue;
                }

                j++;
            }
        }

        return {
            child: {
                kind,
                name,
                file: filePath,
                line: startLine,
                children: nestedChildren.length > 0 ? nestedChildren : undefined
            },
            nextLine
        };
    }

    private getIndentation(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }
}
