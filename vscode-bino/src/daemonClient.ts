import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as http from 'http';
import { getBinoPath } from './bino';

/** Port file written by `bino daemon` */
interface PortFile {
    pid: number;
    port: number;
    startedAt: string;
    /** The bino version that started the daemon; missing in files from older daemons. */
    version?: string;
    /** Sent as a Bearer token on every request; missing in files from older daemons. */
    token?: string;
}

/** What a client needs to reach the connected daemon. */
export interface DaemonConnection {
    port: number;
    token?: string;
    version?: string;
}

/** Delay before reconnect attempt n (0-based): 1s, 2s, 4s, ... capped at 30s. */
export function reconnectDelay(attempt: number): number {
    return Math.min(1000 * 2 ** attempt, 30000);
}

/** Reconnect attempts before the client gives up and reports an error. */
const MAX_RECONNECT_ATTEMPTS = 8;

/**
 * Whether a process with this pid exists and is ours (signal 0 also works on
 * Windows in Node). EPERM counts as gone: a process of another user cannot be
 * our daemon, so its pid was reused.
 */
export function pidAlive(pid: number): boolean {
    try {
        process.kill(pid, 0);
        return true;
    } catch {
        return false;
    }
}

/**
 * Wait until a stopped daemon has removed its port file. The daemon removes it
 * only after its shutdown, so the file can outlive the kill by seconds. A file
 * whose process is gone (a hard kill on Windows) is removed here.
 */
export async function waitForDaemonExit(projectRoot: string, pid: number | undefined, timeoutMs = 10000): Promise<void> {
    if (pid === undefined) {
        return;
    }
    const file = path.join(projectRoot, '.bino', 'daemon.json');
    const deadline = Date.now() + timeoutMs;
    while (fs.existsSync(file) && Date.now() < deadline) {
        if (!pidAlive(pid)) {
            fs.rmSync(file, { force: true });
            return;
        }
        await new Promise((r) => setTimeout(r, 100));
    }
}

/** Status of the daemon connection */
export type DaemonStatus = 'disconnected' | 'connecting' | 'connected' | 'error';

/** One declared package parameter (mirrors the daemon's RegistryParam). */
export interface RegistryParam {
    name: string;
    type?: string;
    required?: boolean;
    default?: string;
    description?: string;
    options?: string[];
}

/** One project dependency (mirrors the daemon's RegistryPackage). */
export interface RegistryPackage {
    name: string;
    version?: string;
    tag?: string;
    kind?: string;
    /** The package's primary document, project-relative. */
    path?: string;
    /** Every kind a multi-file package ships. */
    kinds?: string[];
    /** Every file the package installs, project-relative. */
    files?: string[];
    direct: boolean;
    declaredRef?: string;
    installed: boolean;
    dependencies?: string[];
    params?: RegistryParam[];
}

/** One registry search hit (mirrors the daemon's proxied SearchItem). */
export interface RegistrySearchItem {
    package: string;
    kind: string;
    description: string;
    latestVersion: string;
    pullsTotal: number;
}

/** The registry search response envelope. */
export interface RegistrySearchResult {
    page: number;
    perPage: number;
    totalItems: number;
    totalPages: number;
    items: RegistrySearchItem[];
    error?: string;
}

/**
 * DaemonClient manages the connection to a persistent `bino daemon` process.
 * It discovers or spawns the daemon, communicates over HTTP, and listens for
 * SSE push events (index-updated, diagnostics, file-changed, etc.).
 */
