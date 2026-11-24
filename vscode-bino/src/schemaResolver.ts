import * as fs from 'fs';
import * as path from 'path';
import type { KindInfo } from './indexer';

/** Describes a single field in a kind's spec */
export interface FieldDef {
    key: string;
    path: string[];
    type: string;
    enumValues?: string[];
    description?: string;
    required: boolean;
    defaultValue?: unknown;
    children?: FieldDef[];
    /** For oneOf/anyOf - expose superset of possible properties */
    isOneOf?: boolean;
    /** For $ref fields - the ref name (e.g. "datasetRef") */
    refName?: string;
}

/** Maps a kind to its spec $ref name */
interface KindSpecMapping {
    kind: string;
    specRef: string;
    /** Extra metadata properties (e.g. LayoutPage gets params) */
    metadataExtras?: Record<string, unknown>;
}

/** Where the merged (built-in + plugin) schema and kind list come from. */
export interface SchemaSource {
    /** The merged JSON schema text, or undefined when the backend can't serve it. */
    getSchema(): Promise<string | undefined>;
    /** Every manifest kind with its capability category, or undefined. */
    getKinds(): Promise<KindInfo[] | undefined>;
}

// Shared by every SchemaResolver instance, so resolvers created before the
// backend answered pick up plugin kinds on their next read.
let backendSchema: Record<string, unknown> | undefined;
let backendKinds: KindInfo[] = [];
let backendGeneration = 0;

/**
 * Fetch the merged schema and kind list from the backend. Returns false and
 * keeps the bundled schema when the backend serves neither (an older bino, or
 * no bino at all).
 */
export async function loadBackendSchema(source: SchemaSource): Promise<boolean> {
    const [raw, kinds] = await Promise.all([
        source.getSchema().catch(() => undefined),
        source.getKinds().catch(() => undefined),
    ]);
    let parsed: Record<string, unknown> | undefined;
    try {
        parsed = raw ? JSON.parse(raw) : undefined;
    } catch {
        parsed = undefined;
    }
    if (parsed && typeof (parsed as any).$defs !== 'object') {
        parsed = undefined;
    }
    const hasKinds = Array.isArray(kinds) && kinds.length > 0;
    if (!parsed && !hasKinds) {
        return false;
    }
    if (parsed) {
        backendSchema = parsed;
    }
    if (hasKinds) {
        backendKinds = kinds;
    }
    backendGeneration++;
    return true;
}

/** Forget the backend schema (tests). */
export function resetBackendSchema(): void {
    backendSchema = undefined;
    backendKinds = [];
    backendGeneration++;
}

/** The backend's capability category for a kind, if it served one. */
export function getKindCategory(kind: string): string | undefined {
    return backendKinds.find(k => k.name === kind)?.category;
}

/**
 * Resolves the Bino document schema and provides per-kind field definitions.
 * Uses the backend's merged schema (with plugin kinds) once loadBackendSchema
 * succeeded, else the bundled schema/document.schema.json.
 */
export class SchemaResolver {
    private schema: Record<string, unknown> | undefined;
    private defs: Record<string, unknown> = {};
    private kindMappings: KindSpecMapping[] = [];
    private kindFieldsCache: Map<string, FieldDef[]> = new Map();
    private recursionGuard: Set<string> = new Set();
    private loadedGeneration = -1;

    constructor(private extensionPath: string) { }

    /** Load and parse the schema. Call once at startup. */
    load(): boolean {
        this.loadedGeneration = backendGeneration;
        if (backendSchema) {
            this.apply(backendSchema);
            return true;
        }
        const schemaPath = path.join(this.extensionPath, 'schema', 'document.schema.json');
        try {
            const content = fs.readFileSync(schemaPath, 'utf8');
            this.apply(JSON.parse(content));
            return true;
        } catch (err) {
            console.warn(`bino: failed to load ${schemaPath}:`, err);
            return false;
        }
    }

    private apply(schema: Record<string, unknown>): void {
        this.schema = schema;
        // A copy: inline plugin specs are added to it.
        this.defs = { ...((schema as any).$defs || {}) };
        this.kindMappings = [];
        this.kindFieldsCache.clear();
        this.buildKindMappings();
    }

