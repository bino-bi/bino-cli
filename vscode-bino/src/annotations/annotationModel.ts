/**
 * The first template engine that names the annotation of each mark, so a click
 * on a mark can open it. A copy of AnnotationNamesMinVersion in bino-cli's
 * internal/engine/compat.go; a test there keeps the two equal.
 */
export const ANNOTATION_NAMES_MIN_ENGINE = 'v1.0.0-next.88';

/** The marks of an Annotation, `marker` first: it is the default. */
export const MARKS = ['marker', 'frame', 'arrow', 'difference', 'pointer', 'bracket'];

/** The kinds an Annotation can point at. */
export const TARGET_KINDS = ['ChartTime', 'ChartStructure', 'ChartScatter', 'ChartBubble', 'ChartBullet', 'Table', 'Text', 'Tree', 'Grid', 'SmallMultiple'];

/** The spec keys that only some marks read. Keys not listed here apply to every mark. */
export const MARK_FIELDS: Record<string, string[]> = {
    to: ['arrow', 'difference', 'bracket', 'frame'],
    markText: ['frame', 'bracket'],
    markTextPosition: ['frame'],
    percent: ['difference', 'bracket'],
    digits: ['difference'],
    sentiment: ['difference'],
    highlight: ['difference'],
};

/** Marks that cannot do without `to`. */
export const TO_REQUIRED = ['arrow', 'difference', 'bracket'];

/** Whether a spec key applies to a mark. */
export function fieldApplies(key: string, mark: string | undefined): boolean {
    const marks = MARK_FIELDS[key];
    return !marks || marks.includes(mark || 'marker');
}

/**
 * The point fields the engine reports per kind. A kind without an entry has
 * no data points: Text, Tree and Grid take a mark on the whole component, a
 * SmallMultiple only through one panel.
 */
const POINT_FIELDS: Record<string, string[]> = {
    ChartTime: ['date', 'scenario', 'category', 'row'],
    ChartStructure: ['rowGroup', 'category', 'subCategory', 'scenario', 'row'],
    ChartScatter: ['rowGroup', 'category', 'subCategory'],
    ChartBubble: ['rowGroup', 'category', 'subCategory'],
    ChartBullet: ['rowGroup', 'category', 'subCategory', 'scenario'],
    Table: ['rowGroup', 'category', 'subCategory', 'date', 'scenario', 'row'],
};

/** The point fields of a target, `part` included where it applies. For a SmallMultiple panel, `inner` is the kind of its panels. */
export function pointFieldsFor(kind: string, inner?: string): string[] {
    const fields = POINT_FIELDS[kind === 'SmallMultiple' ? inner ?? '' : kind];
    if (!fields) { return []; }
    return kind === 'Table' || inner === 'Table' ? fields : [...fields, 'part'];
}

/** Fixed values of a point field, or undefined when they come from the data. */
export function fixedValues(field: string, kind: string): string[] | undefined {
    if (field === 'row') { return ['subtotal', 'total']; }
    if (field === 'part') { return kind === 'Table' ? ['element'] : ['element', 'label', 'total']; }
    return undefined;
}

/** A scenario column of the data: ac1, pl2, ... */
export function isScenarioColumn(column: string): boolean {
    return /^(ac|pp|fc|pl|bu|py)[1-4]$/.test(column);
}

/**
 * The distinct values of a point field in some rows, sorted, at most `cap`.
 * A date is cut to the day; a period start keeps its time when it has one.
 */
export function distinctValues(rows: Record<string, unknown>[], field: string, cap: number): { values: string[]; truncated: boolean } {
    const seen = new Set<string>();
    for (const row of rows) {
        const raw = row[field];
        if (raw === null || raw === undefined || raw === '') { continue; }
        let value = String(raw);
        if (field === 'date') { value = value.replace(/T00:00:00(\.0+)?(Z|[+-]00:?00)?$/, ''); }
        seen.add(value);
    }
    const all = [...seen].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }));
    return { values: all.slice(0, cap), truncated: all.length > cap };
}

/** A component on a page, by kind and name. */
export interface PlacedComponent {
    kind: string;
    name: string;
}

/** A page with the components on it, and the artefacts that print it. */
export interface PageInfo {
    name: string;
    artefacts: string[];
    components: PlacedComponent[];
}

/** One parsed manifest, as JSON. */
export interface ManifestJson {
    kind?: unknown;
    metadata?: { name?: unknown };
    spec?: Record<string, unknown>;
}

/**
 * The pages of a project with every component on them: the children of the
 * page, of the LayoutCards and Grids in it, and of referenced cards, by the
 * name a target uses. Each page lists the ReportArtefacts that name it.
 */
