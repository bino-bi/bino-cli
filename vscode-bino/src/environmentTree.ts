import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as path from 'path';
import { WorkspaceIndexer } from './indexer';
import { BinoValidator } from './validation';

type EnvTreeItem = InfoItem | SettingsGroupItem | SettingItem;

class InfoItem extends vscode.TreeItem {
    constructor(label: string, description: string, icon: string, commandId?: string) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.description = description;
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoEnvInfo';
        if (commandId) {
            this.command = { command: commandId, title: label };
        }
    }
}

class SettingsGroupItem extends vscode.TreeItem {
    constructor() {
        super('Settings', vscode.TreeItemCollapsibleState.Collapsed);
        this.iconPath = new vscode.ThemeIcon('gear');
        this.contextValue = 'binoEnvSettingsGroup';
    }
}

class SettingItem extends vscode.TreeItem {
    constructor(label: string, commandId: string, icon: string) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.command = { command: commandId, title: label };
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoEnvSetting';
    }
}

/**
 * Tree data provider for the Environment panel.
 * Shows CLI version, project info, document/error counts, and settings links.
 */
export class EnvironmentTreeProvider implements vscode.TreeDataProvider<EnvTreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<EnvTreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    private cliVersion: string | undefined;

    constructor(
        private indexer: WorkspaceIndexer,
        private validator: BinoValidator
    ) {
        // Refresh when index or diagnostics change
        indexer.onDidFinishIndex(() => this._onDidChangeTreeData.fire());
        validator.onDidChangeDiagnostics(() => this._onDidChangeTreeData.fire());

        // Detect CLI version on construction
        this.detectCliVersion();
    }

    refresh(): void {
        this.detectCliVersion();
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(element: EnvTreeItem): vscode.TreeItem {
        return element;
    }

    getChildren(element?: EnvTreeItem): EnvTreeItem[] {
        if (!element) {
            return this.getRootItems();
        }
        if (element instanceof SettingsGroupItem) {
            return this.getSettingsItems();
        }
        return [];
    }

    private getRootItems(): EnvTreeItem[] {
        const items: EnvTreeItem[] = [];

        // CLI version
        const version = this.cliVersion || 'Not Found';
        const versionIcon = this.cliVersion ? 'check' : 'warning';
        items.push(new InfoItem('Bino CLI', version, versionIcon));

        // Project root
        const projectRoot = this.indexer.getProjectRootForUri();
        if (projectRoot) {
            const folders = vscode.workspace.workspaceFolders;
            let displayPath = projectRoot;
            if (folders && folders.length > 0) {
                const root = folders[0].uri.fsPath;
                if (projectRoot.startsWith(root)) {
                    displayPath = './' + projectRoot.substring(root.length + 1) || '.';
                    if (displayPath === './') { displayPath = '.'; }
                }
            }
            items.push(new InfoItem('Project', displayPath, 'folder'));
        }

        // Document count
        const docs = this.indexer.getDocuments();
        items.push(new InfoItem('Documents', `${docs.length}`, 'file-code'));

        // Error/warning count
        const summary = this.validator.getWorkspaceSummary();
        const errWarn = `${summary.errors} errors / ${summary.warnings} warnings`;
        const diagIcon = summary.errors > 0 ? 'error' : summary.warnings > 0 ? 'warning' : 'pass';
        items.push(new InfoItem('Diagnostics', errWarn, diagIcon, 'bino.validateWorkspace'));

        // Settings group
        items.push(new SettingsGroupItem());

        return items;
    }

    private getSettingsItems(): SettingItem[] {
        return [
            new SettingItem('Edit CLI Path', 'bino.openSettings', 'edit'),
            new SettingItem('Edit Preview Port', 'bino.openSettings', 'edit'),
            new SettingItem('Toggle Validate on Save', 'bino.openSettings', 'edit'),
        ];
    }

    private detectCliVersion(): void {
        const config = vscode.workspace.getConfiguration('bino');
        const binPath = config.get<string>('binPath');
        const bino = binPath && binPath.trim() ? binPath : 'bino';

        try {
            const result = cp.execSync(`${bino} version`, {
                timeout: 5000,
                encoding: 'utf8',
                stdio: ['pipe', 'pipe', 'pipe'],
            });
            // Parse version from output like "bino version 0.25.0"
            const match = result.trim().match(/(\d+\.\d+\.\d+\S*)/);
            this.cliVersion = match ? `v${match[1]}` : result.trim();
        } catch {
            this.cliVersion = undefined;
        }
    }
}