    /** Reload when the backend schema changed since the last load. */
    private sync(): void {
        if (this.loadedGeneration !== backendGeneration && backendSchema) {
            this.load();
        }
    }

    /**
     * Every manifest kind with a category: the backend's kind list, then
     * `fallback` (e.g. an older kind source), then kinds only the schema knows.
     * Those get "embeddable" when they are layout children, else "config".
     */
    getKindInfos(fallback: KindInfo[] = []): KindInfo[] {
        this.sync();
        const out: KindInfo[] = [];
        const known = new Set<string>();
        for (const k of [...backendKinds, ...fallback]) {
            if (!known.has(k.name)) {
                known.add(k.name);
                out.push(k);
            }
        }
        const children = new Set(this.getLayoutChildKinds());
        for (const name of this.getKindEnum()) {
            if (!known.has(name)) {
                out.push({ name, category: children.has(name) ? 'embeddable' : 'config', embeddable: false });
            }
        }
        return out;
    }

    /** The document apiVersion the schema requires. */
    getApiVersion(): string | undefined {
        this.sync();
        return (this.schema as any)?.properties?.apiVersion?.const;
    }

    /** Get the kinds a LayoutPage/LayoutCard child may reference (layoutChild kind enum) */
    getLayoutChildKinds(): string[] {
        this.sync();
        const layoutChild = this.defs['layoutChild'] as Record<string, unknown> | undefined;
        const kindProp = (layoutChild as any)?.properties?.kind;
        return kindProp?.enum || [];
    }

    /** Get the list of all known kind values */
    getKinds(): string[] {
        this.sync();
        return this.kindMappings.map(m => m.kind);
    }

    /** Get the kind enum from the schema */
    getKindEnum(): string[] {
        this.sync();
        if (!this.schema) { return []; }
        const kindProp = (this.schema as any).properties?.kind;
        return kindProp?.enum || [];
    }

    /** Get field definitions for a specific kind's spec */
    getFieldsForKind(kind: string): FieldDef[] {
        this.sync();
        const cached = this.kindFieldsCache.get(kind);
        if (cached) { return cached; }

        const mapping = this.kindMappings.find(m => m.kind === kind);
        if (!mapping) { return []; }

        const specDef = this.defs[mapping.specRef];
        if (!specDef) { return []; }

        this.recursionGuard.clear();
        const fields = this.resolveObjectFields(specDef as Record<string, unknown>, [], mapping.specRef);
        this.kindFieldsCache.set(kind, fields);
        return fields;
    }

    /** Get the full document-level fields (apiVersion, kind, metadata, spec) */
    getDocumentFields(): FieldDef[] {
        this.sync();
        if (!this.schema) { return []; }
        const props = (this.schema as any).properties || {};
        const required = (this.schema as any).required || [];
        return Object.keys(props).map(key => this.propertyToFieldDef(key, props[key], [key], required.includes(key)));
    }

    /** Get metadata field definitions */
    getMetadataFields(kind?: string): FieldDef[] {
        this.sync();
        const metaDef = this.defs['metadata'] as Record<string, unknown> | undefined;
        if (!metaDef) { return []; }

        const fields = this.resolveObjectFields(metaDef, ['metadata'], 'metadata');

        // LayoutPage gets extra params field
        if (kind === 'LayoutPage') {
            const mapping = this.kindMappings.find(m => m.kind === 'LayoutPage');
            if (mapping?.metadataExtras) {
                for (const [key, val] of Object.entries(mapping.metadataExtras)) {
                    const resolved = this.resolveRef(val as Record<string, unknown>);
                    fields.push(this.propertyToFieldDef(key, resolved, ['metadata', key], false));
                }
            }
        }

        return fields;
    }

