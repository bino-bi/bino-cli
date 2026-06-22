import * as vscode from 'vscode';
import { WorkspaceIndexer, KindInfo } from './indexer';
import { DataSourceWizardManager } from './wizard/wizardPanel';

/**
 * Capability category for each kind, mirroring the Go `builtinCategory`
 * (internal/mcp/server.go). The served `/kinds` endpoint only carries the
 * render-embeddable flag, not the category, so the palette categorizes built-in
 * kinds from this table; any kind not listed here (e.g. a plugin kind) falls
 * into OTHER_CATEGORY so it still appears.
 */
const KIND_CATEGORY: Record<string, string> = {
    DataSource: 'data',
    DataSet: 'data',
    ConnectionSecret: 'data',
    LayoutPage: 'layout',
    LayoutCard: 'layout',
    Text: 'embeddable',
    Table: 'embeddable',
    ChartStructure: 'embeddable',
    ChartTime: 'embeddable',
    Tree: 'embeddable',
    Grid: 'embeddable',
    Asset: 'embeddable',
    ReportArtefact: 'artefact',
    LiveReportArtefact: 'artefact',
    DocumentArtefact: 'artefact',
    ComponentStyle: 'config',
    Internationalization: 'config',
    ScalingGroup: 'config',
    SigningProfile: 'config',
};

/** Bucket for kinds with no known category (e.g. plugin-provided kinds). */
const OTHER_CATEGORY = 'other';

/** Display order and labels for the QuickPick category separators. */
const CATEGORY_ORDER: { id: string; label: string }[] = [
    { id: 'data', label: 'Data' },
    { id: 'embeddable', label: 'Components' },
    { id: 'layout', label: 'Layout' },
    { id: 'artefact', label: 'Artefacts' },
    { id: 'config', label: 'Configuration' },
    { id: OTHER_CATEGORY, label: 'Other' },
];

/**
 * Existing `bino add <kind>` command per built-in kind. Picking a non-data kind
 * routes to the matching guided terminal wizard (the fallback escape hatch the
 * Design-mode brief keeps). Casing is taken from the registered command ids
 * (note DataSource → addDatasource), so it is an explicit map, not derived.
 */
const ADD_COMMAND_FOR_KIND: Record<string, string> = {
    DataSet: 'bino.addDataset',
    DataSource: 'bino.addDatasource',
    ConnectionSecret: 'bino.addConnectionSecret',
    LayoutPage: 'bino.addLayoutPage',
    LayoutCard: 'bino.addLayoutCard',
    Table: 'bino.addTable',
    Text: 'bino.addText',
    ChartStructure: 'bino.addChartStructure',
    ChartTime: 'bino.addChartTime',
    ReportArtefact: 'bino.addReportArtefact',
    LiveReportArtefact: 'bino.addLiveReportArtefact',
    SigningProfile: 'bino.addSigningProfile',
    Asset: 'bino.addAsset',
    ComponentStyle: 'bino.addComponentStyle',
    Internationalization: 'bino.addInternationalization',
    ScalingGroup: 'bino.addScalingGroup',
};

/**
 * A palette entry for one manifest kind. `kindName`/`category` are added on top
 * of QuickPickItem (the base `kind` field is reserved by VS Code for separators).
 */
interface KindPick extends vscode.QuickPickItem {
    kindName: string;
    category: string;
}

/** Resolve a kind's category, falling back to the OTHER bucket. */
function categoryFor(kind: string): string {
    return KIND_CATEGORY[kind] ?? OTHER_CATEGORY;
}

/**
 * The "Add element" palette: a single entry point that lists every live manifest
 * kind grouped by capability category and routes the pick into the one existing
 * create path for that kind. Data kinds (DataSource/DataSet) open the wizard;
 * every other built-in kind opens its guided `bino add` terminal wizard. The kind
 * list is sourced from the backend, so plugin kinds appear with no code change.
 */
export class AddElementCommand {
    constructor(
        private readonly indexer: WorkspaceIndexer,
        private readonly wizard: DataSourceWizardManager,
        private readonly getIcon: (kind: string) => string,
    ) {}

    /** Show the palette and route the chosen kind to its create path. */
    async run(): Promise<void> {
        const kinds = await this.indexer.getKindInfos();
        if (kinds.length === 0) {
            vscode.window.showWarningMessage('Bino: could not load the manifest kind list.');
            return;
        }

        const picked = await vscode.window.showQuickPick(this.buildItems(kinds), {
            placeHolder: 'Add element — pick a manifest kind',
            title: 'Bino: Add Element',
            matchOnDescription: true,
            matchOnDetail: true,
        });
        if (!picked || !('kindName' in picked)) {
            return;
        }
        await this.createKind((picked as KindPick).kindName);
    }

    /** Build grouped QuickPick items (category separators + one item per kind). */
    private buildItems(kinds: KindInfo[]): vscode.QuickPickItem[] {
        const byCategory = new Map<string, KindInfo[]>();
        for (const k of kinds) {
            const cat = categoryFor(k.name);
            const bucket = byCategory.get(cat) ?? [];
            bucket.push(k);
            byCategory.set(cat, bucket);
        }

        const items: vscode.QuickPickItem[] = [];
        for (const { id, label } of CATEGORY_ORDER) {
            const bucket = byCategory.get(id);
            if (!bucket || bucket.length === 0) {
                continue;
            }
            items.push({ label, kind: vscode.QuickPickItemKind.Separator });
            for (const k of bucket.sort((a, b) => a.name.localeCompare(b.name))) {
                const item: KindPick = {
                    label: `$(${this.getIcon(k.name)}) ${k.name}`,
                    description: id,
                    kindName: k.name,
                    category: id,
                };
                items.push(item);
            }
        }
        return items;
    }

    /** Route a chosen kind into its existing create path. */
    private async createKind(kind: string): Promise<void> {
        // Data kinds flow through the introspect → typed-SELECT → preview wizard.
        if (kind === 'DataSource' || kind === 'DataSet') {
            this.wizard.openForDatabase();
            return;
        }

        // Every other built-in kind opens its guided `bino add` terminal wizard.
        const command = ADD_COMMAND_FOR_KIND[kind];
        if (command) {
            await vscode.commands.executeCommand(command);
            return;
        }

        // Plugin / unrecognized kinds have no guided create yet.
        vscode.window.showInformationMessage(
            `Bino: guided creation for ${kind} is not available yet — author it manually.`,
        );
    }
}