export class DaemonClient {
    private port: number | undefined;
    private token: string | undefined;
    // Set when the daemon answered 401; cleared on the next good connection.
    private rejected = false;
    private daemonPid: number | undefined; // PID for an explicit restart
    private projectRoot: string | undefined;
    private daemonProcess: cp.ChildProcess | undefined;
    private eventSource: { abort: () => void } | undefined;
    private status: DaemonStatus = 'disconnected';
    private outputChannel: vscode.OutputChannel;
    private eventHandlers = new Map<string, Set<(data: any) => void>>();
    private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    private reconnectAttempt = 0;
    // Bumped whenever the SSE stream is replaced, so a stale stream's end is ignored.
    private streamGeneration = 0;
    // Bumped by every connect, switch and restart, so a connect that was overtaken stops.
    private connectGeneration = 0;
    // The bino it may spawn, so a bino.binPath change does not join a connect with the old one.
    private connecting: { root: string; bin: string; promise: Promise<boolean> } | undefined;
    private disposed = false;
    private daemonVersion: string | undefined;
    private portFileVersion: string | undefined;
    private capabilities: string[] = [];

    private _onStatusChange = new vscode.EventEmitter<DaemonStatus>();
    readonly onStatusChange = this._onStatusChange.event;

    constructor(outputChannel: vscode.OutputChannel) {
        this.outputChannel = outputChannel;
    }

    /** Current connection status */
    getStatus(): DaemonStatus {
        return this.status;
    }

    /** Whether the daemon is connected and ready */
    get isConnected(): boolean {
        return this.status === 'connected';
    }

    /** Port and token of the connected daemon, undefined while not connected. */
    connection(): DaemonConnection | undefined {
        if (!this.isConnected || !this.port) {
            return undefined;
        }
        return { port: this.port, token: this.token, version: this.version };
    }

    /**
     * Connect to an existing daemon or spawn a new one.
     * Returns true if connected successfully.
     */
    async connect(projectRoot: string): Promise<boolean> {
        if (this.status === 'connected') {
            return true;
        }
        const bin = getBinoPath();
        if (this.connecting?.root === projectRoot) {
            if (this.connecting.bin === bin) {
                return this.connecting.promise;
            }
            // The old binary may still be starting a daemon: it would take the port file.
            this.daemonProcess?.kill();
            this.daemonProcess = undefined;
        }
        const generation = ++this.connectGeneration;
        const promise = this.doConnect(projectRoot, generation);
        this.connecting = { root: projectRoot, bin, promise };
        try {
            return await promise;
        } finally {
            if (this.connecting?.promise === promise) {
                this.connecting = undefined;
            }
        }
    }

    private async doConnect(projectRoot: string, generation: number): Promise<boolean> {
        this.setStatus('connecting');
        this.projectRoot = projectRoot;

        // Step 1: Try to discover existing daemon
        const portFile = this.readPortFile(projectRoot);
        if (portFile) {
            this.adopt(portFile);
            const healthy = await this.healthCheck();
            if (generation !== this.connectGeneration) {
                return false;
            }
            if (healthy) {
                this.setStatus('connected');
                this.startEventStream();
                this.outputChannel.appendLine(`[Daemon] Connected to existing daemon on port ${this.port} (pid ${this.daemonPid})`);
                return true;
            }
            this.port = undefined;
            if (this.rejected) {
                // The daemon is alive: spawning another would fail, so wait for a restart.
                this.setStatus('error');
                return false;
            }
            // Stale port file: its pid may belong to another process by now.
            this.forgetPortFile();
        }

        // Step 2: Spawn new daemon
        const spawned = await this.spawnDaemon(projectRoot, generation);
        if (generation !== this.connectGeneration) {
            return false;
        }
        if (!spawned) {
            this.setStatus('error');
            return false;
        }

        this.setStatus('connected');
        this.startEventStream();
        return true;
    }

    /** Register a handler for SSE events */
    on(event: string, callback: (data: any) => void): vscode.Disposable {
        if (!this.eventHandlers.has(event)) {
            this.eventHandlers.set(event, new Set());
        }
        this.eventHandlers.get(event)!.add(callback);
        return { dispose: () => this.eventHandlers.get(event)?.delete(callback) };
    }

    /** GET /index */
    async getIndex(): Promise<{ documents: any[]; error?: string } | undefined> {
        return this.fetchJSON('/index');
    }

