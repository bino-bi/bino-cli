import * as vscode from 'vscode';
import { BinoPreviewManager, PreviewStatus } from './preview';

type PreviewTreeItem = GroupItem | ActionItem;

class GroupItem extends vscode.TreeItem {
    constructor(
        public readonly groupLabel: string,
        public readonly groupId: string
    ) {
        super(groupLabel, vscode.TreeItemCollapsibleState.Expanded);
        this.contextValue = 'binoPreviewGroup';
    }
}

class ActionItem extends vscode.TreeItem {
    constructor(
        label: string,
        public readonly commandId: string,
        icon: string,
        public readonly groupId: string,
        tooltip?: string
    ) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.command = { command: commandId, title: label };
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoPreviewAction';
        if (tooltip) {
            this.tooltip = tooltip;
        }
    }
}

/**
 * Tree data provider for the Preview & Build panel.
 * Shows preview server controls, build actions, validation, and graph commands.
 */
export class PreviewTreeProvider implements vscode.TreeDataProvider<PreviewTreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<PreviewTreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    private previewStatus: PreviewStatus = 'stopped';

    constructor(private previewManager: BinoPreviewManager) {
        previewManager.onStatusChange((status) => {
            this.previewStatus = status;
            this._onDidChangeTreeData.fire();
        });
    }

    getTreeItem(element: PreviewTreeItem): vscode.TreeItem {
        return element;
    }

    getChildren(element?: PreviewTreeItem): PreviewTreeItem[] {
        if (!element) {
            return this.getRootGroups();
        }
        if (element instanceof GroupItem) {
            return this.getGroupChildren(element.groupId);
        }
        return [];
    }

    private getRootGroups(): GroupItem[] {
        return [
            new GroupItem('Preview Server', 'preview'),
            new GroupItem('Build', 'build'),
            new GroupItem('Validate', 'validate'),
            new GroupItem('Graph', 'graph'),
        ];
    }

    private getGroupChildren(groupId: string): ActionItem[] {
        switch (groupId) {
            case 'preview':
                return this.getPreviewItems();
            case 'build':
                return this.getBuildItems();
            case 'validate':
                return this.getValidateItems();
            case 'graph':
                return this.getGraphItems();
            default:
                return [];
        }
    }

    private getPreviewItems(): ActionItem[] {
        const running = this.previewStatus === 'running';
        const starting = this.previewStatus === 'starting';
        const items: ActionItem[] = [];

        if (!running && !starting) {
            items.push(new ActionItem('Start Preview', 'bino.startPreview', 'play', 'preview'));
        }
        if (running || starting) {
            items.push(new ActionItem('Stop Preview', 'bino.stopPreview', 'debug-stop', 'preview'));
            items.push(new ActionItem('Restart', 'bino.restartPreview', 'debug-restart', 'preview'));
        }
        if (running) {
            items.push(new ActionItem('Open in Browser', 'bino.openPreviewInBrowser', 'link-external', 'preview'));
            items.push(new ActionItem('Open in Webview', 'bino.openPreviewInWebview', 'window', 'preview'));
        }

        return items;
    }

    private getBuildItems(): ActionItem[] {
        return [
            new ActionItem('Build All Artefacts', 'bino.build', 'package', 'build'),
            new ActionItem('Build Artefact...', 'bino.buildArtefact', 'package', 'build', 'Select a specific artefact to build'),
        ];
    }

    private getValidateItems(): ActionItem[] {
        return [
            new ActionItem('Validate Workspace', 'bino.validateWorkspace', 'check', 'validate'),
            new ActionItem('Validate with Queries', 'bino.validateWithQueries', 'checklist', 'validate', 'Validate with --execute-queries'),
        ];
    }

    private getGraphItems(): ActionItem[] {
        return [
            new ActionItem('Show Graph (Tree)', 'bino.showGraphTree', 'type-hierarchy', 'graph'),
            new ActionItem('Show Graph (Flat)', 'bino.showGraphFlat', 'list-flat', 'graph'),
        ];
    }
}
