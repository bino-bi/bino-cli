import { defineConfig } from '@vscode/test-cli';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

// With BINO_TEST_BIN set, run in a temporary copy of the fixture that points
// bino.binPath at that binary, so tests that need the language server can run.
function workspaceFolder() {
    const fixture = './src/test/fixtures/project';
    const bin = process.env.BINO_TEST_BIN;
    if (!bin) {
        return fixture;
    }
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-vscode-test-'));
    fs.cpSync(fixture, dir, { recursive: true });
    fs.mkdirSync(path.join(dir, '.vscode'), { recursive: true });
    fs.writeFileSync(
        path.join(dir, '.vscode', 'settings.json'),
        JSON.stringify({ 'bino.binPath': path.resolve(bin), 'bino.daemon.enabled': false }, null, 2)
    );
    return dir;
}

export default defineConfig({
    files: 'out/test/**/*.test.js',
    workspaceFolder: workspaceFolder(),
    mocha: { ui: 'tdd', timeout: 20000 },
});