    /** GET /kinds - every manifest kind with its capability category and render-embeddable flag */
    async getKinds(): Promise<{ kinds: { name: string; category: string; embeddable: boolean }[]; error?: string } | undefined> {
        return this.fetchJSON('/kinds');
    }

    /** GET /validate */
    async getValidation(): Promise<{ valid: boolean; diagnostics: any[]; error?: string } | undefined> {
        return this.fetchJSON('/validate');
    }

    /** POST /validate (with query execution) */
    async validateWithQueries(): Promise<{ valid: boolean; diagnostics: any[]; error?: string } | undefined> {
        return this.fetchJSON('/validate', 'POST');
    }

    /** GET /schema - returns merged JSON schema (built-in + plugin kinds) */
    async getSchema(): Promise<string | undefined> {
        if (!this.isConnected || !this.port) {
            return undefined;
        }
        return new Promise<string | undefined>((resolve) => {
            const req = http.get(`http://127.0.0.1:${this.port}/schema`, { headers: this.authHeaders() }, (res) => {
                if (res.statusCode === 401) {
                    this.unauthorized();
                }
                if (res.statusCode !== 200) {
                    resolve(undefined);
                    res.resume();
                    return;
                }
                const chunks: Buffer[] = [];
                res.on('data', (chunk: Buffer) => chunks.push(chunk));
                res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
            });
            req.on('error', () => resolve(undefined));
            req.setTimeout(5000, () => { req.destroy(); resolve(undefined); });
        });
    }

    /** GET /columns?name=X */
    async getColumns(name: string): Promise<{ name: string; columns: string[]; error?: string } | undefined> {
        return this.fetchJSON(`/columns?name=${encodeURIComponent(name)}`);
    }

    /** GET /rows?name=X&limit=N */
    async getRows(name: string, limit?: number): Promise<any | undefined> {
        let url = `/rows?name=${encodeURIComponent(name)}`;
        if (limit !== undefined) {
            url += `&limit=${limit}`;
        }
        return this.fetchJSON(url);
    }

    /** GET /graph-deps */
    async getGraphDeps(kind: string, name: string, direction: string = 'both', maxDepth: number = 0): Promise<any | undefined> {
        let url = `/graph-deps?kind=${encodeURIComponent(kind)}&name=${encodeURIComponent(name)}&direction=${encodeURIComponent(direction)}`;
        if (maxDepth > 0) {
            url += `&max-depth=${maxDepth}`;
        }
        return this.fetchJSON(url);
    }

    /** POST /preview/start */
    async startPreview(port?: number): Promise<{ status: string; url?: string; port?: number; error?: string } | undefined> {
        return this.fetchJSON('/preview/start', 'POST', port ? { port } : undefined);
    }

    /** POST /preview/stop */
    async stopPreview(): Promise<{ status: string } | undefined> {
        return this.fetchJSON('/preview/stop', 'POST');
    }

    /** GET /preview/status */
    async getPreviewStatus(): Promise<{ status: string; url?: string; port?: number } | undefined> {
        return this.fetchJSON('/preview/status');
    }

    /** POST /introspect-draft - introspect a not-yet-registered data source */
    async introspectDraft(req: { spec: any; baseDir?: string; sheet?: string; limit?: number }): Promise<any | undefined> {
        return this.fetchJSON('/introspect-draft', 'POST', req);
    }

    /** POST /sqlgen/typed-select - generate a column-aware SELECT */
    async typedSelect(req: { source: string; columns: any[]; pretty?: boolean; castMode?: string }): Promise<{ sql: string; aliases: string[] } | undefined> {
        return this.fetchJSON('/sqlgen/typed-select', 'POST', req);
    }

    /** POST /preview-dataset - run a draft DataSet SQL against a not-yet-registered source */
    async previewDataSet(req: { spec: any; sourceName: string; sql: string; baseDir?: string; sheet?: string; limit?: number }): Promise<any | undefined> {
        return this.fetchJSON('/preview-dataset', 'POST', req);
    }

    /** GET /dataset-schema - the canonical standard dataset columns */
    async datasetSchema(): Promise<{ columns: any[] } | undefined> {
        return this.fetchJSON('/dataset-schema');
    }

