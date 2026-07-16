import * as vscode from 'vscode';
import * as path from 'path';
import { WorkspaceIndexer, KindInfo } from './indexer';
import { DataSourceWizardManager } from './wizard/wizardPanel';
import { AuthoringClient, formatEditDiagnostics } from './authoringClient';
import { SchemaResolver, FieldDef } from './schemaResolver';

/**
 * Display order and human labels for the capability categories the backend
 * serves on `bino://kinds` (data | layout | embeddable | artefact | config) plus
 * a trailing bucket for any category the backend introduces later. This is only
 * presentation: the category a kind belongs to comes from the served
 * `KindInfo.category`, never a hand-maintained kind→category table.
 */
const CATEGORY_ORDER: { id: string; label: string }[] = [
    { id: 'data', label: 'Data' },
    { id: 'embeddable', label: 'Components' },
    { id: 'layout', label: 'Layout' },
    { id: 'artefact', label: 'Artefacts' },
    { id: 'config', label: 'Configuration' },
];

/** Fallback label for a served category not in CATEGORY_ORDER. */
const OTHER_LABEL = 'Other';

/**
 * A palette entry for one manifest kind. `kindName`/`category` are added on top
 * of QuickPickItem (the base `kind` field is reserved by VS Code for separators).
 */
interface KindPick extends vscode.QuickPickItem {
    kindName: string;
    category: string;
}

/** A children-pick entry: one existing component document. */
interface ChildPick extends vscode.QuickPickItem {
    childKind: string;
}

/**
 * The "Add element" palette: a single entry point that lists every live manifest
 * kind grouped by its served capability category and creates the chosen kind
 * through the one authoring path. Data kinds (DataSource/DataSet) open the
 * introspect→typed-SELECT wizard; every other kind — built-in or plugin — runs a
 * schema-driven guided form and is created via the AuthoringClient (the Go
 * create path: envelope build → schema validation → atomic write), then the
 * index refreshes and the new file opens. The kind list and its categories are
 * sourced from the backend, so plugin kinds appear and are created with no
 * extension change.
 */
export class AddElementCommand {
    private readonly authoring: AuthoringClient;
    private readonly schema: SchemaResolver;
    private schemaLoaded: boolean;