export function collectPages(manifests: ManifestJson[]): PageInfo[] {
    const byKey = new Map<string, ManifestJson>();
    for (const m of manifests) {
        if (typeof m.kind === 'string' && typeof m.metadata?.name === 'string') { byKey.set(`${m.kind}:${m.metadata.name}`, m); }
    }
    // layoutPages holds names or glob patterns, one string or a list; absent means every page
    const selectors: { artefact: string; patterns: RegExp[] }[] = [];
    for (const m of manifests) {
        if (m.kind !== 'ReportArtefact' || typeof m.metadata?.name !== 'string') { continue; }
        const raw = m.spec?.layoutPages;
        const entries = typeof raw === 'string' ? [raw] : asArray(raw);
        const names = entries
            .map(entry => typeof entry === 'string' ? entry : typeof (entry as { page?: unknown })?.page === 'string' ? (entry as { page: string }).page : undefined)
            .map(name => name?.trim())
            .filter((p): p is string => !!p);
        // an empty list selects every page, like no list
        const patterns = (names.length ? names : ['*']).map(globToRegExp);
        selectors.push({ artefact: m.metadata.name, patterns });
    }

    // children of pages, cards and grids, and the nodes of a tree
    const walk = (spec: Record<string, unknown> | undefined, out: PlacedComponent[], seen: Set<string>) => {
        for (const child of [...asArray(spec?.children), ...asArray(spec?.nodes)]) {
            if (!child || typeof child !== 'object') { continue; }
            const c = child as { kind?: unknown; ref?: unknown; metadata?: { name?: unknown }; spec?: Record<string, unknown> };
            if (typeof c.kind !== 'string') { continue; }
            const name = typeof c.ref === 'string' ? c.ref : typeof c.metadata?.name === 'string' ? c.metadata.name : undefined;
            if (name) { out.push({ kind: c.kind, name }); }
            if (typeof c.ref !== 'string') {
                walk(c.spec, out, seen);
                continue;
            }
            // a ref is walked once per page, which also stops a cycle; its own children win over the referenced ones
            const key = `${c.kind}:${c.ref}`;
            if (seen.has(key)) { continue; }
            seen.add(key);
            const own = c.spec && (Array.isArray(c.spec.children) || Array.isArray(c.spec.nodes)) ? c.spec : undefined;
            walk(own ?? byKey.get(key)?.spec, out, seen);
        }
    };

    const pages: PageInfo[] = [];
    for (const m of manifests) {
        if (m.kind !== 'LayoutPage' || typeof m.metadata?.name !== 'string') { continue; }
        const components: PlacedComponent[] = [];
        walk(m.spec, components, new Set());
        const name = m.metadata.name;
        pages.push({ name, artefacts: selectors.filter(sel => sel.patterns.some(re => re.test(name))).map(sel => sel.artefact), components });
    }
    return pages;
}

function globToRegExp(pattern: string): RegExp {
    const body = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
    return new RegExp(`^${body}$`);
}

/** The kind of the panels of a SmallMultiple: the kind of its base. */
export function smallMultipleInner(spec: Record<string, unknown> | undefined): string | undefined {
    const base = spec?.base as { kind?: unknown } | undefined;
    return typeof base?.kind === 'string' ? base.kind : undefined;
}

function asArray(value: unknown): unknown[] {
    return Array.isArray(value) ? value : [];
}

/**
 * The writes that turn an Annotation spec into another: changed or new keys
 * as a patch of `spec.<key>`, keys that are gone as removals. Unchanged keys
 * are not touched, so their comments stay.
 */
export function specChanges(before: Record<string, unknown>, after: Record<string, unknown>): { patch: Record<string, unknown>; remove: string[] } {
    const patch: Record<string, unknown> = {};
    const remove: string[] = [];
    for (const [key, value] of Object.entries(after)) {
        if (value === undefined) { continue; }
        if (JSON.stringify(before[key]) !== JSON.stringify(value)) { patch[`spec.${key}`] = value; }
    }
    for (const key of Object.keys(before)) {
        if (after[key] === undefined) { remove.push(`spec.${key}`); }
    }
    return { patch, remove };
}

/** The spec the form wrote, without empty values and without keys its mark does not read. */
export function cleanSpec(spec: Record<string, unknown>): Record<string, unknown> {
    const mark = typeof spec.mark === 'string' ? spec.mark : 'marker';
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(spec)) {
        if (value === undefined || value === null || value === '' || value === false) { continue; }
        if (!fieldApplies(key, mark)) { continue; }
        if (key === 'mark' && value === 'marker') { continue; }
        if (key === 'target' || key === 'to') {
            const t = cleanTarget(value);
            if (t) { out[key] = t; }
            continue;
        }
        out[key] = value;
    }
    return out;
}

function cleanTarget(value: unknown): Record<string, unknown> | undefined {
    if (!value || typeof value !== 'object') { return undefined; }
    const t = value as { kind?: string; name?: string; panel?: string; point?: Record<string, unknown> };
    if (!t.kind || !t.name) { return undefined; }
    const out: Record<string, unknown> = { kind: t.kind, name: t.name };
    if (t.panel && t.kind === 'SmallMultiple') { out.panel = t.panel; }
    const point: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(t.point ?? {})) {
        if (v !== undefined && v !== null && v !== '') { point[k] = v; }
    }
    if (Object.keys(point).length > 0) { out.point = point; }
    return out;
}

/**
 * The lines to delete to remove the `position`-th document (1-based) of a
 * YAML file: the document and one `---` separator next to it. Returns
 * [start, end) line numbers, or undefined when the file has no such document.
 */
export function documentLines(text: string, position: number): [number, number] | undefined {
    const lines = text.split('\n');
    const separators = lines.map((l, i) => /^---\s*(#.*)?$/.test(l) ? i : -1).filter(i => i >= 0);
    // a separator on the first content line opens the first document and does not split
    const firstContent = lines.findIndex(l => l.trim() !== '' && !l.trimStart().startsWith('#'));
    const splits = separators.filter(i => i !== firstContent);
    if (position < 1 || position > splits.length + 1) { return undefined; }
    const start = position === 1 ? 0 : splits[position - 2];
    const next = splits[position - 1];
    if (next !== undefined) {
        // the document and the separator after it
        return [position === 1 ? 0 : start + 1, next + 1];
    }
    // the last document and the separator before it
    return [position === 1 ? 0 : start, lines.length];
}
