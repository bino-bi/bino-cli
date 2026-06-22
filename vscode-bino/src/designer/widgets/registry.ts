import { FieldDef } from '../../schemaResolver';

/** A standard dataset column (the 16 IBCS measure slots + dims + meta). */
export interface StandardColumn {
    name: string;
    kind: string;
    group: string;
    pair?: string;
}

/** The data-binding context a column-aware widget reads to offer choices. */
export interface Binding {
    /** The bound dataset/datasource name (DataSet name or $DataSource). */
    dataset: string;
    /** Live column names for the bound dataset (from /columns). */
    columns: string[];
    /** The canonical standard dataset columns (from /dataset-schema). */
    standardColumns: StandardColumn[];
}

/**
 * The render context handed to a widget: the current YAML value for the field,
 * the (optional) data binding, and the edit callback. `onChange` is the single
 * sink every widget reports through — the host wires it to the brief-01
 * AuthoringClient, so a widget never talks to VS Code or the engine directly.
 */
export interface WidgetContext<T = unknown> {
    /** The field this widget renders (its key/path drive the edit). */
    field: FieldDef;
    /** Current YAML value (string | number | boolean | array | object | null). */
    value: T;
    /** Binding context for column-aware widgets; absent until a dataset is picked. */
    binding?: Binding;
    /**
     * The whole component spec as plain JSON, so a widget can read a sibling
     * field (e.g. the variance widget constrains its slots to `spec.scenarios`).
     */
    spec?: Record<string, unknown>;
    /** Report a new value → host emits editField → AuthoringClient.edit. */
    onChange(next: T): void;
}

/**
 * The webview hook a widget produces: the HTML for its value control. The host
 * renders this into the form; interactive controls report changes by posting
 * `editField` (the webview routes any `[data-designer-edit]` element's change to
 * `WidgetContext.onChange`). A `setup` hook can wire bespoke client behavior in
 * a future iteration; the reference widget needs only `html`.
 */
export interface WidgetHandle {
    /** HTML fragment rendered into the field's value cell. */
    html: string;
}

/**
 * The widget-plugin contract. Brief 04 implements instances (scenario, variance,
 * stack, edges, aggregate); this brief ships the registry plus one reference
 * widget (enum-select) to prove it. A widget claims a field via `match` and
 * renders its control via `render`.
 */
export interface DesignerWidget<T = unknown> {
    /** Stable id for diagnostics. */
    readonly id: string;
    /** Claim a field, e.g. `kind === 'Table' && field.key === 'scenarios'`. */
    match(ctx: { kind: string; field: FieldDef }): boolean;
    /** Render the field's value control. */
    render(ctx: WidgetContext<T>): WidgetHandle;
}

/**
 * A data-driven registry mapping a matcher to a widget. The form renderer asks
 * `resolve(kind, field)` per field; the first matching widget wins, a miss falls
 * back to the generic control. Kept as an ordered list (not a switch) so brief 04
 * registers widgets without touching the shell.
 */
export class WidgetRegistry {
    private readonly widgets: DesignerWidget[] = [];

    /** Register a widget. Later registrations match only if earlier ones miss. */
    register(widget: DesignerWidget): void {
        this.widgets.push(widget);
    }

    /** Find the widget that claims this field, or undefined for the generic control. */
    resolve(kind: string, field: FieldDef): DesignerWidget | undefined {
        return this.widgets.find(w => w.match({ kind, field }));
    }
}
