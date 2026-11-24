import * as vscode from 'vscode';

export type ToExternalUri = (uri: vscode.Uri) => Thenable<vscode.Uri>;

/**
 * The base URL of the preview server as the webview and the browser reach it.
 * Locally this is http://localhost:<port>; in Codespaces, Dev Containers, WSL
 * and Remote SSH asExternalUri forwards the port and returns the forwarded URL.
 */
export async function previewBaseUrl(port: number, toExternal: ToExternalUri = vscode.env.asExternalUri): Promise<string> {
    const external = await toExternal(vscode.Uri.parse(`http://localhost:${port}`));
    return external.toString(true).replace(/\/+$/, '');
}

/** scheme://authority of a URL, for a webview CSP source. */
export function urlOrigin(url: string): string {
    const uri = vscode.Uri.parse(url);
    return `${uri.scheme}://${uri.authority}`;
}

/**
 * The full-preview route of a document, or undefined for kinds the full preview
 * has no route for (pages and components are shown inside an artefact).
 */
export function fullPreviewPath(kind: string, name: string): string | undefined {
    switch (kind) {
        case 'ReportArtefact':
            return '/' + encodeURI(name);
        case 'DocumentArtefact':
            return '/doc/' + encodeURI(name);
        default:
            return undefined;
    }
}
