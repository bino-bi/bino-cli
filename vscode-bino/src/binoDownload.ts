import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as crypto from 'crypto';
import * as fs from 'fs';
import * as https from 'https';
import * as path from 'path';
import { INSTALL_DOCS_URL, installedBinoVersion, isOnPath, setManagedBinoPath } from './bino';

const LATEST_RELEASE_URL = 'https://api.github.com/repos/bino-bi/bino-cli-releases/releases/latest';

/** Set once bino can be run; the walkthrough's install step completes on it. */
export const BINO_INSTALLED_CONTEXT = 'bino.binoInstalled';

/** The release asset for this platform, or undefined when none is published. */
export function assetName(platform: NodeJS.Platform, arch: string): string | undefined {
    if (platform === 'win32') {
        // Only x86_64 is published; Windows on arm64 runs it emulated.
        return arch === 'x64' || arch === 'arm64' ? 'bino-cli_Windows_x86_64.zip' : undefined;
    }
    const os = platform === 'linux' ? 'Linux' : platform === 'darwin' ? 'Darwin' : undefined;
    const cpu = arch === 'x64' ? 'x86_64' : arch === 'arm64' ? 'arm64' : undefined;
    return os && cpu ? `bino-cli_${os}_${cpu}.tar.gz` : undefined;
}

/** The sha256 for name in a goreleaser checksums.txt. */
export function checksumFor(checksums: string, name: string): string | undefined {
    for (const line of checksums.split(/\r?\n/)) {
        const [hash, file] = line.trim().split(/\s+/);
        if (file === name && /^[0-9a-f]{64}$/i.test(hash)) {
            return hash.toLowerCase();
        }
    }
    return undefined;
}

export function verifyChecksum(data: Buffer, checksums: string, name: string): boolean {
    const want = checksumFor(checksums, name);
    return want !== undefined && crypto.createHash('sha256').update(data).digest('hex') === want;
}

function versionParts(version: string): number[] | undefined {
    const match = version.trim().replace(/^v/, '').match(/^(\d+)\.(\d+)\.(\d+)/);
    return match ? match.slice(1).map(Number) : undefined;
}

/**
 * Whether version is older than required. A local build ("-dev") or a version
 * that does not parse never counts as older.
 */
export function isOlderVersion(version: string, required: string): boolean {
    const have = versionParts(version);
    const want = versionParts(required);
    if (!have || !want || /-dev\b/.test(version)) {
        return false;
    }
    for (let i = 0; i < 3; i++) {
        if (have[i] !== want[i]) {
            return have[i] < want[i];
        }
    }
    return false;
}

function binaryName(platform: NodeJS.Platform = process.platform): string {
    return platform === 'win32' ? 'bino.exe' : 'bino';
}

/** The newest downloaded bino under binDir (binDir/<version>/bino), if any. */
export function findManagedBino(binDir: string, platform: NodeJS.Platform = process.platform): string | undefined {
    let versions: string[];
    try {
        versions = fs.readdirSync(binDir).filter(v => versionParts(v));
    } catch {
        return undefined;
    }
    versions.sort((a, b) => (isOlderVersion(a, b) ? 1 : isOlderVersion(b, a) ? -1 : 0));
    for (const version of versions) {
        const binary = path.join(binDir, version, binaryName(platform));
        if (fs.existsSync(binary)) {
            return binary;
        }
    }
    return undefined;
}

/** GET url, following redirects (release assets redirect to a CDN). */
function download(url: string, onData?: (received: number, total: number) => void, redirects = 5): Promise<Buffer> {
    return new Promise((resolve, reject) => {
        const req = https.get(url, { headers: { 'User-Agent': 'vscode-bino', Accept: 'application/octet-stream, application/json' } }, res => {
            const status = res.statusCode ?? 0;
            if (status >= 300 && status < 400 && res.headers.location && redirects > 0) {
                res.resume();
                resolve(download(new URL(res.headers.location, url).toString(), onData, redirects - 1));
                return;
            }
            if (status !== 200) {
                res.resume();
                reject(new Error(`GET ${url}: HTTP ${status}`));
                return;
            }
            const total = Number(res.headers['content-length'] ?? 0);
            const chunks: Buffer[] = [];
            let received = 0;
            res.on('data', (chunk: Buffer) => {
                chunks.push(chunk);
                received += chunk.length;
                onData?.(received, total);
            });
            res.on('end', () => resolve(Buffer.concat(chunks)));
            res.on('error', reject);
        });
        req.on('error', reject);
    });
}

/** Extract with the system tar; Windows' bsdtar in System32 also reads zip. */
function extract(archive: string, dest: string): Promise<void> {
    const tar = process.platform === 'win32'
        ? path.join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'tar.exe')
        : 'tar';
    return new Promise((resolve, reject) => {
        cp.execFile(tar, ['-xf', archive, '-C', dest], { timeout: 120_000 }, err => (err ? reject(err) : resolve()));
    });
}

interface Release {
    tag_name: string;
    assets: { name: string; browser_download_url: string }[];
}

