import * as fs from 'fs';
import * as path from 'path';

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
    /** For oneOf/anyOf — expose superset of possible properties */
    isOneOf?: boolean;
    /** For $ref fields — the ref name (e.g. "datasetRef") */
    refName?: string;
}

/** Maps a kind to its spec $ref name */
interface KindSpecMapping {
    kind: string;
    specRef: string;
    /** Extra metadata properties (e.g. LayoutPage gets params) */
    metadataExtras?: Record<string, unknown>;
}

/**
 * Resolves the Bino document.schema.json and provides per-kind field definitions.
 */
export class SchemaResolver {
    private schema: Record<string, unknown> | undefined;
    private defs: Record<string, unknown> = {};
    private kindMappings: KindSpecMapping[] = [];
    private kindFieldsCache: Map<string, FieldDef[]> = new Map();
    private recursionGuard: Set<string> = new Set();

    constructor(private extensionPath: string) {}

    /** Load and parse the schema. Call once at startup. */
    load(): boolean {
        const schemaPath = path.join(this.extensionPath, 'schema', 'document.schema.json');
        try {
            const content = fs.readFileSync(schemaPath, 'utf8');
            this.schema = JSON.parse(content);
            this.defs = (this.schema as any).$defs || {};
            this.buildKindMappings();
            return true;
        } catch {
            return false;
        }
    }

    /** Get the list of all known kind values */
    getKinds(): string[] {
        return this.kindMappings.map(m => m.kind);
    }

    /** Get the kind enum from the schema */
    getKindEnum(): string[] {
        if (!this.schema) { return []; }
        const kindProp = (this.schema as any).properties?.kind;
        return kindProp?.enum || [];
    }

    /** Get field definitions for a specific kind's spec */
    getFieldsForKind(kind: string): FieldDef[] {
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
        if (!this.schema) { return []; }
        const props = (this.schema as any).properties || {};
        const required = (this.schema as any).required || [];
        return Object.keys(props).map(key => this.propertyToFieldDef(key, props[key], [key], required.includes(key)));
    }

    /** Get metadata field definitions */
    getMetadataFields(kind?: string): FieldDef[] {
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

            const specRef = thenBlock.properties?.spec?.$ref;
            if (specRef) {
                const refName = specRef.replace('#/$defs/', '');
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
        const props = (resolved as any).properties || {};
        const required: string[] = (resolved as any).required || [];
        const fields: FieldDef[] = [];

        for (const [key, propDef] of Object.entries(props)) {
            fields.push(this.propertyToFieldDef(key, propDef as Record<string, unknown>, [...parentPath, key], required.includes(key)));
        }

        this.recursionGuard.delete(defName);
        return fields;
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
