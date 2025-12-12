import * as vscode from 'vscode';
import { findPrqlBlockAtCursor } from './prql';

/**
 * PRQL keywords for completion suggestions.
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
    // Aggregate functions
    'sum', 'count', 'avg', 'average', 'min', 'max', 'first', 'last',
    'stddev', 'variance', 'any', 'all',
    // Other common keywords
    'as', 'and', 'or', 'not', 'in',
];

/**
 * PRQL snippet definitions for common patterns.
 */
interface PrqlSnippet {
    label: string;
    insertText: string;
    documentation: string;
    detail?: string;
}

const PRQL_SNIPPETS: PrqlSnippet[] = [
    {
        label: 'from',
        insertText: 'from ${1:table}',
        documentation: 'Start a pipeline from a table/data source.',
        detail: 'PRQL: from table',
    },
    {
        label: 'from-select',
        insertText: 'from ${1:table}\nselect { ${2:columns} }',
        documentation: 'Start a pipeline and select specific columns.',
        detail: 'PRQL: from + select',
    },
    {
        label: 'filter',
        insertText: 'filter ${1:condition}',
        documentation: 'Filter rows based on a condition.',
        detail: 'PRQL: filter condition',
    },
    {
        label: 'derive',
        insertText: 'derive { ${1:new_col} = ${2:expr} }',
        documentation: 'Create new columns from expressions.',
        detail: 'PRQL: derive new columns',
    },
    {
        label: 'derive-simple',
        insertText: 'derive ${1:new_col} = ${2:expr}',
        documentation: 'Create a single new column.',
        detail: 'PRQL: derive single column',
    },
    {
        label: 'select',
        insertText: 'select { ${1:columns} }',
        documentation: 'Select specific columns to include in output.',
        detail: 'PRQL: select columns',
    },
    {
        label: 'group-aggregate',
        insertText: [
            'group ${1:key} (',
            '  aggregate {',
            '    ${2:result} = ${3:sum} ${4:column}',
            '  }',
            ')',
        ].join('\n'),
        documentation: 'Group rows and compute aggregates.',
        detail: 'PRQL: group + aggregate',
    },
    {
        label: 'aggregate',
        insertText: [
            'aggregate {',
            '  ${1:result} = ${2:sum} ${3:column}',
            '}',
        ].join('\n'),
        documentation: 'Compute aggregate values.',
        detail: 'PRQL: aggregate block',
    },
    {
        label: 'sort',
        insertText: 'sort ${1:column}',
        documentation: 'Sort rows by column(s). Use - prefix for descending.',
        detail: 'PRQL: sort column',
    },
    {
        label: 'sort-desc',
        insertText: 'sort { -${1:column} }',
        documentation: 'Sort rows in descending order.',
        detail: 'PRQL: sort descending',
    },
    {
        label: 'take',
        insertText: 'take ${1:10}',
        documentation: 'Limit output to first N rows.',
        detail: 'PRQL: take N rows',
    },
    {
        label: 'join',
        insertText: 'join ${1:alias}=${2:table} (==${3:key})',
        documentation: 'Join with another table on a key.',
        detail: 'PRQL: join tables',
    },
    {
        label: 'join-left',
        insertText: 'join side:left ${1:alias}=${2:table} (==${3:key})',
        documentation: 'Left join with another table.',
        detail: 'PRQL: left join',
    },
    {
        label: 'window',
        insertText: [
            'window ${1:rows:-1..1} (',
            '  ${2:result} = ${3:avg} ${4:column}',
            ')',
        ].join('\n'),
        documentation: 'Compute window functions over rows.',
        detail: 'PRQL: window function',
    },
    {
        label: 'if-then-else',
        insertText: 'if ${1:condition} then ${2:value1} else ${3:value2}',
        documentation: 'Conditional expression.',
        detail: 'PRQL: conditional',
    },
    {
        label: 'case',
        insertText: [
            'case [',
            '  ${1:condition1} => ${2:result1},',
            '  ${3:condition2} => ${4:result2},',
            '  true => ${5:default}',
            ']',
        ].join('\n'),
        documentation: 'Pattern matching / case expression.',
        detail: 'PRQL: case expression',
    },
    {
        label: 'let',
        insertText: 'let ${1:name} = ${2:value}',
        documentation: 'Define a variable.',
        detail: 'PRQL: let binding',
    },
    {
        label: 'func',
        insertText: [
            'func ${1:name} ${2:arg} -> (',
            '  ${3:body}',
            ')',
        ].join('\n'),
        documentation: 'Define a reusable function.',
        detail: 'PRQL: function definition',
    },
    {
        label: 'pipeline',
        insertText: [
            'from ${1:table}',
            'filter ${2:condition}',
            'derive { ${3:new_col} = ${4:expr} }',
            'select { ${5:columns} }',
        ].join('\n'),
        documentation: 'Complete PRQL pipeline skeleton.',
        detail: 'PRQL: full pipeline',
    },
    {
        label: 'pipeline-agg',
        insertText: [
            'from ${1:table}',
            'filter ${2:condition}',
            'group ${3:key} (',
            '  aggregate {',
            '    ${4:total} = sum ${5:column}',
            '  }',
            ')',
            'sort { -${4:total} }',
            'take ${6:10}',
        ].join('\n'),
        documentation: 'Aggregation pipeline with group, sort, and limit.',
        detail: 'PRQL: aggregation pipeline',
    },
];

/**
 * Completion provider for PRQL inside YAML spec.prql blocks.
 */
class PrqlCompletionProvider implements vscode.CompletionItemProvider {
    provideCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
        _token: vscode.CancellationToken,
        _context: vscode.CompletionContext
    ): vscode.CompletionItem[] | undefined {
        // Only provide completions inside spec.prql blocks
        const prqlBlock = findPrqlBlockAtCursor(document, position);
        if (!prqlBlock) {
            return undefined;
        }

        const items: vscode.CompletionItem[] = [];

        // Add snippet completions (higher priority)
        for (const snippet of PRQL_SNIPPETS) {
            const item = new vscode.CompletionItem(
                snippet.label,
                vscode.CompletionItemKind.Snippet
            );
            item.insertText = new vscode.SnippetString(snippet.insertText);
            item.documentation = new vscode.MarkdownString(snippet.documentation);
            item.detail = snippet.detail;
            // Sort snippets before keywords
            item.sortText = `0_${snippet.label}`;
            items.push(item);
        }

        // Add keyword completions
        for (const keyword of PRQL_KEYWORDS) {
            const item = new vscode.CompletionItem(
                keyword,
                vscode.CompletionItemKind.Keyword
            );
            item.insertText = keyword;
            item.detail = 'PRQL keyword';
            // Sort keywords after snippets
            item.sortText = `1_${keyword}`;
            items.push(item);
        }

        return items;
    }
}

/**
 * Register PRQL completion provider for YAML files.
 * Call this from extension.ts activate().
 */
export function registerPrqlCompletion(context: vscode.ExtensionContext): void {
    const selector: vscode.DocumentSelector = [
        { language: 'yaml', scheme: 'file' },
        { language: 'yaml', scheme: 'untitled' },
    ];

    const provider = new PrqlCompletionProvider();

    // Register with trigger characters that commonly start PRQL constructs
    const disposable = vscode.languages.registerCompletionItemProvider(
        selector,
        provider,
        ' ', // space after pipeline step
        '\n', // new line
    );

    context.subscriptions.push(disposable);
}
