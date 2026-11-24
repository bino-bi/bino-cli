import { defineConfig } from '@vscode/test-cli';
import * as fs from 'fs';
import * as path from 'path';

const mocha = { ui: 'tdd', timeout: 20000 };
const fixtures = './src/test/fixtures';

// The e2e configurations run the extension against a real bino from
// BINO_TEST_BIN. Without it only those that need no bino run, so CI needs none.
const userData = path.resolve('.vscode-test', 'user-data-e2e');
fs.mkdirSync(path.join(userData, 'User'), { recursive: true });
const bin = process.env.BINO_TEST_BIN;
fs.writeFileSync(path.join(userData, 'User', 'settings.json'),
    JSON.stringify(bin ? { 'bino.binPath': path.resolve(bin) } : {}, null, 2));

// Stop a daemon left over from an earlier run: it would be adopted, and on
// its idle exit it would remove the port file of this run's daemon.
for (const root of ['e2e-project', 'e2e-subfolder/reports']) {
    const dir = path.join(fixtures, root, '.bino');
    try {
        const { pid } = JSON.parse(fs.readFileSync(path.join(dir, 'daemon.json'), 'utf8'));
        // Only a bino daemon: the pid may belong to another process by now.
        if (fs.readFileSync(`/proc/${pid}/cmdline`, 'utf8').split('\0').includes('daemon')) {
            process.kill(pid);
        }
    } catch { /* none, or not Linux */ }
    fs.rmSync(dir, { recursive: true, force: true });
}

const e2e = (label, files, workspace) => ({
    label,
    files,
    workspaceFolder: path.join(fixtures, workspace),
    launchArgs: [`--user-data-dir=${userData}`, '--disable-workspace-trust'],
    mocha: { ...mocha, timeout: 60000 },
});

export default defineConfig([
    { label: 'unit', files: 'out/test/*.test.js', workspaceFolder: path.join(fixtures, 'project'), mocha },
    e2e('e2e-empty', 'out/test/e2e/empty.test.js', 'e2e-empty'),
    // Only with a real bino: else the extension would start whatever bino is on PATH.
    ...(bin ? [
        e2e('e2e-project', 'out/test/e2e/project/*.test.js', 'e2e-project'),
        e2e('e2e-subfolder', 'out/test/e2e/subfolder.test.js', 'e2e-subfolder'),
    ] : []),
]);