    /** Build the kind → specRef mapping from allOf array */
    private buildKindMappings(): void {
        if (!this.schema) { return; }
        const allOf = (this.schema as any).allOf;
        if (!Array.isArray(allOf)) { return; }

        for (const entry of allOf) {
            const ifBlock = entry.if;
            const thenBlock = entry.then;
            if (!ifBlock || !thenBlock) { continue; }

            const kindConst = ifBlock.properties?.kind?.const;
            if (!kindConst) { continue; }

            const spec = thenBlock.properties?.spec;
            // A built-in kind's spec is a bare $ref into the root $defs. A plugin
            // kind's spec is an embedded schema of its own.
            const bareRef = spec && Object.keys(spec).length === 1 && typeof spec.$ref === 'string';
            const refName = bareRef ? spec.$ref.replace('#/$defs/', '')
                : spec && typeof spec === 'object' ? this.registerInlineSpec(kindConst, spec) : undefined;
            if (refName) {
                const mapping: KindSpecMapping = { kind: kindConst, specRef: refName };

                // Check for metadata extras (LayoutPage params)
                const metaProps = thenBlock.properties?.metadata?.properties;
                if (metaProps) {
                    mapping.metadataExtras = {};
                    for (const [key, val] of Object.entries(metaProps)) {
                        if (key !== 'name') { // name is already in base metadata
                            mapping.metadataExtras[key] = val;
                        }
                    }
                }

                // Only add if not a duplicate (DataSource appears twice for sqlIdentifier constraint)
                if (!this.kindMappings.find(m => m.kind === kindConst && m.specRef === refName)) {
                    this.kindMappings.push(mapping);
                }
            }
        }
    }

    /**
     * Add an embedded spec schema to the defs under a name of its own. Its local
     * `#/$defs/x` (or draft-04 `#/definitions/x`) refs point at its own defs, so
     * they are renamed with it.
     */
    private registerInlineSpec(kind: string, spec: Record<string, unknown>): string {
        const name = `${kind}:spec`;
        const local = /^#\/(\$defs|definitions)\//;
        const rewrite = (node: unknown): unknown => {
            if (Array.isArray(node)) { return node.map(rewrite); }
            if (!node || typeof node !== 'object') { return node; }
            const out: Record<string, unknown> = {};
            for (const [key, value] of Object.entries(node)) {
                out[key] = key === '$ref' && typeof value === 'string' && local.test(value)
                    ? value.replace(local, `#/$defs/${name}/`)
                    : rewrite(value);
            }
            return out;
        };
        const { $defs, definitions, ...rest } = rewrite(spec) as Record<string, unknown>;
        for (const [key, def] of Object.entries({ ...(definitions as object), ...($defs as object) })) {
            this.defs[`${name}/${key}`] = def;
        }
        this.defs[name] = rest;
        return name;
    }

    /** Resolve object properties into FieldDef array */
    private resolveObjectFields(
        def: Record<string, unknown>,
        parentPath: string[],
        defName: string
    ): FieldDef[] {
        // Guard against infinite recursion (e.g. layoutChild -> layoutCardSpec -> layoutChild)
        if (this.recursionGuard.has(defName)) {
            return [];
        }
        this.recursionGuard.add(defName);

        const resolved = this.resolveRef(def);
        const { properties: props, required } = this.collectObjectShape(resolved);
        const fields: FieldDef[] = [];

        for (const [key, propDef] of Object.entries(props)) {
            fields.push(this.propertyToFieldDef(key, propDef as Record<string, unknown>, [...parentPath, key], required.has(key)));
        }

        this.recursionGuard.delete(defName);
        return fields;
    }

    /**
     * Collect an object def's effective properties and required keys, flattening
     * `allOf` composition (and the `$ref`s inside it). Many specs are defined as
     * `{ allOf: [{ $ref: fooSpecBase }], required: [...] }`, so the properties
     * live in a referenced base while `required` sits at the top level; without
     * flattening, getFieldsForKind would return no fields for those kinds.
     */
    private collectObjectShape(def: Record<string, unknown>): { properties: Record<string, unknown>; required: Set<string> } {
        const properties: Record<string, unknown> = { ...((def as any).properties || {}) };
        const required = new Set<string>((def as any).required || []);

        const allOf = (def as any).allOf;
        if (Array.isArray(allOf)) {
            for (const member of allOf) {
                const resolvedMember = this.resolveRef(member as Record<string, unknown>);
                const sub = this.collectObjectShape(resolvedMember);
                for (const [k, v] of Object.entries(sub.properties)) {
                    if (!(k in properties)) {
                        properties[k] = v;
                    }
                }
                for (const k of sub.required) {
                    required.add(k);
                }
            }
        }

        return { properties, required };
    }

