import * as path from 'path';
import * as vscode from 'vscode';
import { FileFormat, FileSource } from './wizardTypes';

const EXT_FORMAT: Record<string, FileFormat> = {
    '.csv': 'csv',
    '.tsv': 'csv',
    '.txt': 'csv',
    '.xlsx': 'excel',
    '.xls': 'excel',
    '.parquet': 'parquet',
};

/** Returns the supported file format for a uri, or undefined. */
export function formatForUri(uri: vscode.Uri): FileFormat | undefined {
    return EXT_FORMAT[path.extname(uri.fsPath).toLowerCase()];
}

/** Builds an initial FileSource from a right-clicked file uri. */
export function fileSourceFromUri(uri: vscode.Uri): FileSource | undefined {
    const format = formatForUri(uri);
    if (!format) {
        return undefined;
    }
    const fileName = path.basename(uri.fsPath);
    const source: FileSource = {
        kind: 'file',
        format,
        absPath: uri.fsPath,
        fileName,
    };
    if (format === 'csv') {
        source.header = true;
    }
    return source;
}

/** Suggests a snake_case identifier base from a file name (without extension). */
export function suggestName(fileName: string): string {
    const base = fileName.replace(/\.[^.]+$/, '');
    const cleaned = base
        .toLowerCase()
        .replace(/[^a-z0-9_]+/g, '_')
        .replace(/^_+|_+$/g, '');
    return cleaned || 'data';
}