    /** GET /registry/packages - offline dependency report (bino.lock + bino.toml + store) */
    async getRegistryPackages(): Promise<{ packages: RegistryPackage[]; error?: string } | undefined> {
        return this.fetchJSON('/registry/packages');
    }

    /** GET /registry/search - proxied full-text registry search */
    async searchRegistry(query: string, opts?: { kinds?: string[]; page?: number; perPage?: number }): Promise<RegistrySearchResult | undefined> {
        const params = new URLSearchParams();
        if (query) {
            params.set('q', query);
        }
        for (const kind of opts?.kinds ?? []) {
            params.append('kind', kind);
        }
        if (opts?.page) {
            params.set('page', String(opts.page));
        }
        if (opts?.perPage) {
            params.set('perPage', String(opts.perPage));
        }
        return this.fetchJSON(`/registry/search?${params.toString()}`);
    }

    /** GET /registry/info - resolve a spec ("@scope/name[@ref]") + installed version */
    async getRegistryInfo(spec: string): Promise<any | undefined> {
        return this.fetchJSON(`/registry/info?spec=${encodeURIComponent(spec)}`);
    }

    /**
     * Disconnect from the daemon without stopping it. Other windows and MCP
     * clients may share it; the daemon exits on its own once no client is left.
     */
    shutdown(): void {
        this.cleanup();
    }

    /** Dispose SSE stream without shutting down the daemon. */
    dispose(): void {
        this.cleanup();
    }

