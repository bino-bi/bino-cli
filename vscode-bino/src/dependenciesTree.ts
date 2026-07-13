import * as vscode from 'vscode';
import { DaemonClient, RegistryPackage, RegistryParam } from './daemonClient';

type DepsTreeItem = PackageItem | DetailItem | InfoItem;

/** One dependency from bino.lock / bino.toml, with its install state. */
export class PackageItem extends vscode.TreeItem {
    constructor(readonly pkg: RegistryPackage) {
        super(pkg.name, vscode.TreeItemCollapsibleState.Collapsed);
        this.contextValue = 'binoPackage';
        if (!pkg.installed) {
            this.description = `${pkg.version || pkg.declaredRef || ''} (not installed)`.trim();
            this.iconPath = new vscode.ThemeIcon('warning', new vscode.ThemeColor('list.warningForeground'));
            this.tooltip = `${pkg.name} is declared but not installed — run "Bino: Install Dependencies from Lockfile".`;
        } else {
            this.description = pkg.tag ? `${pkg.version} (${pkg.tag})` : `${pkg.version} (pinned)`;
            this.iconPath = new vscode.ThemeIcon('package');
            this.tooltip = [
                pkg.name,
                `Kind: ${pkg.kind ?? 'unknown'}`,
                `Version: ${pkg.version}${pkg.tag ? ` (follows tag '${pkg.tag}')` : ' (pinned)'}`,
                pkg.direct ? 'Direct dependency' : 'Transitive dependency',
                `Path: ${pkg.path}`,
            ].join('\n');
        }
    }
}

/** A static detail row under a package. */
class DetailItem extends vscode.TreeItem {
    constructor(label: string, description: string, icon: string, command?: vscode.Command) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.description = description;
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoPackageDetail';
        if (command) {
            this.command = command;
        }
    }
}

/** A status / call-to-action row shown instead of packages. */
class InfoItem extends vscode.TreeItem {
    constructor(label: string, icon: string, commandId?: string) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoDepsInfo';
        if (commandId) {
            this.command = { command: commandId, title: label };
        }
    }
}

/**
 * Tree data provider for the Dependencies panel: the project's registry
 * packages from the daemon's offline /registry/packages report. Refreshes on
 * the registry-changed SSE event and gates on the daemon capability so a stale
 * daemon surfaces a restart nudge instead of an empty view.
 */
export class DependenciesTreeProvider implements vscode.TreeDataProvider<DepsTreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<DepsTreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    constructor(private readonly daemonClient: () => DaemonClient | undefined) {}

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(element: DepsTreeItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: DepsTreeItem): Promise<DepsTreeItem[]> {
        if (element) {
            return element instanceof PackageItem ? this.getPackageDetails(element.pkg) : [];
        }

        const client = this.daemonClient();
        if (!client || !client.isConnected) {
            return [new InfoItem('Daemon not connected — dependencies unavailable', 'plug', 'bino.restartDaemon')];
        }
        if (!client.hasCapability('registry-packages')) {
            return [new InfoItem('Registry view needs a newer daemon — click to restart', 'warning', 'bino.restartDaemon')];
        }

        const result = await client.getRegistryPackages();
        if (!result) {
            return [new InfoItem('Failed to load dependencies', 'error', 'bino.registryRefresh')];
        }
        if (result.error) {
            return [new InfoItem(`bino.lock: ${result.error}`, 'error')];
        }
        if (result.packages.length === 0) {
            return [new InfoItem('No dependencies — search the registry to add one', 'search', 'bino.registrySearch')];
        }
        return result.packages.map((pkg) => new PackageItem(pkg));
    }

    private getPackageDetails(pkg: RegistryPackage): DepsTreeItem[] {
        const items: DepsTreeItem[] = [];
        if (pkg.kind) {
            items.push(new DetailItem('Kind', pkg.kind, 'symbol-class'));
        }
        if (pkg.path) {
            items.push(new DetailItem('Path', pkg.path, 'go-to-file', {
                command: 'bino.registryOpenPackageFile',
                title: 'Open Installed File',
                arguments: [new PackageItem(pkg)],
            }));
        }
        for (const param of pkg.params ?? []) {
            items.push(new DetailItem(param.name, describeParam(param), 'symbol-parameter'));
        }
        for (const dep of pkg.dependencies ?? []) {
            items.push(new DetailItem('Depends on', dep, 'references'));
        }
        return items;
    }
}

function describeParam(p: RegistryParam): string {
    const parts = [p.type || 'string'];
    if (p.required && p.default === undefined) {
        parts.push('required');
    } else if (p.default !== undefined) {
        parts.push(`default: ${p.default}`);
    }
    return parts.join(', ');
}
