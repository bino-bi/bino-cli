import * as vscode from 'vscode';

type ActionsTreeItem = CategoryItem | ScaffoldItem;

class CategoryItem extends vscode.TreeItem {
    constructor(
        public readonly categoryLabel: string,
        public readonly categoryId: string
    ) {
        super(categoryLabel, vscode.TreeItemCollapsibleState.Collapsed);
        this.contextValue = 'binoActionCategory';
    }
}

class ScaffoldItem extends vscode.TreeItem {
    constructor(
        label: string,
        public readonly commandId: string,
        icon: string,
        public readonly categoryId: string
    ) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.command = { command: commandId, title: label };
        this.iconPath = new vscode.ThemeIcon(icon);
        this.contextValue = 'binoScaffoldAction';
    }
}

interface ScaffoldEntry {
    label: string;
    commandId: string;
    icon: string;
}

const SCAFFOLD_CATEGORIES: { id: string; label: string; items: ScaffoldEntry[] }[] = [
    {
        id: 'data', label: 'Data', items: [
            { label: 'Add DataSource', commandId: 'bino.addDatasource', icon: 'database' },
            { label: 'Add DataSet', commandId: 'bino.addDataset', icon: 'table' },
            { label: 'Add ConnectionSecret', commandId: 'bino.addConnectionSecret', icon: 'lock' },
        ]
    },
    {
        id: 'layout', label: 'Layout', items: [
            { label: 'Add LayoutPage', commandId: 'bino.addLayoutPage', icon: 'layout' },
            { label: 'Add LayoutCard', commandId: 'bino.addLayoutCard', icon: 'credit-card' },
        ]
    },
    {
        id: 'visualization', label: 'Visualization', items: [
            { label: 'Add Table', commandId: 'bino.addTable', icon: 'list-flat' },
            { label: 'Add Text', commandId: 'bino.addText', icon: 'symbol-text' },
            { label: 'Add ChartStructure', commandId: 'bino.addChartStructure', icon: 'graph' },
            { label: 'Add ChartTime', commandId: 'bino.addChartTime', icon: 'graph' },
        ]
    },
    {
        id: 'reports', label: 'Reports', items: [
            { label: 'Add ReportArtefact', commandId: 'bino.addReportArtefact', icon: 'file-pdf' },
            { label: 'Add LiveReportArtefact', commandId: 'bino.addLiveReportArtefact', icon: 'browser' },
            { label: 'Add SigningProfile', commandId: 'bino.addSigningProfile', icon: 'key' },
        ]
    },
    {
        id: 'resources', label: 'Resources', items: [
            { label: 'Add Asset', commandId: 'bino.addAsset', icon: 'file-media' },
            { label: 'Add ComponentStyle', commandId: 'bino.addComponentStyle', icon: 'paintcan' },
            { label: 'Add Internationalization', commandId: 'bino.addInternationalization', icon: 'globe' },
        ]
    },
    {
        id: 'maintenance', label: 'Maintenance', items: [
            { label: 'Clean Cache', commandId: 'bino.cacheClean', icon: 'trash' },
            { label: 'Clean Cache (Global)', commandId: 'bino.cacheCleanGlobal', icon: 'trash' },
        ]
    },
];

/**
 * Tree data provider for the Scaffolding panel.
 * Shows `bino add` commands grouped by category.
 */
export class ActionsTreeProvider implements vscode.TreeDataProvider<ActionsTreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<ActionsTreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    getTreeItem(element: ActionsTreeItem): vscode.TreeItem {
        return element;
    }

    getChildren(element?: ActionsTreeItem): ActionsTreeItem[] {
        if (!element) {
            return SCAFFOLD_CATEGORIES.map(c => new CategoryItem(c.label, c.id));
        }
        if (element instanceof CategoryItem) {
            const cat = SCAFFOLD_CATEGORIES.find(c => c.id === element.categoryId);
            if (!cat) { return []; }
            return cat.items.map(i => new ScaffoldItem(i.label, i.commandId, i.icon, cat.id));
        }
        return [];
    }
}