/** Download the latest bino release into storage/bin/<version>/ and return the binary. */
async function downloadLatestBino(storage: vscode.Uri, progress: vscode.Progress<{ message?: string; increment?: number }>): Promise<string> {
    const name = assetName(process.platform, process.arch);
    if (!name) {
        throw new Error(`No bino download for ${process.platform}/${process.arch}.`);
    }
    progress.report({ message: 'Looking up the latest release...' });
    const release = JSON.parse((await download(LATEST_RELEASE_URL)).toString('utf8')) as Release;
    const asset = release.assets.find(a => a.name === name);
    const sums = release.assets.find(a => a.name === 'checksums.txt');
    if (!asset || !sums) {
        throw new Error(`Release ${release.tag_name} has no ${asset ? 'checksums.txt' : name}.`);
    }
    const version = release.tag_name.replace(/^v/, '');
    if (!versionParts(version)) {
        throw new Error(`Unexpected release tag ${release.tag_name}.`);
    }
    const checksums = (await download(sums.browser_download_url)).toString('utf8');

    let reported = 0;
    const data = await download(asset.browser_download_url, (received, total) => {
        if (total > 0) {
            const percent = Math.floor((received / total) * 100);
            if (percent > reported) {
                progress.report({ message: `Downloading bino ${version}...`, increment: percent - reported });
                reported = percent;
            }
        }
    });
    if (!verifyChecksum(data, checksums, name)) {
        throw new Error(`The checksum of ${name} does not match checksums.txt.`);
    }

    progress.report({ message: 'Extracting...' });
    const dir = path.join(storage.fsPath, 'bin', version);
    await fs.promises.mkdir(dir, { recursive: true });
    const archive = path.join(dir, name);
    await fs.promises.writeFile(archive, data);
    try {
        await extract(archive, dir);
    } finally {
        await fs.promises.rm(archive, { force: true });
    }
    const binary = path.join(dir, binaryName());
    if (!fs.existsSync(binary)) {
        throw new Error(`${name} has no ${binaryName()}.`);
    }
    await fs.promises.chmod(binary, 0o755);
    return binary;
}

/** The `bino.downloadBino` command: download, verify and use the latest bino. */
export async function downloadBinoCommand(context: vscode.ExtensionContext, log: vscode.LogOutputChannel): Promise<void> {
    try {
        const binary = await vscode.window.withProgress(
            { location: vscode.ProgressLocation.Notification, title: 'Bino: downloading bino', cancellable: false },
            progress => downloadLatestBino(context.globalStorageUri, progress)
        );
        setManagedBinoPath(binary);
        log.info(`Downloaded bino to ${binary}`);
        if (isOnPath('bino')) {
            // PATH comes before a downloaded bino, so only the setting makes it count.
            const choice = await vscode.window.showInformationMessage(
                `bino was downloaded to ${binary}, but the bino on PATH comes first.`,
                'Set bino.binPath'
            );
            if (choice === 'Set bino.binPath') {
                await vscode.workspace.getConfiguration('bino').update('binPath', binary, vscode.ConfigurationTarget.Global);
            }
        }
        const version = await installedBinoVersion();
        await vscode.commands.executeCommand('setContext', BINO_INSTALLED_CONTEXT, version !== undefined);
        const choice = await vscode.window.showInformationMessage(
            `bino ${version ?? ''} is ready. Reload the window so every Bino feature uses it.`,
            'Reload Window'
        );
        if (choice === 'Reload Window') {
            await vscode.commands.executeCommand('workbench.action.reloadWindow');
        }
    } catch (err) {
        log.error(`bino download failed: ${err}`);
        const choice = await vscode.window.showErrorMessage(`Could not download bino: ${err instanceof Error ? err.message : err}`, 'Install Instructions');
        if (choice === 'Install Instructions') {
            await vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
        }
    }
}

/** Pick up a bino downloaded in an earlier session. Runs nothing. */
export function loadManagedBino(context: vscode.ExtensionContext): void {
    setManagedBinoPath(findManagedBino(path.join(context.globalStorageUri.fsPath, 'bin')));
}

/**
 * Check that bino runs, set the walkthrough context,
 * and offer a download when it is missing or older than this extension.
 */
export async function detectBino(context: vscode.ExtensionContext): Promise<void> {
    const version = await installedBinoVersion();
    await vscode.commands.executeCommand('setContext', BINO_INSTALLED_CONTEXT, version !== undefined);
    const wanted: string = context.extension.packageJSON?.version ?? '0.0.0';
    let message: string | undefined;
    if (version === undefined) {
        message = 'The bino CLI was not found.';
    } else if (isOlderVersion(version, wanted)) {
        message = `bino ${version} is older than this extension (${wanted}). Some features may not work.`;
    }
    if (!message) {
        return;
    }
    const choice = await vscode.window.showWarningMessage(message, 'Download bino', 'Install Instructions');
    if (choice === 'Download bino') {
        await vscode.commands.executeCommand('bino.downloadBino');
    } else if (choice === 'Install Instructions') {
        await vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
    }
}
