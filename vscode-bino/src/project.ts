import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';

const PROJECT_FILE = 'bino.toml';

function isInside(file: string, dir: string): boolean {
    const rel = path.relative(dir, file);
    return rel !== '' && !rel.startsWith('..') && !path.isAbsolute(rel);
}

/**
 * The current project rule: the project of the active file if it has one,
 * else the last current project, else the first project found.
 */
export function selectProject(roots: string[], activeFile: string | undefined, last: string | undefined): string | undefined {
    if (activeFile) {
        // The nearest bino.toml wins for nested projects.
        const containing = roots.filter(r => isInside(activeFile, r)).sort((a, b) => b.length - a.length);
        if (containing.length > 0) {
            return containing[0];
        }
    }
    if (last && roots.includes(last)) {
        return last;
    }
    return roots[0];
}

/** Walk up from dir to the nearest folder with a bino.toml. */
function findUpward(dir: string): string | undefined {
    let current = dir;
    for (;;) {
        if (fs.existsSync(path.join(current, PROJECT_FILE))) {
            return current;
        }
        const parent = path.dirname(current);
        if (parent === current) {
            return undefined;
        }
        current = parent;
    }
}

function activeFile(): string | undefined {
    const uri = vscode.window.activeTextEditor?.document.uri;
    return uri?.scheme === 'file' ? uri.fsPath : undefined;
}

/**
 * Tracks the current Bino project of the window. LSP, daemon, preview and
 * build all use it, so they always agree on one project.
 */
export class ProjectTracker implements vscode.Disposable {
    private roots: string[] = [];
    private currentRoot: string | undefined;
    private disposables: vscode.Disposable[] = [];
    private readonly _onDidChange = new vscode.EventEmitter<string>();
    /** Fires with the new root when the current project changes. */
    readonly onDidChange = this._onDidChange.event;

    /** Find the projects and pick the current one. Call once before use. */
    async init(): Promise<void> {
        await this.refreshRoots();
        this.currentRoot = selectProject(this.roots, activeFile(), undefined);
        const watcher = vscode.workspace.createFileSystemWatcher(`**/${PROJECT_FILE}`, false, true, false);
        this.disposables.push(
            this._onDidChange,
            watcher,
            watcher.onDidCreate(() => void this.refreshRoots()),
            watcher.onDidDelete(() => void this.refreshRoots().then(() => this.update())),
            vscode.window.onDidChangeActiveTextEditor(() => this.update())
        );
    }

    /** The current project root. */
    get current(): string | undefined {
        return this.currentRoot;
    }

    /** All project roots in the workspace. */
    get all(): readonly string[] {
        return this.roots;
    }

    /** Make root the current project, as if its file were active. */
    select(root: string): void {
        this.set(root);
    }

    private update(): void {
        this.set(selectProject(this.roots, activeFile(), this.currentRoot));
    }

    private set(root: string | undefined): void {
        if (!root || root === this.currentRoot) {
            return;
        }
        this.currentRoot = root;
        this._onDidChange.fire(root);
    }

    private async refreshRoots(): Promise<void> {
        const roots: string[] = [];
        const add = (r: string | undefined) => {
            if (r && !roots.includes(r)) {
                roots.push(r);
            }
        };
        // A folder opened inside a project belongs to that project.
        for (const folder of vscode.workspace.workspaceFolders ?? []) {
            add(findUpward(folder.uri.fsPath));
        }
        const files = await vscode.workspace.findFiles(`**/${PROJECT_FILE}`, '**/node_modules/**', 50);
        const depth = (p: string) => p.split(path.sep).length;
        files.map(f => path.dirname(f.fsPath))
            .sort((a, b) => depth(a) - depth(b) || a.localeCompare(b))
            .forEach(add);
        this.roots = roots;
    }

    /** Ask the user for a project and make it current. */
    async pick(): Promise<void> {
        await this.refreshRoots();
        if (this.roots.length === 0) {
            vscode.window.showInformationMessage('No Bino project (bino.toml) found in this workspace.');
            return;
        }
        const items = this.roots.map(r => ({
            label: path.basename(r),
            description: vscode.workspace.asRelativePath(r, true),
            detail: r === this.currentRoot ? 'current project' : undefined,
            root: r,
        }));
        const picked = await vscode.window.showQuickPick(items, { title: 'Bino: Switch Project', placeHolder: 'Select the Bino project' });
        if (picked) {
            this.select(picked.root);
        }
    }

    dispose(): void {
        this.disposables.forEach(d => d.dispose());
    }
}