    constructor(
        private readonly indexer: WorkspaceIndexer,
        private readonly wizard: DataSourceWizardManager,
        private readonly getIcon: (kind: string) => string,
        extensionPath: string,
        private readonly runAdd: (kind: string) => void,
    ) {
        this.authoring = new AuthoringClient(indexer);
        this.schema = new SchemaResolver(extensionPath);
        this.schemaLoaded = this.schema.load();
    }

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
        await this.createKind(picked as KindPick);
    }

    /** Build grouped QuickPick items (category separators + one item per kind). */
    private buildItems(kinds: KindInfo[]): vscode.QuickPickItem[] {
        const byCategory = new Map<string, KindInfo[]>();
        for (const k of kinds) {
            const cat = k.category || 'other';
            const bucket = byCategory.get(cat) ?? [];
            bucket.push(k);
            byCategory.set(cat, bucket);
        }

        // Known categories first (in display order), then any unexpected ones.
        const order = [...CATEGORY_ORDER];
        for (const cat of [...byCategory.keys()].sort()) {
            if (!order.some(o => o.id === cat)) {
                order.push({ id: cat, label: OTHER_LABEL });
            }
        }

        const items: vscode.QuickPickItem[] = [];
        for (const { id, label } of order) {
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

    /** Route a chosen kind into its create path. */
    private async createKind(pick: KindPick): Promise<void> {
        const kind = pick.kindName;

        // Data kinds flow through the introspect → typed-SELECT → preview wizard.
        if (kind === 'DataSource' || kind === 'DataSet') {
            this.wizard.openForDatabase();
            return;
        }

        // Kinds whose schema requires an object/array field the scalar form cannot
        // fill (e.g. LayoutPage.children, SigningProfile.certificate, Asset.source)
        // can't be completed by the guided form, so the form+create would always
        // fail with schema diagnostics. Route them to `bino add <kind>` — the
        // interactive CLI scaffolder — as the escape hatch instead.
        const fields = this.schemaLoaded ? this.schema.getFieldsForKind(kind) : [];
        if (this.needsGuidedWidgets(fields)) {
            vscode.window.showInformationMessage(
                `Bino: ${kind} needs the interactive wizard — running \`bino add ${kind.toLowerCase()}\` in the terminal.`
            );
            this.runAdd(kind);
            return;
        }

        // Every other kind (built-in or plugin) is created through a schema-driven
        // guided form and the one authoring path.
        await this.createViaForm(kind, pick.category, fields);
    }

    /**
     * Run a schema-driven guided form for `kind` and create the manifest through
     * the AuthoringClient. The form prompts for the name, a data binding (for
     * embeddable components that take a dataset), and each required scalar field;
     * the spec is then validated and written by the Go create path, which returns
     * diagnostics if the result is incomplete. On success the index refreshes and
     * the new file opens.
     */
    private async createViaForm(kind: string, category: string, fields: FieldDef[]): Promise<void> {
        const name = await this.promptName(kind);
        if (name === undefined) {
            return;
        }

        const spec: Record<string, unknown> = {};

        // Embeddable components bind to a dataset/datasource; offer the binding as
        // the first guided step when the kind's spec carries a `dataset` field.
        const hasDataset = fields.some(f => f.key === 'dataset');
        if (category === 'embeddable' && hasDataset) {
            const dataset = await this.promptDataset();
            if (dataset === null) {
                return; // user cancelled the binding step
            }
            if (dataset) {
                spec.dataset = dataset;
            }
        }

        // Layout containers reference existing components as children; offer a
        // skippable multi-pick so a new page/card starts out usable.
        if (category === 'layout' && fields.some(f => f.key === 'children')) {
            const children = await this.promptChildren(kind);
            if (children === null) {
                return; // user cancelled
            }
            spec.children = children;
        }

        // Prompt for each required scalar/enum field (objects and arrays are left
        // for the designer's rich widgets; the create path reports any still
        // missing as diagnostics).
        for (const field of fields) {
            if (field.key === 'dataset' && spec.dataset !== undefined) {
                continue;
            }
            if (!field.required || !this.isScalarField(field)) {
                continue;
            }
            const value = await this.promptField(kind, field);
            if (value === undefined) {
                return; // cancelled
            }
            if (value !== null) {
                spec[field.key] = value;
            }
        }

        const result = await this.authoring.create({ kind, name, spec });
        if (!result.ok) {
            // The guided form couldn't produce a schema-valid spec — typically a
            // conditional requirement the flat field list can't express (e.g.
            // ConnectionSecret type:postgres needs a `postgres` object). Fall back
            // to `bino add <kind>` rather than dead-ending on the diagnostics.
            vscode.window.showWarningMessage(
                `Bino: could not create ${kind} (${formatEditDiagnostics(result)}) — continuing with \`bino add ${kind.toLowerCase()}\` in the terminal.`
            );
            this.runAdd(kind);
            return;
        }

        await this.indexer.refreshIndex();
        await this.openCreatedFile(result.file);
        vscode.window.showInformationMessage(`Bino: created ${result.file}`);
    }

    /** Prompt for a unique metadata.name for the new manifest. */
    private async promptName(kind: string): Promise<string | undefined> {
        const existing = new Set(this.indexer.getDocuments([kind]).map(d => d.name));
        return vscode.window.showInputBox({
            title: `New ${kind}`,
            prompt: `Name for the new ${kind}`,
            value: this.suggestName(kind, existing),
            validateInput: (v) => {
                const t = v.trim();
                if (!t) { return 'A name is required.'; }
                if (existing.has(t)) { return `A ${kind} named "${t}" already exists.`; }
                return undefined;
            },
        }).then(v => (v === undefined ? undefined : v.trim()));
    }

    /** Suggest a unique default name like `table_1`. */
    private suggestName(kind: string, existing: Set<string>): string {
        const base = kind.toLowerCase();
        for (let i = 1; ; i++) {
            const candidate = `${base}_${i}`;
            if (!existing.has(candidate)) {
                return candidate;
            }
        }
    }

    /**
     * Prompt for a dataset/datasource to bind. Returns the chosen reference, an
     * empty string when the user skips the binding, or null when they cancel.
     */
    private async promptDataset(): Promise<string | null> {
        const datasets = this.indexer.getDocuments(['DataSet']).map(d => d.name);
        const sources = this.indexer.getDocuments(['DataSource']).map(d => `$${d.name}`);
        const items: vscode.QuickPickItem[] = [
            ...datasets.map(label => ({ label, description: 'DataSet' })),
            ...sources.map(label => ({ label, description: 'DataSource' })),
            { label: '$(circle-slash) Skip — bind later', description: '' },
        ];
        const picked = await vscode.window.showQuickPick(items, {
            title: 'Bind data',
            placeHolder: datasets.length + sources.length > 0
                ? 'Pick a dataset or datasource to bind'
                : 'No datasets yet — skip and bind later',
        });
        if (!picked) {
            return null;
        }
        return picked.label.startsWith('$(circle-slash)') ? '' : picked.label;
    }

    /**
     * Prompt for existing components to reference as children of a new layout
     * container. Returns `{kind, ref}` entries — empty when the pick is skipped
     * (confirmed with nothing selected) or no components exist yet — or null
     * when the user cancels.
     */
    private async promptChildren(kind: string): Promise<Array<{ kind: string; ref: string }> | null> {
        const childKinds = this.schema.getLayoutChildKinds();
        if (childKinds.length === 0) {
            return [];
        }
        const components = this.indexer.getDocuments(childKinds);
        if (components.length === 0) {
            return [];
        }
        const items: ChildPick[] = components.map(d => ({
            label: d.name,
            description: d.kind,
            childKind: d.kind,
        }));
        const picked = await vscode.window.showQuickPick(items, {
            title: `New ${kind}: children`,
            placeHolder: 'Pick components to show on it (confirm with none selected to skip)',
            canPickMany: true,
        });
        if (picked === undefined) {
            return null;
        }
        return picked.map(p => ({ kind: p.childKind, ref: p.label }));
    }

    /**
     * Prompt for a single scalar field. Enums become a QuickPick, booleans a
     * yes/no pick, strings/numbers an InputBox. Returns the value, null when the
     * field is left empty, or undefined when cancelled.
     */
    private async promptField(kind: string, field: FieldDef): Promise<unknown> {
        const detail = field.description ? ` — ${field.description}` : '';

        if (field.enumValues && field.enumValues.length > 0) {
            const picked = await vscode.window.showQuickPick(field.enumValues, {
                title: `${kind}: ${field.key}`,
                placeHolder: `Choose ${field.key}${detail}`,
            });
            return picked === undefined ? undefined : picked;
        }

        if (field.type === 'boolean') {
            const picked = await vscode.window.showQuickPick(['true', 'false'], {
                title: `${kind}: ${field.key}`,
                placeHolder: `${field.key}${detail}`,
            });
            if (picked === undefined) { return undefined; }
            return picked === 'true';
        }

        const isNumber = field.type === 'number' || field.type === 'integer';
        const raw = await vscode.window.showInputBox({
            title: `${kind}: ${field.key}`,
            prompt: `${field.key}${detail}`,
            validateInput: (v) => {
                if (isNumber && v.trim() && Number.isNaN(Number(v))) {
                    return 'Enter a number.';
                }
                return undefined;
            },
        });
        if (raw === undefined) { return undefined; }
        const trimmed = raw.trim();
        if (!trimmed) { return null; }
        return isNumber ? Number(trimmed) : trimmed;
    }

    /**
     * True when the kind has a required field the guided form cannot fill: a
     * required object/array that isn't the `dataset` binding (which the form does
     * fill via promptDataset). Such kinds need the designer's rich widgets, so the
     * palette routes them to the `bino add <kind>` escape hatch instead of running
     * a form that could only fail schema validation.
     */
    private needsGuidedWidgets(fields: FieldDef[]): boolean {
        return fields.some(f => f.required && f.key !== 'dataset' && !this.isScalarField(f));
    }

    /** True for fields a simple prompt can fill (scalar or enum, not object/array). */
    private isScalarField(field: FieldDef): boolean {
        if (field.enumValues && field.enumValues.length > 0) {
            return true;
        }
        return field.type === 'string'
            || field.type === 'number'
            || field.type === 'integer'
            || field.type === 'boolean';
    }

    /** Open the newly created manifest (path is project-root relative). */
    private async openCreatedFile(file: string): Promise<void> {
        const root = this.indexer.getProjectRootForUri();
        const abs = path.isAbsolute(file) ? file : root ? path.join(root, file) : file;
        try {
            const doc = await vscode.workspace.openTextDocument(abs);
            await vscode.window.showTextDocument(doc, { preview: false });
        } catch {
            // The create reported the file; if opening fails (e.g. a race with the
            // watcher), the success toast still names it.
        }
    }
}
