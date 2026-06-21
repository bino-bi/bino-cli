import * as vscode from 'vscode';
import * as cp from 'child_process';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    TransportKind,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

/** Resolve the bino executable path from configuration. */
export function getBinoPath(): string {
    const configured = vscode.workspace.getConfiguration('bino').get<string>('binPath');
    return configured && configured.trim() ? configured : 'bino';
}

/** Probe whether the installed binary supports the `bino lsp` subcommand. */
export function probeLspCapability(binPath: string): Promise<boolean> {
    return new Promise(resolve => {
        cp.execFile(binPath, ['lsp', '--help'], { timeout: 10000 }, err => resolve(!err));
    });
}

/**
 * Start the bino Language Server client. The client spawns `bino lsp` over stdio,
 * which proxies to the project's daemon when one is running (so the daemon stays
 * the single heavy instance) and serves standalone otherwise.
 *
 * Returns false when the binary is too old to support `bino lsp`; the caller
 * should not fall back to legacy providers (they have been removed).
 */
export async function startLanguageClient(
    context: vscode.ExtensionContext,
    outputChannel: vscode.OutputChannel,
    projectRoot: string
): Promise<boolean> {
    const binPath = getBinoPath();
    if (!(await probeLspCapability(binPath))) {
        const action = await vscode.window.showWarningMessage(
            'This bino binary does not support the language server (`bino lsp`). ' +
                'Update bino to get in-editor completion, hover, navigation, and diagnostics.',
            'Update Instructions'
        );
        if (action === 'Update Instructions') {
            vscode.env.openExternal(vscode.Uri.parse('https://bino.bi/docs/getting-started'));
        }
        return false;
    }

    const serverOptions: ServerOptions = {
        command: binPath,
        args: ['lsp', '--work-dir', projectRoot],
        options: { cwd: projectRoot },
        transport: TransportKind.stdio,
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [
            { language: 'yaml', scheme: 'file' },
            { language: 'yaml', scheme: 'untitled' },
        ],
        outputChannel,
    };

    client = new LanguageClient('bino', 'bino Language Server', serverOptions, clientOptions);
    await client.start();
    context.subscriptions.push({ dispose: () => void stopLanguageClient() });
    outputChannel.appendLine('[LSP] bino language server started');
    return true;
}

/** Stop the language client if running. */
export async function stopLanguageClient(): Promise<void> {
    if (client) {
        await client.stop();
        client = undefined;
    }
}
