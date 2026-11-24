import * as vscode from 'vscode';
import * as cp from 'child_process';
import { getBinoPath, INSTALL_DOCS_URL } from './bino';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

/** Probe whether the binary exists and supports the `bino lsp` subcommand. */
export function probeLspCapability(binPath: string): Promise<'ok' | 'missing' | 'unsupported'> {
    return new Promise(resolve => {
        cp.execFile(binPath, ['lsp', '--help'], { timeout: 10000 }, err => {
            if (!err) {
                resolve('ok');
            } else {
                resolve((err as NodeJS.ErrnoException).code === 'ENOENT' ? 'missing' : 'unsupported');
            }
        });
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
    outputChannel: vscode.LogOutputChannel,
    projectRoot: string
): Promise<boolean> {
    const binPath = getBinoPath();
    const capability = await probeLspCapability(binPath);
    if (capability === 'missing') {
        // Not awaited: the caller must not wait for the user to click.
        void vscode.window.showWarningMessage(
            `bino was not found at '${binPath}'. Install bino or set "bino.binPath" to get completion, hover, navigation, and diagnostics.`,
            'Install Instructions',
            'Configure Path'
        ).then(action => {
            if (action === 'Install Instructions') {
                void vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
            } else if (action === 'Configure Path') {
                void vscode.commands.executeCommand('workbench.action.openSettings', 'bino.binPath');
            }
        });
        return false;
    }
    if (capability === 'unsupported') {
        void vscode.window.showWarningMessage(
            'This bino binary does not support the language server (`bino lsp`). ' +
            'Update bino to get in-editor completion, hover, navigation, and diagnostics.',
            'Update Instructions'
        ).then(action => {
            if (action === 'Update Instructions') {
                void vscode.env.openExternal(vscode.Uri.parse('https://cli.bino.bi/cli/update/'));
            }
        });
        return false;
    }

    // An Executable communicates over the child process's stdin/stdout by
    // default. Do NOT set `transport: TransportKind.stdio` - that appends a
    // `--stdio` arg (a client convention) which `bino lsp` does not require.
    const serverOptions: ServerOptions = {
        command: binPath,
        args: ['lsp', '--work-dir', projectRoot],
        options: { cwd: projectRoot },
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [
            { language: 'yaml', scheme: 'file' },
            { language: 'yaml', scheme: 'untitled' },
            // Plain VS Code has no TOML language, so match bino.toml by name too.
            { language: 'toml', pattern: '**/bino.toml' },
            { scheme: 'file', pattern: '**/bino.toml' },
        ],
        outputChannel,
        // Opt in to bino/indexChanged, the push that keeps the project index current.
        initializationOptions: { indexChanged: true, queryDiagnostics: queryDiagnosticsSetting() },
    };

    client = new LanguageClient('bino', 'bino Language Server', serverOptions, clientOptions);
    await client.start();
    context.subscriptions.push({ dispose: () => void stopLanguageClient() });
    context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(e => {
        if (client && e.affectsConfiguration('bino.lsp.queryDiagnostics')) {
            // An older server ignores the setting; this notification is harmless there.
            void client.sendNotification('workspace/didChangeConfiguration', {
                settings: { bino: { queryDiagnostics: queryDiagnosticsSetting() } },
            });
        }
    }));
    outputChannel.appendLine('[LSP] bino language server started');
    return true;
}

function queryDiagnosticsSetting(): boolean {
    return vscode.workspace.getConfiguration('bino').get<boolean>('lsp.queryDiagnostics') ?? false;
}

/** The running language client, if any. */
export function getLanguageClient(): LanguageClient | undefined {
    return client;
}

/** Stop the language client if running. */
export async function stopLanguageClient(): Promise<void> {
    if (client) {
        await client.stop();
        client = undefined;
    }
}
