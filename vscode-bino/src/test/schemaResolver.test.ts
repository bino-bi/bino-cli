import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import { SchemaResolver, SchemaSource, loadBackendSchema, resetBackendSchema, getKindCategory } from '../schemaResolver';
import { isScenarioListField } from '../designer/widgets/scenario';
import { buildScaffoldSnippet } from '../addElement';
import type { KindInfo } from '../indexer';

// out/test -> the extension root, which holds schema/document.schema.json.
const extensionPath = path.resolve(__dirname, '..', '..');
const PLUGIN_KIND = 'SciInterval';

/** The bundled schema with one plugin kind merged in, the way the backend serves it. */
function mergedSchemaWithPluginKind(): string {
    const schema = JSON.parse(fs.readFileSync(path.join(extensionPath, 'schema', 'document.schema.json'), 'utf8'));
    schema.properties.kind.enum.push(PLUGIN_KIND);
    schema.$defs.sciIntervalSpec = {
        type: 'object',
        properties: {
            dataset: { type: 'string' },
            level: { type: 'number', description: 'Confidence level' },
        },
        required: ['level'],
    };
    schema.allOf.push({
        if: { properties: { kind: { const: PLUGIN_KIND } } },
        then: { properties: { spec: { $ref: '#/$defs/sciIntervalSpec' } } },
    });
    return JSON.stringify(schema);
}

function stubSource(schema: string | undefined, kinds: KindInfo[] | undefined): SchemaSource {
    return { getSchema: async () => schema, getKinds: async () => kinds };
}

suite('SchemaResolver backend schema', () => {
    teardown(() => resetBackendSchema());

    test('uses backend kinds and fields, plugin kinds included', async () => {
        const resolver = new SchemaResolver(extensionPath);
        assert.ok(resolver.load());
        assert.deepStrictEqual(resolver.getFieldsForKind(PLUGIN_KIND), []);

        const kinds: KindInfo[] = [
            { name: 'Table', category: 'embeddable', embeddable: true },
            { name: PLUGIN_KIND, category: 'embeddable', embeddable: false },
        ];
        assert.ok(await loadBackendSchema(stubSource(mergedSchemaWithPluginKind(), kinds)));

        // A resolver created before the backend answered picks up the new schema.
        assert.deepStrictEqual(resolver.getFieldsForKind(PLUGIN_KIND).map(f => f.key).sort(), ['dataset', 'level']);
        assert.ok(resolver.getKindEnum().includes(PLUGIN_KIND));
        assert.strictEqual(getKindCategory(PLUGIN_KIND), 'embeddable');

        const infos = resolver.getKindInfos();
        assert.strictEqual(infos.find(k => k.name === PLUGIN_KIND)?.category, 'embeddable');
        // Kinds only the schema knows still appear.
        assert.ok(infos.some(k => k.name === 'ScreenshotArtefact'));
    });

    test('reads a plugin kind whose spec schema is inline with its own $defs', async () => {
        // bino serves plugin kinds this way, not as a $ref into the root $defs.
        const schema = JSON.parse(fs.readFileSync(path.join(extensionPath, 'schema', 'document.schema.json'), 'utf8'));
        schema.properties.kind.enum.push(PLUGIN_KIND);
        schema.allOf.push({
            if: { properties: { kind: { const: PLUGIN_KIND } } },
            then: { properties: { spec: {
                $id: 'https://bino.bi/schemas/plugins/SciInterval.json',
                $defs: { intervalType: { type: 'string', enum: ['sd', 'ci95'] } },
                type: 'object',
                properties: { dataset: { type: 'string' }, interval: { $ref: '#/$defs/intervalType' } },
                required: ['dataset'],
            } } },
        });
        // A draft-04 spec that is only a $ref into its own definitions.
        schema.properties.kind.enum.push('SciDraft4');
        schema.allOf.push({
            if: { properties: { kind: { const: 'SciDraft4' } } },
            then: { properties: { spec: {
                $schema: 'http://json-schema.org/draft-04/schema#',
                $ref: '#/definitions/spec',
                definitions: {
                    spec: { type: 'object', properties: { level: { $ref: '#/definitions/level' } } },
                    level: { type: 'number', description: 'Confidence level' },
                },
            } } },
        });
        assert.ok(await loadBackendSchema(stubSource(JSON.stringify(schema), undefined)));
        const resolver = new SchemaResolver(extensionPath);
        assert.ok(resolver.load());
        const fields = resolver.getFieldsForKind(PLUGIN_KIND);
        assert.deepStrictEqual(fields.map(f => f.key).sort(), ['dataset', 'interval']);
        assert.strictEqual(fields.find(f => f.key === 'dataset')?.required, true);
        assert.deepStrictEqual(fields.find(f => f.key === 'interval')?.enumValues, ['sd', 'ci95']);
        const level = resolver.getFieldsForKind('SciDraft4').find(f => f.key === 'level');
        assert.strictEqual(level?.type, 'number');
        assert.strictEqual(level?.description, 'Confidence level');
        assert.ok(resolver.getFieldsForKind('Table').length > 0, 'built-in kinds still resolve');
    });

    test('keeps the bundled schema when the backend serves nothing', async () => {
        assert.strictEqual(await loadBackendSchema(stubSource(undefined, undefined)), false);
        assert.strictEqual(await loadBackendSchema(stubSource('not json', [])), false);
        const resolver = new SchemaResolver(extensionPath);
        assert.ok(resolver.load());
        assert.ok(resolver.getKindEnum().includes('Table'));
        assert.ok(!resolver.getKindEnum().includes(PLUGIN_KIND));
        assert.ok(resolver.getLayoutChildKinds().includes('Tree'));
    });

    test('scenario widgets claim list fields only, not ChartBullet variances', () => {
        const resolver = new SchemaResolver(extensionPath);
        resolver.load();
        const field = (kind: string, key: string) => resolver.getFieldsForKind(kind).find(f => f.key === key);
        assert.ok(isScenarioListField(field('Table', 'scenarios')!));
        assert.ok(isScenarioListField(field('ChartStructure', 'variances')!));
        assert.ok(!isScenarioListField(field('ChartBullet', 'variances')!));
    });

    test('scaffold snippet lists required fields and given values', () => {
        const resolver = new SchemaResolver(extensionPath);
        resolver.load();
        const fields = resolver.getFieldsForKind('Tree');
        const snippet = buildScaffoldSnippet(resolver.getApiVersion()!, 'Tree', 'tree_1', fields, { dataset: 'sales' }).value;
        assert.ok(snippet.startsWith('apiVersion: bino.bi/v1alpha1\nkind: Tree\nmetadata:\n  name: ${1:tree_1}\nspec:'));
        assert.ok(snippet.includes('dataset: ${2:"sales"}'));
        for (const f of fields.filter(f => f.required)) {
            assert.ok(snippet.includes(`\n  ${f.key}: `), `missing required ${f.key}`);
        }
    });
});
