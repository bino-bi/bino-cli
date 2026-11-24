import * as assert from 'assert';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as http from 'http';
import * as net from 'net';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

/** The real bino under test, or undefined: then e2e tests skip. */
export const binoBin = process.env.BINO_TEST_BIN;

/** Skip the test without a real bino. */
export function needBino(ctx: Mocha.Context): void {
    if (!binoBin || process.platform === 'win32') {
        ctx.skip();
    }
}

export async function waitFor<T>(what: string, fn: () => Promise<T | undefined> | T | undefined, timeoutMs = 30000): Promise<T> {
    const end = Date.now() + timeoutMs;
    for (;;) {
        const value = await fn();
        if (value !== undefined && value !== false) {
            return value;
        }
        if (Date.now() > end) {
            assert.fail(`timeout waiting for ${what}`);
        }
        await new Promise(r => setTimeout(r, 250));
    }
}

export interface PortFile { pid: number; port: number; token?: string; version?: string }

export function readPortFile(root: string): PortFile | undefined {
    try {
        return JSON.parse(fs.readFileSync(path.join(root, '.bino', 'daemon.json'), 'utf8'));
    } catch {
        return undefined;
    }
}

export function pidAlive(pid: number): boolean {
    try {
        process.kill(pid, 0);
        return true;
    } catch {
        return false;
    }
}

/** Stop a daemon the test started and wait until its port file is gone. */
export async function stopDaemon(root: string): Promise<void> {
    const pf = readPortFile(root);
    if (pf) {
        try { process.kill(pf.pid); } catch { /* gone */ }
        await waitFor('daemon exit', () => !pidAlive(pf.pid) || undefined, 10000).catch(() => undefined);
    }
}

export interface HttpResult { status: number; headers: http.IncomingHttpHeaders; body: string }

export function request(url: string, opts: { method?: string; headers?: Record<string, string>; body?: string } = {}): Promise<HttpResult> {
    return new Promise((resolve, reject) => {
        const req = http.request(url, { method: opts.method ?? 'GET', headers: opts.headers, timeout: 30000 }, res => {
            let body = '';
            res.setEncoding('utf8');
            res.on('data', c => { body += c; });
            res.on('end', () => resolve({ status: res.statusCode ?? 0, headers: res.headers, body }));
        });
        req.on('error', reject);
        req.on('timeout', () => req.destroy(new Error(`timeout ${url}`)));
        if (opts.body) {
            req.write(opts.body);
        }
        req.end();
    });
}

/** The parts of an MCP answer the tests read. */
export interface RpcResponse {
    result?: { serverInfo?: { name?: string }; tools?: { name: string }[] };
    error?: unknown;
}

/** One JSON-RPC call over MCP Streamable HTTP; the answer may come as JSON or as SSE. */
export async function mcpCall(url: string, headers: Record<string, string>, msg: object): Promise<{ result: RpcResponse | undefined; res: HttpResult }> {
    const res = await request(url, {
        method: 'POST',
        headers: { ...headers, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
        body: JSON.stringify(msg),
    });
    let text = res.body;
    if (String(res.headers['content-type']).startsWith('text/event-stream')) {
        text = res.body.split('\n').filter(l => l.startsWith('data:')).map(l => l.slice(5)).join('');
    }
    let result: RpcResponse | undefined;
    try {
        result = text ? JSON.parse(text) : undefined;
    } catch {
        result = undefined;
    }
    return { result, res };
}

/** Copy a fixture project to a temp dir, so a test can run its own daemon on it. */
export function tempProject(fixture: string, extraToml = ''): string {
    const src = path.resolve(__dirname, '..', '..', '..', 'src', 'test', 'fixtures', fixture);
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-e2e-'));
    for (const f of fs.readdirSync(src)) {
        if (f !== '.bino') {
            fs.cpSync(path.join(src, f), path.join(dir, f), { recursive: true });
        }
    }
    if (extraToml) {
        fs.appendFileSync(path.join(dir, 'bino.toml'), extraToml);
    }
    return fs.realpathSync(dir);
}

export function freePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const srv = net.createServer();
        srv.listen(0, '127.0.0.1', () => {
            const port = (srv.address() as net.AddressInfo).port;
            srv.close(() => resolve(port));
        });
        srv.on('error', reject);
    });
}

/** Labels of the completion items at a position. */
export async function completionLabels(uri: vscode.Uri, pos: vscode.Position): Promise<string[]> {
    const list = await vscode.commands.executeCommand<vscode.CompletionList>('vscode.executeCompletionItemProvider', uri, pos);
    return (list?.items ?? []).map(i => typeof i.label === 'string' ? i.label : i.label.label);
}

/** Put the cursor after `kind: ` in a new document at the end of the buffer and wait for kind completions. */
export async function waitForKindCompletion(doc: vscode.TextDocument, want = 'DataSet', timeoutMs = 45000): Promise<string[]> {
    const editor = await vscode.window.showTextDocument(doc);
    const end = doc.lineAt(doc.lineCount - 1).range.end;
    await editor.edit(e => e.insert(end, '\n---\napiVersion: bino.bi/v1alpha1\nkind: '));
    const pos = doc.lineAt(doc.lineCount - 1).range.end;
    try {
        return await waitFor(`completion with ${want}`, async () => {
            const labels = await completionLabels(doc.uri, pos);
            return labels.includes(want) ? labels : undefined;
        }, timeoutMs);
    } finally {
        await vscode.commands.executeCommand('workbench.action.files.revert');
    }
}

/** Command lines of running processes (Linux). */
export function processArgs(): string[] {
    return cp.execFileSync('ps', ['-eo', 'args'], { encoding: 'utf8' }).split('\n');
}

/** Pids of the processes whose command line ends with suffix. */
export function processPids(suffix: string): string[] {
    return cp.execFileSync('ps', ['-eo', 'pid=,args='], { encoding: 'utf8' }).split('\n')
        .map(l => l.trim()).filter(l => l.endsWith(suffix)).map(l => l.split(/\s+/)[0]);
}

export function workspaceRoot(): string {
    return vscode.workspace.workspaceFolders![0].uri.fsPath;
}

export async function activateExtension(): Promise<void> {
    const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
    assert.ok(ext, 'extension not found');
    await ext.activate();
}
