import * as vscode from 'vscode';
import { PrqlBlockInfo } from './prql';

/**
 * PRQL keywords based on https://prql-lang.org
 * These are the core pipeline and statement keywords.
 */
const PRQL_KEYWORDS = [
    // Core pipeline transforms
    'from', 'select', 'derive', 'filter', 'group', 'aggregate',
    'sort', 'take', 'join', 'window', 'append', 'loop',
    // Clauses and modifiers
    'let', 'func', 'prql', 'into', 'case', 'type', 'module',
    // Join types
    'inner', 'left', 'right', 'full', 'cross', 'side',
    // Sorting modifiers
    'asc', 'desc',
    // Boolean/null literals
    'true', 'false', 'null',
    // Conditionals
    'if', 'then', 'else',
    // Aggregate functions (commonly highlighted)
    'sum', 'count', 'avg', 'average', 'min', 'max', 'first', 'last',
    'stddev', 'variance', 'any', 'all',
    // Other common functions
    'as', 'and', 'or', 'not', 'in',
];

// Precompile keyword regex for performance
const KEYWORD_REGEX = new RegExp(`\\b(${PRQL_KEYWORDS.join('|')})\\b`, 'gi');

// String patterns: double and single quoted, with escapes
const STRING_REGEX = /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g;

// F-strings and S-strings: f"..." or s"..."
const FSTRING_REGEX = /[fs]"(?:[^"\\]|\\.)*"/g;

// Comments: # to end of line
const COMMENT_REGEX = /#.*/g;

// Numbers: integers and decimals
const NUMBER_REGEX = /\b\d+(?:\.\d+)?\b/g;

// Date/time literals: @2023-01-01 or @2023-01-01T12:00:00
const DATE_REGEX = /@\d{4}-\d{2}-\d{2}(?:T[\d:]+)?/g;

// Operators for subtle highlighting
const OPERATOR_REGEX = /==|!=|>=|<=|=>|->|\|\||&&|[+\-*/%<>=!|&]/g;

/**
 * Decoration types for PRQL syntax highlighting.
 * Uses VS Code theme colors where possible for consistency.
 */
interface DecorationTypes {
    keyword: vscode.TextEditorDecorationType;
    string: vscode.TextEditorDecorationType;
    number: vscode.TextEditorDecorationType;
    comment: vscode.TextEditorDecorationType;
    operator: vscode.TextEditorDecorationType;
    date: vscode.TextEditorDecorationType;
}

function createDecorationTypes(): DecorationTypes {
    return {
        keyword: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('editorLink.activeForeground'),
            fontWeight: 'bold',
        }),
        string: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('terminal.ansiYellow'),
        }),
        number: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('terminal.ansiCyan'),
        }),
        comment: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('terminal.ansiGreen'),
            fontStyle: 'italic',
        }),
        operator: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('terminal.ansiBrightBlack'),
        }),
        date: vscode.window.createTextEditorDecorationType({
            color: new vscode.ThemeColor('terminal.ansiMagenta'),
        }),
    };
}

/**
 * Find all PRQL blocks in a YAML document.
 * Mirrors the logic in prql.ts but returns all blocks, not just the one at cursor.
 */
export function findAllPrqlBlocks(document: vscode.TextDocument): PrqlBlockInfo[] {
    const blocks: PrqlBlockInfo[] = [];
    const text = document.getText();
    const lines = text.split('\n');

    let currentDocKind = '';
    let currentDocName = '';
    let inPrqlBlock = false;
    let prqlStartLine = -1;
    let prqlIndent = 0;
    let prqlLines: string[] = [];

    const pushBlock = (endLine: number) => {
        if (currentDocKind === 'DataSet' && prqlLines.length > 0) {
            blocks.push({
                datasetName: currentDocName,
                prqlText: prqlLines.join('\n'),
                startLine: prqlStartLine,
                startChar: prqlIndent,
                endLine: endLine,
                filePath: document.uri.fsPath,
            });
        }
    };

    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        const line = lines[lineNum];
        const trimmed = line.trim();

        // Document separator
        if (trimmed === '---') {
            if (inPrqlBlock) {
                pushBlock(lineNum - 1);
            }
            currentDocKind = '';
            currentDocName = '';
            inPrqlBlock = false;
            prqlLines = [];
            continue;
        }

        // Track kind
        const kindMatch = trimmed.match(/^kind:\s*(\w+)/);
        if (kindMatch) {
            currentDocKind = kindMatch[1];
        }

        // Track name (first occurrence after metadata:)
        const nameMatch = trimmed.match(/^name:\s*(\w+)/);
        if (nameMatch && currentDocName === '') {
            currentDocName = nameMatch[1];
        }

        // Look for prql: field start (block style with |)
        const prqlBlockMatch = line.match(/^(\s*)prql:\s*\|?\s*$/);
        if (prqlBlockMatch && currentDocKind === 'DataSet') {
            // End any previous block first
            if (inPrqlBlock) {
                pushBlock(lineNum - 1);
            }
            inPrqlBlock = true;
            prqlStartLine = lineNum + 1;
            prqlIndent = prqlBlockMatch[1].length + 2;
            prqlLines = [];
            continue;
        }

        // Check for inline prql: <code> (single line)
        const prqlInlineMatch = line.match(/^(\s*)prql:\s+(.+)$/);
        if (prqlInlineMatch && currentDocKind === 'DataSet' && !prqlInlineMatch[2].startsWith('|')) {
            const inlineContent = prqlInlineMatch[2].trim();
            if (inlineContent) {
                blocks.push({
                    datasetName: currentDocName,
                    prqlText: inlineContent,
                    startLine: lineNum,
                    startChar: prqlInlineMatch[1].length + 'prql: '.length,
                    endLine: lineNum,
                    filePath: document.uri.fsPath,
                });
            }
            continue;
        }

        // Collect PRQL block lines
        if (inPrqlBlock) {
            const lineIndent = line.length - line.trimStart().length;

            if (trimmed === '' || lineIndent >= prqlIndent) {
                // Still in the block
                const content = lineIndent >= prqlIndent ? line.substring(prqlIndent) : '';
                prqlLines.push(content);
            } else {
                // Block ended due to dedent
                pushBlock(lineNum - 1);
                inPrqlBlock = false;
                prqlLines = [];
            }
        }
    }

    // Handle final block at end of file
    if (inPrqlBlock) {
        pushBlock(lines.length - 1);
    }

    return blocks;
}

