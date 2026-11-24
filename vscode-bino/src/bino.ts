import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as path from 'path';

/** Where to install bino. */
export const INSTALL_DOCS_URL = 'https://cli.bino.bi/getting-started/installation/';

/** Pick the bino path from the inspected setting; a workspace value counts only in a trusted workspace. */
export function resolveBinPath(
    setting: { globalValue?: string; workspaceValue?: string; workspaceFolderValue?: string } | undefined,
    trusted: boolean
): string {
    const candidates = trusted
        ? [setting?.workspaceFolderValue, setting?.workspaceValue, setting?.globalValue]
        : [setting?.globalValue];
    for (const value of candidates) {
        if (value && value.trim()) {
            return value.trim();
        }
    }
    return 'bino';
}

let managedBinoPath: string | undefined;

/** Use a bino the extension downloaded when bino.binPath is empty and PATH has none. */
export function setManagedBinoPath(binary: string | undefined): void {
    managedBinoPath = binary;
}

/** Whether an executable named `name` is on PATH. */
export function isOnPath(name: string, env: NodeJS.ProcessEnv = process.env, platform: NodeJS.Platform = process.platform): boolean {
    const exts = platform === 'win32' ? (env.PATHEXT ?? '.EXE;.CMD;.BAT').split(';').filter(Boolean) : [''];
    for (const dir of (env.PATH ?? env.Path ?? '').split(path.delimiter)) {
        if (!dir) {
            continue;
        }
        for (const ext of exts) {
            try {
                if (fs.statSync(path.join(dir, name + ext)).isFile()) {
                    return true;
                }
            } catch {
                // not in this directory
            }
        }
    }
    return false;
}

/** The bino executable to run: bino.binPath, `bino` on PATH, or the downloaded one. */
export function getBinoPath(): string {
    const setting = vscode.workspace.getConfiguration('bino').inspect<string>('binPath');
    const configured = resolveBinPath(setting, vscode.workspace.isTrusted);
    if (configured !== 'bino' || !managedBinoPath || isOnPath('bino')) {
        return configured;
    }
    return managedBinoPath;
}

export interface RunOptions {
    cwd?: string;
    timeout?: number;
    maxBuffer?: number;
}

/**
 * Run bino without a shell, so paths with spaces work and the extension host is
 * not blocked. A failure rejects with the error, its stderr attached.
 */
export function runBino(args: string[], options: RunOptions = {}): Promise<string> {
    return new Promise((resolve, reject) => {
        cp.execFile(
            getBinoPath(),
            args,
            { cwd: options.cwd, timeout: options.timeout ?? 10000, maxBuffer: options.maxBuffer ?? 10 * 1024 * 1024, encoding: 'utf8' },
            (error, stdout, stderr) => {
                if (error) {
                    reject(Object.assign(error, { stderr }));
                    return;
                }
                resolve(stdout);
            }
        );
    });
}

/** Start bino without a shell for output that streams (a build). */
export function spawnBino(args: string[], cwd: string | undefined): cp.ChildProcessWithoutNullStreams {
    return cp.spawn(getBinoPath(), args, { cwd, env: { ...process.env, BINO_DISABLE_UPDATE_CHECK: '1', NO_COLOR: '1' } });
}

/** The version in `bino version` output ("bino 0.104.0 (commit ..., built ...)"). */
export function parseBinoVersion(output: string): string | undefined {
    const text = output.trim();
    const match = text.match(/^bino\s+(\S+)/) ?? text.match(/v?(\d+\.\d+\.\d+\S*)/);
    return match ? normalizeVersion(match[1]) : undefined;
}

function normalizeVersion(version: string): string {
    return version.trim().replace(/^v/, '');
}

/**
 * Whether a daemon started by daemonVersion should be restarted for the installed
 * binary. A daemon without a version comes from an older bino and counts as stale.
 */
export function daemonVersionMismatch(daemonVersion: string | undefined, installedVersion: string | undefined): boolean {
    if (!installedVersion) {
        return false;
    }
    if (!daemonVersion) {
        return true;
    }
    return normalizeVersion(daemonVersion) !== normalizeVersion(installedVersion);
}

/** The installed bino version, or undefined when bino cannot be run. */
export async function installedBinoVersion(): Promise<string | undefined> {
    try {
        return parseBinoVersion(await runBino(['version'], { timeout: 5000 }));
    } catch {
        return undefined;
    }
}