    /**
     * Terminate the current daemon and connect to a freshly spawned one. Used to
     * recover from a stale daemon (one started by an older binary that lacks a
     * needed capability). Unlike shutdown(), this preserves event handlers and
     * the status emitter so the extension stays wired up across the restart.
     */
    async restart(projectRoot: string): Promise<boolean> {
        const pid = this.daemonPid ?? this.daemonProcess?.pid;
        this.cancelConnect();
        this.stopEventStream();
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = undefined;
        }
        if (pid) {
            try {
                process.kill(pid, 'SIGTERM');
            } catch {
                // already gone
            }
        }
        this.port = undefined;
        this.token = undefined;
        this.rejected = false;
        this.daemonPid = undefined;
        this.daemonProcess = undefined;
        this.capabilities = [];
        this.daemonVersion = undefined;
        this.portFileVersion = undefined;
        this.setStatus('disconnected');
        // A new daemon refuses to start while the old port file is still there.
        await waitForDaemonExit(projectRoot, pid);
        return this.connect(projectRoot);
    }

    /**
     * Connect to the daemon of another project. The current daemon keeps
     * running: other windows may use it, and it exits once no client is left.
     */
    async switchProject(projectRoot: string): Promise<boolean> {
        if (projectRoot === this.projectRoot && this.status === 'connected') {
            return true;
        }
        if (this.connecting?.root === projectRoot) {
            return this.connecting.promise;
        }
        this.cancelConnect();
        this.stopEventStream();
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = undefined;
        }
        // The new root's port file brings its own port and token.
        this.port = undefined;
        this.token = undefined;
        this.rejected = false;
        this.daemonPid = undefined;
        this.daemonProcess = undefined;
        this.capabilities = [];
        this.daemonVersion = undefined;
        this.portFileVersion = undefined;
        this.setStatus('disconnected');
        return this.connect(projectRoot);
    }

    // --- Private methods ---

    /** Make a running connect() stop at its next step, without adopting or setting a status. */
    private cancelConnect(): void {
        this.connectGeneration++;
        this.connecting = undefined;
    }

    /** Drop what a port file said once its daemon did not answer. */
    private forgetPortFile(): void {
        this.token = undefined;
        this.daemonPid = undefined;
        this.portFileVersion = undefined;
    }

    private setStatus(status: DaemonStatus): void {
        this.status = status;
        this._onStatusChange.fire(status);
    }

    private adopt(portFile: PortFile): void {
        this.port = portFile.port;
        this.token = portFile.token;
        this.daemonPid = portFile.pid;
        this.portFileVersion = portFile.version;
    }

    /** Request headers with the daemon token, when the port file has one. */
    private authHeaders(): Record<string, string> {
        return this.token ? { Authorization: `Bearer ${this.token}` } : {};
    }

    /** A 401 means this extension and the running daemon disagree on the token. */
    private unauthorized(): void {
        if (this.rejected) {
            return;
        }
        this.rejected = true;
        this.outputChannel.appendLine('[Daemon] The daemon rejected the request (401 Unauthorized)');
        void vscode.window.showWarningMessage(
            'The running bino daemon rejected this extension. It is probably newer than this extension. Restart the daemon to start one that matches.',
            'Restart Daemon'
        ).then(choice => {
            if (choice === 'Restart Daemon') {
                void vscode.commands.executeCommand('bino.restartDaemon');
            }
        });
    }

    private readPortFile(projectRoot: string): PortFile | undefined {
        const filePath = path.join(projectRoot, '.bino', 'daemon.json');
        try {
            const data = fs.readFileSync(filePath, 'utf8');
            return JSON.parse(data) as PortFile;
        } catch {
            return undefined;
        }
    }

    private async healthCheck(): Promise<boolean> {
        if (!this.port) {
            return false;
        }
        const generation = this.connectGeneration;
        try {
            const result = await this.fetchJSON('/health');
            if (generation !== this.connectGeneration) {
                return false;
            }
            if (result?.status === 'ok') {
                this.rejected = false;
                this.daemonVersion = typeof result.version === 'string' ? result.version : undefined;
                this.capabilities = Array.isArray(result.capabilities) ? result.capabilities : [];
                return true;
            }
            return false;
        } catch {
            return false;
        }
    }

    /** The project root of the last connect(), if any. */
    getProjectRoot(): string | undefined {
        return this.projectRoot;
    }

    /** The bino version that started the connected daemon, if known. */
    get version(): string | undefined {
        return this.portFileVersion ?? this.daemonVersion;
    }

    /** The pid of the connected daemon, if known. */
    get pid(): number | undefined {
        return this.daemonPid;
    }

    /**
     * Whether the connected daemon advertises a given capability. Daemons spawned
     * from an older binary won't list new capabilities, so this is the signal the
     * extension uses to detect a stale daemon and fall back / offer a restart.
     */
    hasCapability(name: string): boolean {
        return this.capabilities.includes(name);
    }

    private async spawnDaemon(projectRoot: string, generation: number): Promise<boolean> {
        const binPath = getBinoPath();

        this.outputChannel.appendLine(`[Daemon] Spawning: ${binPath} daemon --work-dir ${projectRoot}`);

        return new Promise((resolve) => {
            try {
                const proc = cp.spawn(binPath, ['daemon', '--work-dir', projectRoot], {
                    cwd: projectRoot,
                    stdio: ['ignore', 'pipe', 'pipe'],
                    shell: false,
                });

                this.daemonProcess = proc;

                // The daemon outlives a disposed client (other windows use it), and
                // so may its output: keep reading the pipes, but stop logging.
                const log = (data: Buffer) => {
                    if (!this.disposed) {
                        this.outputChannel.append(data.toString());
                    }
                };
                proc.stdout?.on('data', log);
                proc.stderr?.on('data', log);

                proc.on('error', (err) => {
                    if (!this.disposed) {
                        this.outputChannel.appendLine(`[Daemon] Spawn error: ${err.message}`);
                    }
                    this.daemonProcess = undefined;
                    resolve(false);
                });

                proc.on('exit', (code) => {
                    if (this.disposed) {
                        return;
                    }
                    this.outputChannel.appendLine(`[Daemon] Process exited with code ${code}`);
                    if (this.daemonProcess !== proc) {
                        return; // an old daemon, already replaced
                    }
                    this.daemonProcess = undefined;
                    this.connectionLost();
                });

                // Poll for port file with timeout
                const startTime = Date.now();
                const maxWait = 10000; // 10 seconds
                const pollInterval = 200;

                const poll = async () => {
                    if (generation !== this.connectGeneration) {
                        // Overtaken by another connect; the daemon exits once idle.
                        resolve(false);
                        return;
                    }
                    if (Date.now() - startTime > maxWait) {
                        this.outputChannel.appendLine('[Daemon] Timeout waiting for daemon to start');
                        // Do not leave a half-started daemon behind.
                        if (this.daemonProcess === proc) {
                            this.daemonProcess = undefined;
                        }
                        proc.kill();
                        resolve(false);
                        return;
                    }

                    const portFile = this.readPortFile(projectRoot);
                    if (portFile) {
                        this.adopt(portFile);
                        if (await this.healthCheck()) {
                            this.outputChannel.appendLine(`[Daemon] Started on port ${this.port} (pid ${this.daemonPid})`);
                            resolve(true);
                            return;
                        }
                    }

                    setTimeout(poll, pollInterval);
                };

                setTimeout(poll, pollInterval);
            } catch (err) {
                this.outputChannel.appendLine(`[Daemon] Failed to spawn: ${err}`);
                resolve(false);
            }
        });
    }

    private startEventStream(): void {
        if (!this.port) {
            return;
        }

        this.stopEventStream();
        const generation = this.streamGeneration;

        const controller = new AbortController();
        this.eventSource = { abort: () => controller.abort() };

        const url = `http://127.0.0.1:${this.port}/events`;
        this.outputChannel.appendLine(`[Daemon] Connecting SSE to ${url}`);

        const req = http.get(url, { signal: controller.signal as any, headers: this.authHeaders() }, (res) => {
            if (res.statusCode !== 200) {
                res.resume();
                this.outputChannel.appendLine(`[Daemon] SSE returned status ${res.statusCode}`);
                if (generation !== this.streamGeneration) {
                    return;
                }
                if (res.statusCode === 401) {
                    // A reconnect would get the same answer: stop until the daemon is restarted.
                    this.unauthorized();
                    this.stopEventStream();
                    this.setStatus('error');
                } else {
                    this.connectionLost();
                }
                return;
            }
            // Reset only here, so a daemon that keeps refusing the stream is given up on.
            this.reconnectAttempt = 0;

            let buffer = '';
            let currentEvent = '';

            res.setEncoding('utf8');
            res.on('data', (chunk: string) => {
                buffer += chunk;
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        currentEvent = line.slice(7).trim();
                    } else if (line.startsWith('data: ')) {
                        const data = line.slice(6);
                        if (currentEvent) {
                            this.handleSSEEvent(currentEvent, data);
                            currentEvent = '';
                        }
                    } else if (line.trim() === '') {
                        currentEvent = '';
                    }
                }
            });

            res.on('end', () => {
                if (generation !== this.streamGeneration) {
                    return;
                }
                this.outputChannel.appendLine('[Daemon] SSE connection closed');
                this.connectionLost();
            });
        });

        req.on('error', (err: any) => {
            if (err.code === 'ABORT_ERR' || err.name === 'AbortError' || generation !== this.streamGeneration) {
                return; // Expected on cleanup
            }
            this.outputChannel.appendLine(`[Daemon] SSE error: ${err.message}`);
            this.connectionLost();
        });
    }

    private stopEventStream(): void {
        this.streamGeneration++;
        if (this.eventSource) {
            this.eventSource.abort();
            this.eventSource = undefined;
        }
    }

    private handleSSEEvent(event: string, dataStr: string): void {
        try {
            const data = JSON.parse(dataStr);
            const handlers = this.eventHandlers.get(event);
            if (handlers) {
                for (const handler of handlers) {
                    try {
                        handler(data);
                    } catch (err) {
                        this.outputChannel.appendLine(`[Daemon] Event handler error for ${event}: ${err}`);
                    }
                }
            }
        } catch {
            // Ignore parse errors for non-JSON events (e.g., keepalive)
        }
    }

    /** The daemon's event stream or process ended: reconnect with backoff. */
    private connectionLost(): void {
        if (this.disposed || this.status !== 'connected') {
            return;
        }
        this.stopEventStream();
        this.setStatus('disconnected');
        this.scheduleReconnect();
    }

    private scheduleReconnect(): void {
        if (this.reconnectTimer || this.disposed || !this.projectRoot) {
            return;
        }
        if (this.reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
            this.outputChannel.appendLine('[Daemon] Giving up reconnecting. Use "Bino: Restart Daemon" to try again.');
            this.setStatus('error');
            return;
        }
        const delay = reconnectDelay(this.reconnectAttempt++);
        this.outputChannel.appendLine(`[Daemon] Reconnecting in ${delay / 1000}s...`);
        this.reconnectTimer = setTimeout(async () => {
            this.reconnectTimer = undefined;
            if (!(await this.reconnect())) {
                this.scheduleReconnect();
            }
        }, delay);
    }

    /**
     * Adopt the daemon in the port file when it answers. While its process is
     * alive (busy, or another window is starting it) keep waiting; once it is
     * really gone, spawn a new one if this window still has the daemon enabled.
     */
    private async reconnect(): Promise<boolean> {
        const root = this.projectRoot;
        if (this.disposed || !root || this.status === 'connected') {
            return this.status === 'connected';
        }
        const generation = this.connectGeneration;
        const portFile = this.readPortFile(root);
        if (portFile && pidAlive(portFile.pid)) {
            this.adopt(portFile);
            const healthy = await this.healthCheck();
            if (generation !== this.connectGeneration) {
                return false;
            }
            if (healthy) {
                this.setStatus('connected');
                this.startEventStream();
                this.outputChannel.appendLine(`[Daemon] Reconnected to daemon on port ${this.port} (pid ${this.daemonPid})`);
                return true;
            }
            this.port = undefined;
            if (!this.rejected) {
                this.forgetPortFile();
            }
            return false;
        }
        if (!vscode.workspace.getConfiguration('bino').get<boolean>('daemon.enabled', true)) {
            return false;
        }
        this.port = undefined;
        this.daemonPid = undefined;
        this.portFileVersion = undefined;
        return this.connect(root);
    }

    private async fetchJSON<T = any>(urlPath: string, method: string = 'GET', body?: any): Promise<T | undefined> {
        if (!this.port) {
            return undefined;
        }

        return new Promise((resolve, reject) => {
            const url = `http://127.0.0.1:${this.port}${urlPath}`;
            const bodyStr = body ? JSON.stringify(body) : undefined;
            const options: http.RequestOptions = {
                method,
                timeout: method === 'POST' ? 300000 : 30000, // 5 min for POST (builds)
                headers: {
                    ...this.authHeaders(),
                    ...(bodyStr ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(bodyStr) } : {}),
                },
            };

            const req = http.request(url, options, (res) => {
                if (res.statusCode === 401) {
                    this.unauthorized();
                }
                let data = '';
                res.setEncoding('utf8');
                res.on('data', (chunk) => { data += chunk; });
                res.on('end', () => {
                    try {
                        resolve(JSON.parse(data) as T);
                    } catch {
                        resolve(undefined);
                    }
                });
            });

            req.on('error', (err) => {
                this.outputChannel.appendLine(`[Daemon] HTTP error ${method} ${urlPath}: ${err.message}`);
                resolve(undefined);
            });

            req.on('timeout', () => {
                req.destroy();
                resolve(undefined);
            });

            if (bodyStr) {
                req.write(bodyStr);
            }
            req.end();
        });
    }

    private cleanup(): void {
        this.disposed = true;
        this.cancelConnect();
        this.stopEventStream();
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = undefined;
        }
        this.port = undefined;
        this.token = undefined;
        this.daemonPid = undefined;
        this.daemonProcess = undefined;
        this.setStatus('disconnected');
        this.eventHandlers.clear();
        this._onStatusChange.dispose();
    }
}