/**
 * Tokenize a PRQL block and return decoration options for each token type.
 */
interface TokenizedBlock {
    keywords: vscode.DecorationOptions[];
    strings: vscode.DecorationOptions[];
    numbers: vscode.DecorationOptions[];
    comments: vscode.DecorationOptions[];
    operators: vscode.DecorationOptions[];
    dates: vscode.DecorationOptions[];
}

function tokenizePrqlBlock(block: PrqlBlockInfo): TokenizedBlock {
    const result: TokenizedBlock = {
        keywords: [],
        strings: [],
        numbers: [],
        comments: [],
        operators: [],
        dates: [],
    };

    const prqlLines = block.prqlText.split('\n');

    for (let localLine = 0; localLine < prqlLines.length; localLine++) {
        const prqlLine = prqlLines[localLine];
        const yamlLine = block.startLine + localLine;

        // Track which character ranges are already classified (to avoid overlapping decorations)
        const classified: Set<number> = new Set();

        const markRange = (start: number, end: number) => {
            for (let i = start; i < end; i++) {
                classified.add(i);
            }
        };

        const isClassified = (start: number, end: number): boolean => {
            for (let i = start; i < end; i++) {
                if (classified.has(i)) return true;
            }
            return false;
        };

        const makeRange = (matchStart: number, matchEnd: number): vscode.Range => {
            return new vscode.Range(
                yamlLine,
                block.startChar + matchStart,
                yamlLine,
                block.startChar + matchEnd
            );
        };

        // 1. Comments first (highest priority - nothing else in a comment)
        let match: RegExpExecArray | null;
        COMMENT_REGEX.lastIndex = 0;
        while ((match = COMMENT_REGEX.exec(prqlLine)) !== null) {
            markRange(match.index, match.index + match[0].length);
            result.comments.push({ range: makeRange(match.index, match.index + match[0].length) });
        }

        // 2. Strings (including f-strings and s-strings)
        STRING_REGEX.lastIndex = 0;
        while ((match = STRING_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                markRange(match.index, match.index + match[0].length);
                result.strings.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }

        FSTRING_REGEX.lastIndex = 0;
        while ((match = FSTRING_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                markRange(match.index, match.index + match[0].length);
                result.strings.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }

        // 3. Date literals
        DATE_REGEX.lastIndex = 0;
        while ((match = DATE_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                markRange(match.index, match.index + match[0].length);
                result.dates.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }

        // 4. Numbers
        NUMBER_REGEX.lastIndex = 0;
        while ((match = NUMBER_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                markRange(match.index, match.index + match[0].length);
                result.numbers.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }

        // 5. Keywords
        KEYWORD_REGEX.lastIndex = 0;
        while ((match = KEYWORD_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                markRange(match.index, match.index + match[0].length);
                result.keywords.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }

        // 6. Operators (low priority)
        OPERATOR_REGEX.lastIndex = 0;
        while ((match = OPERATOR_REGEX.exec(prqlLine)) !== null) {
            if (!isClassified(match.index, match.index + match[0].length)) {
                result.operators.push({ range: makeRange(match.index, match.index + match[0].length) });
            }
        }
    }

    return result;
}

/**
 * PRQL Highlighter class manages decorations for YAML editors with PRQL blocks.
 */
class PrqlHighlighter implements vscode.Disposable {
    private decorationTypes: DecorationTypes;
    private disposables: vscode.Disposable[] = [];
    private pendingUpdates: Map<string, NodeJS.Timeout> = new Map();
    private readonly debounceMs = 150;

    constructor() {
        this.decorationTypes = createDecorationTypes();
    }

    /**
     * Register event handlers and start highlighting.
     */
    register(context: vscode.ExtensionContext): void {
        // Update on active editor change
        this.disposables.push(
            vscode.window.onDidChangeActiveTextEditor((editor) => {
                if (editor) {
                    this.scheduleUpdate(editor);
                }
            })
        );

        // Update on document change
        this.disposables.push(
            vscode.workspace.onDidChangeTextDocument((event) => {
                // Find all visible editors for this document
                for (const editor of vscode.window.visibleTextEditors) {
                    if (editor.document === event.document) {
                        this.scheduleUpdate(editor);
                    }
                }
            })
        );

        // Update on selection change (for responsive feel)
        this.disposables.push(
            vscode.window.onDidChangeTextEditorSelection((event) => {
                this.scheduleUpdate(event.textEditor);
            })
        );

        // Initial update for active editor
        if (vscode.window.activeTextEditor) {
            this.scheduleUpdate(vscode.window.activeTextEditor);
        }

        // Register all disposables
        context.subscriptions.push(this);
    }

    /**
     * Schedule a debounced decoration update for an editor.
     */
    private scheduleUpdate(editor: vscode.TextEditor): void {
        const uri = editor.document.uri.toString();

        // Clear any pending timer
        const existing = this.pendingUpdates.get(uri);
        if (existing) {
            clearTimeout(existing);
        }

        // Schedule new update
        const timer = setTimeout(() => {
            this.pendingUpdates.delete(uri);
            this.updateDecorations(editor);
        }, this.debounceMs);

        this.pendingUpdates.set(uri, timer);
    }

    /**
     * Update decorations for a single editor.
     */
    private updateDecorations(editor: vscode.TextEditor): void {
        // Only process YAML files
        if (editor.document.languageId !== 'yaml') {
            this.clearDecorations(editor);
            return;
        }

        // Find all PRQL blocks
        const blocks = findAllPrqlBlocks(editor.document);

        if (blocks.length === 0) {
            this.clearDecorations(editor);
            return;
        }

        // Aggregate all tokens from all blocks
        const allKeywords: vscode.DecorationOptions[] = [];
        const allStrings: vscode.DecorationOptions[] = [];
        const allNumbers: vscode.DecorationOptions[] = [];
        const allComments: vscode.DecorationOptions[] = [];
        const allOperators: vscode.DecorationOptions[] = [];
        const allDates: vscode.DecorationOptions[] = [];

        for (const block of blocks) {
            const tokens = tokenizePrqlBlock(block);
            allKeywords.push(...tokens.keywords);
            allStrings.push(...tokens.strings);
            allNumbers.push(...tokens.numbers);
            allComments.push(...tokens.comments);
            allOperators.push(...tokens.operators);
            allDates.push(...tokens.dates);
        }

        // Apply decorations
        editor.setDecorations(this.decorationTypes.keyword, allKeywords);
        editor.setDecorations(this.decorationTypes.string, allStrings);
        editor.setDecorations(this.decorationTypes.number, allNumbers);
        editor.setDecorations(this.decorationTypes.comment, allComments);
        editor.setDecorations(this.decorationTypes.operator, allOperators);
        editor.setDecorations(this.decorationTypes.date, allDates);
    }

    /**
     * Clear all PRQL decorations from an editor.
     */
    private clearDecorations(editor: vscode.TextEditor): void {
        editor.setDecorations(this.decorationTypes.keyword, []);
        editor.setDecorations(this.decorationTypes.string, []);
        editor.setDecorations(this.decorationTypes.number, []);
        editor.setDecorations(this.decorationTypes.comment, []);
        editor.setDecorations(this.decorationTypes.operator, []);
        editor.setDecorations(this.decorationTypes.date, []);
    }

    /**
     * Dispose all resources.
     */
    dispose(): void {
        // Clear pending timers
        for (const timer of this.pendingUpdates.values()) {
            clearTimeout(timer);
        }
        this.pendingUpdates.clear();

        // Dispose decoration types
        this.decorationTypes.keyword.dispose();
        this.decorationTypes.string.dispose();
        this.decorationTypes.number.dispose();
        this.decorationTypes.comment.dispose();
        this.decorationTypes.operator.dispose();
        this.decorationTypes.date.dispose();

        // Dispose event subscriptions
        for (const d of this.disposables) {
            d.dispose();
        }
        this.disposables = [];
    }
}

/**
 * Register PRQL inline highlighting for YAML files.
 * Call this from extension.ts activate().
 */
export function registerPrqlHighlighting(context: vscode.ExtensionContext): void {
    const highlighter = new PrqlHighlighter();
    highlighter.register(context);
}
