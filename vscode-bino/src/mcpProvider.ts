import * as vscode from 'vscode';
import { DaemonClient, DaemonConnection } from './daemonClient';

/** Must match contributes.mcpServerDefinitionProviders in package.json. */
export const MCP_PROVIDER_ID = 'bino';

/** The daemon's /mcp endpoint as an MCP server definition, or undefined without a connection. */
export function mcpServerDefinition(conn: DaemonConnection | undefined): vscode.McpHttpServerDefinition | undefined {
    if (!conn) {
        return undefined;
    }
    const headers: Record<string, string> = conn.token ? { Authorization: `Bearer ${conn.token}` } : {};
    return new vscode.McpHttpServerDefinition(
        'Bino',
        vscode.Uri.parse(`http://127.0.0.1:${conn.port}/mcp`),
        headers,
        conn.version,
    );
}

/**
 * Offer the connected daemon's MCP server to VS Code. Nothing is offered while
 * the daemon is not connected or the workspace is untrusted.
 */
export function registerMcpProvider(client: DaemonClient): vscode.Disposable {
    // Feature-detect: the API is missing in editors that only claim compatibility.
    const lm = vscode.lm as Partial<typeof vscode.lm> | undefined;
    if (typeof lm?.registerMcpServerDefinitionProvider !== 'function') {
        return new vscode.Disposable(() => undefined);
    }
    const changed = new vscode.EventEmitter<void>();
    const registration = lm.registerMcpServerDefinitionProvider(MCP_PROVIDER_ID, {
        onDidChangeMcpServerDefinitions: changed.event,
        provideMcpServerDefinitions: () => {
            if (!vscode.workspace.isTrusted) {
                return [];
            }
            const def = mcpServerDefinition(client.connection());
            return def ? [def] : [];
        },
    });
    // Every reconnect may bring a new daemon with another port and token.
    const statusSub = client.onStatusChange(() => changed.fire());
    return vscode.Disposable.from(registration, statusSub, changed);
}