    /** Convert a JSON Schema property definition to a FieldDef */
    private propertyToFieldDef(
        key: string,
        prop: Record<string, unknown>,
        fieldPath: string[],
        isRequired: boolean
    ): FieldDef {
        const resolved = this.resolveRef(prop);
        const field: FieldDef = {
            key,
            path: fieldPath,
            type: this.inferType(resolved),
            description: (resolved as any).description,
            required: isRequired,
            defaultValue: (resolved as any).default,
        };

        // Enum values
        if ((resolved as any).enum) {
            field.enumValues = (resolved as any).enum;
        }

        // For objects, resolve children
        if (field.type === 'object' && (resolved as any).properties) {
            const refName = this.getRefName(prop) || key;
            field.children = this.resolveObjectFields(resolved, fieldPath, refName);
        }

        // For arrays with object items, resolve item fields
        if (field.type === 'array') {
            const items = (resolved as any).items;
            if (items) {
                const resolvedItems = this.resolveRef(items);
                if ((resolvedItems as any).type === 'object' && (resolvedItems as any).properties) {
                    const refName = this.getRefName(items) || `${key}Item`;
                    field.children = this.resolveObjectFields(resolvedItems, [...fieldPath, '[]'], refName);
                }
            }
        }

        // Handle oneOf by merging possible enum values
        if ((resolved as any).oneOf) {
            field.isOneOf = true;
            const oneOf = (resolved as any).oneOf as Record<string, unknown>[];
            const enums: string[] = [];
            for (const variant of oneOf) {
                const resolvedVariant = this.resolveRef(variant);
                if ((resolvedVariant as any).enum) {
                    enums.push(...(resolvedVariant as any).enum);
                }
                // If one variant is a string type, note it
                if ((resolvedVariant as any).type === 'string' && !field.enumValues) {
                    field.type = 'string';
                }
            }
            if (enums.length > 0 && !field.enumValues) {
                field.enumValues = enums;
            }
        }

        // Track $ref name for completion mapping
        const refName = this.getRefName(prop);
        if (refName) {
            field.refName = refName;
        }

        return field;
    }

    /** Infer the user-facing type string from a JSON Schema definition */
    private inferType(def: Record<string, unknown>): string {
        if (typeof (def as any).type === 'string') {
            return (def as any).type;
        }
        if (Array.isArray((def as any).type)) {
            // e.g. ["string", "number"]
            return (def as any).type[0];
        }
        if ((def as any).oneOf || (def as any).anyOf) {
            // Try to infer from first variant
            const variants = ((def as any).oneOf || (def as any).anyOf) as Record<string, unknown>[];
            for (const v of variants) {
                const resolved = this.resolveRef(v);
                if ((resolved as any).type) {
                    return typeof (resolved as any).type === 'string' ? (resolved as any).type : (resolved as any).type[0];
                }
            }
            return 'any';
        }
        if ((def as any).const !== undefined) {
            return typeof (def as any).const;
        }
        if ((def as any).properties) {
            return 'object';
        }
        return 'any';
    }

    /** Resolve a $ref to its target definition */
    private resolveRef(def: Record<string, unknown>): Record<string, unknown> {
        if (!def) { return {}; }
        const ref = (def as any).$ref;
        if (typeof ref === 'string' && ref.startsWith('#/$defs/')) {
            const refName = ref.replace('#/$defs/', '');
            const target = this.defs[refName] as Record<string, unknown>;
            if (target) {
                // Merge any sibling properties (e.g. description overrides)
                const { $ref: _, ...rest } = def as any;
                return { ...target, ...rest };
            }
        }
        return def;
    }

    /** Extract the $ref name from a property definition */
    private getRefName(def: Record<string, unknown>): string | undefined {
        const ref = (def as any)?.$ref;
        if (typeof ref === 'string' && ref.startsWith('#/$defs/')) {
            return ref.replace('#/$defs/', '');
        }
        return undefined;
    }
}
