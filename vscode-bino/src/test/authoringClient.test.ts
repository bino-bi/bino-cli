import * as assert from 'assert';
import * as path from 'path';
import { AuthoringClient } from '../authoringClient';
import { WorkspaceIndexer } from '../indexer';

suite('AuthoringClient', () => {
    test('reorder sends op reorder with path, from and to to the edit helper', async () => {
        const payloads: Record<string, unknown>[] = [];
        const indexer = {
            editManifest: async (payload: Record<string, unknown>) => {
                payloads.push(payload);
                return { ok: true, diagnostics: [], version: 'test' };
            },
        } as unknown as WorkspaceIndexer;
        const file = path.resolve('not-open', 'pages.yaml');

        const result = await new AuthoringClient(indexer).reorder({ file, position: 2, path: 'spec.children', from: 3, to: 0 });

        assert.deepStrictEqual(result, { ok: true, applied: 'write' });
        assert.deepStrictEqual(payloads, [{ file, position: 2, op: 'reorder', path: 'spec.children', from: 3, to: 0, mode: 'write' }]);
    });

    test('reorder returns the helper error and writes nothing', async () => {
        const indexer = {
            editManifest: async () => ({ ok: false, diagnostics: [], error: 'reorder spec.children: index 9 out of range', version: 'test' }),
        } as unknown as WorkspaceIndexer;

        const result = await new AuthoringClient(indexer).reorder({ file: path.resolve('x.yaml'), path: 'spec.children', from: 9, to: 0 });

        assert.deepStrictEqual(result, { ok: false, diagnostics: [], error: 'reorder spec.children: index 9 out of range' });
    });
});
