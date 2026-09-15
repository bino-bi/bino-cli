import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as http from 'http';

/** Port file written by `bino daemon` */
interface PortFile {
    pid: number;
    port: number;
    startedAt: string;
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
    private daemonPid: number | undefined; // PID for direct kill on shutdown
    private daemonProcess: cp.ChildProcess | undefined;
    private eventSource: { abort: () => void } | undefined;
    private status: DaemonStatus = 'disconnected';
    private outputChannel: vscode.OutputChannel;
    private eventHandlers = new Map<string, Set<(data: any) => void>>();
    private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    private daemonVersion: string | undefined;
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

    /**
     * Connect to an existing daemon or spawn a new one.
     * Returns true if connected successfully.
     */
    async connect(projectRoot: string): Promise<boolean> {
        if (this.status === 'connected') {
            return true;
        }

        this.setStatus('connecting');

        // Step 1: Try to discover existing daemon
        const portFile = this.readPortFile(projectRoot);
        if (portFile) {
            this.port = portFile.port;
            this.daemonPid = portFile.pid;
            if (await this.healthCheck()) {
                this.setStatus('connected');
                this.startEventStream();
                this.outputChannel.appendLine(`[Daemon] Connected to existing daemon on port ${this.port} (pid ${this.daemonPid})`);
                return true;
            }
            // Stale port file, continue to spawn
            this.port = undefined;
        }

        // Step 2: Spawn new daemon
        const spawned = await this.spawnDaemon(projectRoot);
        if (!spawned) {
            this.setStatus('error');
            return false;
        }

        this.startEventStream();
        this.setStatus('connected');
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

    /** GET /kinds — every manifest kind with its capability category and render-embeddable flag */
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

    /** GET /schema — returns merged JSON schema (built-in + plugin kinds) */
    async getSchema(): Promise<string | undefined> {
        if (!this.isConnected || !this.port) {
            return undefined;
        }
        return new Promise<string | undefined>((resolve) => {
            const req = http.get(`http://127.0.0.1:${this.port}/schema`, (res) => {
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

    /** POST /build */
    async build(artefact?: string): Promise<{ status: string; exitCode: number; output: string; error?: string } | undefined> {
        return this.fetchJSON('/build', 'POST', artefact ? { artefact } : undefined);
    }

    /** POST /introspect-draft — introspect a not-yet-registered data source */
    async introspectDraft(req: { spec: any; baseDir?: string; sheet?: string; limit?: number }): Promise<any | undefined> {
        return this.fetchJSON('/introspect-draft', 'POST', req);
    }

    /** POST /sqlgen/typed-select — generate a column-aware SELECT */
    async typedSelect(req: { source: string; columns: any[]; pretty?: boolean; castMode?: string }): Promise<{ sql: string; aliases: string[] } | undefined> {
        return this.fetchJSON('/sqlgen/typed-select', 'POST', req);
    }

    /** POST /preview-dataset — run a draft DataSet SQL against a not-yet-registered source */
    async previewDataSet(req: { spec: any; sourceName: string; sql: string; baseDir?: string; sheet?: string; limit?: number }): Promise<any | undefined> {
        return this.fetchJSON('/preview-dataset', 'POST', req);
    }

    /** GET /dataset-schema — the canonical standard dataset columns */
    async datasetSchema(): Promise<{ columns: any[] } | undefined> {
        return this.fetchJSON('/dataset-schema');
    }

    /** GET /registry/packages — offline dependency report (bino.lock + bino.toml + store) */
    async getRegistryPackages(): Promise<{ packages: RegistryPackage[]; error?: string } | undefined> {
        return this.fetchJSON('/registry/packages');
    }

    /** GET /registry/search — proxied full-text registry search */
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

    /** GET /registry/info — resolve a spec ("@scope/name[@ref]") + installed version */
    async getRegistryInfo(spec: string): Promise<any | undefined> {
        return this.fetchJSON(`/registry/info?spec=${encodeURIComponent(spec)}`);
    }

    /**
     * Shut down the daemon process.
     * Sends SIGTERM directly to the daemon PID — this is synchronous and
     * reliable even when VS Code is closing and won't wait for HTTP requests.
     * The daemon's Go signal handler catches SIGTERM and runs deferred cleanup
     * (remove port file, stop preview child, close DuckDB).
     */
    shutdown(): void {
        this.stopEventStream();

        const pid = this.daemonPid ?? this.daemonProcess?.pid;
        if (pid) {
            try {
                this.outputChannel.appendLine(`[Daemon] Sending SIGTERM to pid ${pid}`);
                process.kill(pid, 'SIGTERM');
            } catch {
                // Process may already be dead
            }
        }

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
        this.stopEventStream();
        if (pid) {
            try {
                process.kill(pid, 'SIGTERM');
            } catch {
                // already gone
            }
        }
        this.port = undefined;
        this.daemonPid = undefined;
        this.daemonProcess = undefined;
        this.capabilities = [];
        this.daemonVersion = undefined;
        this.setStatus('disconnected');
        // Give the old daemon a moment to remove its port file before respawning.
        await new Promise((r) => setTimeout(r, 400));
        return this.connect(projectRoot);
    }

    // --- Private methods ---

    private setStatus(status: DaemonStatus): void {
        this.status = status;
        this._onStatusChange.fire(status);
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
        try {
            const result = await this.fetchJSON('/health');
            if (result?.status === 'ok') {
                this.daemonVersion = typeof result.version === 'string' ? result.version : undefined;
                this.capabilities = Array.isArray(result.capabilities) ? result.capabilities : [];
                return true;
            }
            return false;
        } catch {
            return false;
        }
    }

    /** The bino version that started the connected daemon, if known. */
    get version(): string | undefined {
        return this.daemonVersion;
    }

    /**
     * Whether the connected daemon advertises a given capability. Daemons spawned
     * from an older binary won't list new capabilities, so this is the signal the
     * extension uses to detect a stale daemon and fall back / offer a restart.
     */
    hasCapability(name: string): boolean {
        return this.capabilities.includes(name);
    }

    private async spawnDaemon(projectRoot: string): Promise<boolean> {
        const config = vscode.workspace.getConfiguration('bino');
        const binPath = config.get<string>('binPath')?.trim() || 'bino';

        this.outputChannel.appendLine(`[Daemon] Spawning: ${binPath} daemon --work-dir ${projectRoot}`);

        return new Promise((resolve) => {
            try {
                const proc = cp.spawn(binPath, ['daemon', '--work-dir', projectRoot], {
                    cwd: projectRoot,
                    stdio: ['ignore', 'pipe', 'pipe'],
                    shell: false,
                });

                this.daemonProcess = proc;

                proc.stdout?.on('data', (data: Buffer) => {
                    this.outputChannel.append(data.toString());
                });

                proc.stderr?.on('data', (data: Buffer) => {
                    this.outputChannel.append(data.toString());
                });

                proc.on('error', (err) => {
                    this.outputChannel.appendLine(`[Daemon] Spawn error: ${err.message}`);
                    this.daemonProcess = undefined;
                    resolve(false);
                });

                proc.on('exit', (code) => {
                    this.outputChannel.appendLine(`[Daemon] Process exited with code ${code}`);
                    this.daemonProcess = undefined;
                    if (this.status === 'connected') {
                        this.setStatus('disconnected');
                        this.scheduleReconnect(projectRoot);
                    }
                });

                // Poll for port file with timeout
                const startTime = Date.now();
                const maxWait = 10000; // 10 seconds
                const pollInterval = 200;

                const poll = async () => {
                    if (Date.now() - startTime > maxWait) {
                        this.outputChannel.appendLine('[Daemon] Timeout waiting for daemon to start');
                        resolve(false);
                        return;
                    }

                    const portFile = this.readPortFile(projectRoot);
                    if (portFile) {
                        this.port = portFile.port;
                        this.daemonPid = portFile.pid;
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

        const controller = new AbortController();
        this.eventSource = { abort: () => controller.abort() };

        const url = `http://127.0.0.1:${this.port}/events`;
        this.outputChannel.appendLine(`[Daemon] Connecting SSE to ${url}`);

        const req = http.get(url, { signal: controller.signal as any }, (res) => {
            if (res.statusCode !== 200) {
                this.outputChannel.appendLine(`[Daemon] SSE returned status ${res.statusCode}`);
                return;
            }

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
                this.outputChannel.appendLine('[Daemon] SSE connection closed');
                if (this.status === 'connected') {
                    this.setStatus('disconnected');
                }
            });
        });

        req.on('error', (err: any) => {
            if (err.code === 'ABORT_ERR' || err.name === 'AbortError') {
                return; // Expected on cleanup
            }
            this.outputChannel.appendLine(`[Daemon] SSE error: ${err.message}`);
            if (this.status === 'connected') {
                this.setStatus('disconnected');
            }
        });
    }

    private stopEventStream(): void {
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

    private scheduleReconnect(projectRoot: string): void {
        if (this.reconnectTimer) {
            return;
        }
        this.outputChannel.appendLine('[Daemon] Scheduling reconnect in 3s...');
        this.reconnectTimer = setTimeout(async () => {
            this.reconnectTimer = undefined;
            await this.connect(projectRoot);
        }, 3000);
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
                headers: bodyStr ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(bodyStr) } : undefined,
            };

            const req = http.request(url, options, (res) => {
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
        this.stopEventStream();
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = undefined;
        }
        this.port = undefined;
        this.daemonPid = undefined;
        this.daemonProcess = undefined;
        this.setStatus('disconnected');
        this.eventHandlers.clear();
        this._onStatusChange.dispose();
    }
}
