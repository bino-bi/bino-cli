import * as assert from 'assert';
import * as path from 'path';
import { selectProject } from '../project';

suite('current project rule', () => {
    const a = path.join(path.sep, 'ws', 'a');
    const b = path.join(path.sep, 'ws', 'b');
    const nested = path.join(b, 'sub');
    const roots = [a, b, nested];

    test('the project of the active file wins', () => {
        assert.strictEqual(selectProject(roots, path.join(b, 'report.yaml'), a), b);
    });

    test('the nearest bino.toml wins for nested projects', () => {
        assert.strictEqual(selectProject(roots, path.join(nested, 'x', 'page.yaml'), a), nested);
    });

    test('a file outside every project keeps the last project', () => {
        assert.strictEqual(selectProject(roots, path.join(path.sep, 'tmp', 'notes.md'), b), b);
        assert.strictEqual(selectProject(roots, undefined, b), b);
    });

    test('a folder with a similar prefix is not inside the project', () => {
        assert.strictEqual(selectProject(roots, path.join(path.sep, 'ws', 'ab', 'x.yaml'), b), b);
    });

    test('without an active project or a valid last one, the first project', () => {
        assert.strictEqual(selectProject(roots, undefined, undefined), a);
        assert.strictEqual(selectProject(roots, undefined, path.join(path.sep, 'gone')), a);
        assert.strictEqual(selectProject([], undefined, undefined), undefined);
    });
});
