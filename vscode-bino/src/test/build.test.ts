import * as assert from 'assert';
import * as crypto from 'crypto';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { buildArgs, buildTaskDefinitions, formatRecord, parseLogLine, recordToDiagnostic, taskName } from '../tasks';
import { assetName, checksumFor, findManagedBino, isOlderVersion, verifyChecksum } from '../binoDownload';
import { isOnPath } from '../bino';

suite('build tasks', () => {
    test('one task for all artefacts, then one per artefact', () => {
        const defs = buildTaskDefinitions(['sales', 'kpis', 'sales']);
        assert.deepStrictEqual(defs, [
            { type: 'bino', command: 'build' },
            { type: 'bino', command: 'build', artefact: 'kpis' },
            { type: 'bino', command: 'build', artefact: 'sales' },
        ]);
        assert.strictEqual(taskName(defs[0]), 'build all artefacts');
        assert.strictEqual(taskName(defs[1]), 'build kpis');
        assert.deepStrictEqual(buildArgs(defs[1]), ['build', '--log-format', 'json', '--no-color', '--artefact', 'kpis']);
    });

    test('package.json declares the bino task type', () => {
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        const defs: { type: string; required: string[]; properties: Record<string, unknown> }[] =
            ext?.packageJSON?.contributes?.taskDefinitions ?? [];
        const bino = defs.find(d => d.type === 'bino');
        assert.ok(bino, 'no bino task definition');
        assert.deepStrictEqual(bino.required, ['command']);
        assert.ok(bino.properties.artefact, 'no artefact property');
    });

    test('maps a JSON error with file and line to a Diagnostic', () => {
        const line = '{"column":10,"file":"/p/report.yaml","kind":"config","level":"error","line":22,'
            + '"msg":"spec.bogus: additional property \'bogus\' not allowed","time":"2026-11-24T21:06:39Z"}';
        const rec = parseLogLine(line);
        assert.ok(rec);
        const entry = recordToDiagnostic(rec, '/p');
        assert.ok(entry);
        assert.strictEqual(entry.file, path.resolve('/p/report.yaml'));
        assert.strictEqual(entry.diagnostic.severity, vscode.DiagnosticSeverity.Error);
        assert.strictEqual(entry.diagnostic.range.start.line, 21);
        assert.strictEqual(entry.diagnostic.range.start.character, 9);
        assert.strictEqual(entry.diagnostic.message, "spec.bogus: additional property 'bogus' not allowed");
        assert.strictEqual(formatRecord(rec), "error: /p/report.yaml:22:10: spec.bogus: additional property 'bogus' not allowed\r\n");
    });

    test('resolves a relative file and defaults the line', () => {
        const rec = parseLogLine('{"level":"error","msg":"decode failed","file":"report.yaml"}');
        assert.ok(rec);
        const entry = recordToDiagnostic(rec, '/p');
        assert.ok(entry);
        assert.strictEqual(entry.file, path.resolve('/p', 'report.yaml'));
        assert.strictEqual(entry.diagnostic.range.start.line, 0);
    });

    test('leaves text lines and records without a file alone', () => {
        assert.strictEqual(parseLogLine('→ Rendering r...'), undefined);
        assert.strictEqual(parseLogLine('{not json'), undefined);
        const info = parseLogLine('{"level":"info","msg":"Generated r","artefact":"r","path":"/p/dist/r.pdf"}');
        assert.ok(info);
        assert.strictEqual(info.path, '/p/dist/r.pdf');
        assert.strictEqual(recordToDiagnostic(info, '/p'), undefined);
        const noFile = parseLogLine('{"level":"error","msg":"boom"}');
        assert.ok(noFile);
        assert.strictEqual(recordToDiagnostic(noFile, '/p'), undefined);
    });
});

suite('bino download', () => {
    test('picks the release asset per platform and arch', () => {
        assert.strictEqual(assetName('linux', 'x64'), 'bino-cli_Linux_x86_64.tar.gz');
        assert.strictEqual(assetName('linux', 'arm64'), 'bino-cli_Linux_arm64.tar.gz');
        assert.strictEqual(assetName('darwin', 'x64'), 'bino-cli_Darwin_x86_64.tar.gz');
        assert.strictEqual(assetName('darwin', 'arm64'), 'bino-cli_Darwin_arm64.tar.gz');
        assert.strictEqual(assetName('win32', 'x64'), 'bino-cli_Windows_x86_64.zip');
        assert.strictEqual(assetName('win32', 'arm64'), 'bino-cli_Windows_x86_64.zip');
        assert.strictEqual(assetName('linux', 'ia32'), undefined);
        assert.strictEqual(assetName('freebsd', 'x64'), undefined);
    });

    test('verifies the sha256 from checksums.txt', () => {
        const data = Buffer.from('bino binary');
        const hash = crypto.createHash('sha256').update(data).digest('hex');
        const checksums = [
            `${'0'.repeat(64)}  bino-cli_Darwin_arm64.tar.gz`,
            `${hash}  bino-cli_Linux_x86_64.tar.gz`,
            '',
        ].join('\n');
        assert.strictEqual(checksumFor(checksums, 'bino-cli_Linux_x86_64.tar.gz'), hash);
        assert.strictEqual(checksumFor(checksums, 'bino-cli_Linux_x86_64'), undefined);
        assert.strictEqual(verifyChecksum(data, checksums, 'bino-cli_Linux_x86_64.tar.gz'), true);
        assert.strictEqual(verifyChecksum(Buffer.from('tampered'), checksums, 'bino-cli_Linux_x86_64.tar.gz'), false);
        assert.strictEqual(verifyChecksum(data, checksums, 'bino-cli_Windows_x86_64.zip'), false);
    });

    test('compares versions', () => {
        assert.strictEqual(isOlderVersion('0.115.2', '0.116.0'), true);
        assert.strictEqual(isOlderVersion('v0.116.0', '0.116.0'), false);
        assert.strictEqual(isOlderVersion('1.0.0', '0.116.0'), false);
        assert.strictEqual(isOlderVersion('0.1.0-dev', '0.116.0'), false);
        assert.strictEqual(isOlderVersion('unknown', '0.116.0'), false);
    });

    test('finds the newest downloaded bino', () => {
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        try {
            for (const v of ['0.9.0', '0.10.0', '0.11.0']) {
                fs.mkdirSync(path.join(dir, v));
            }
            fs.writeFileSync(path.join(dir, '0.9.0', 'bino'), '');
            fs.writeFileSync(path.join(dir, '0.10.0', 'bino'), '');
            // 0.11.0 has no binary (an aborted download).
            assert.strictEqual(findManagedBino(dir, 'linux'), path.join(dir, '0.10.0', 'bino'));
            assert.strictEqual(findManagedBino(path.join(dir, 'missing'), 'linux'), undefined);
        } finally {
            fs.rmSync(dir, { recursive: true, force: true });
        }
    });

    test('looks bino up on PATH', () => {
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-path-'));
        try {
            fs.writeFileSync(path.join(dir, 'bino'), '');
            assert.strictEqual(isOnPath('bino', { PATH: dir }, 'linux'), true);
            assert.strictEqual(isOnPath('bino', { PATH: '' }, 'linux'), false);
        } finally {
            fs.rmSync(dir, { recursive: true, force: true });
        }
    });
});

suite('build task with bino', () => {
    test('puts a build error with file and line into Problems', async function () {
        const bin = process.env.BINO_TEST_BIN;
        if (!bin) {
            this.skip();
        }
        this.timeout(60000);
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        await ext?.activate();
        const root = vscode.workspace.workspaceFolders![0].uri.fsPath;
        const broken = path.join(root, 'zz-build-broken.yaml');
        fs.writeFileSync(broken, 'apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: broken\nspec:\n  value: Hi\n  bogus: 1\n');
        const config = vscode.workspace.getConfiguration('bino');
        await config.update('binPath', bin, vscode.ConfigurationTarget.Global);
        try {
            const tasks = await vscode.tasks.fetchTasks({ type: 'bino' });
            const all = tasks.find(t => !t.definition.artefact);
            assert.ok(all, 'no "build all artefacts" task');
            const ended = new Promise<void>(resolve => {
                const sub = vscode.tasks.onDidEndTask(e => {
                    if (e.execution.task.definition.type === 'bino') {
                        sub.dispose();
                        resolve();
                    }
                });
            });
            await vscode.tasks.executeTask(all);
            await ended;
            const diags = vscode.languages.getDiagnostics(vscode.Uri.file(broken)).filter(d => d.source === 'bino build');
            assert.strictEqual(diags.length, 1);
            assert.strictEqual(diags[0].range.start.line, 6);
            assert.match(diags[0].message, /bogus/);
        } finally {
            await config.update('binPath', undefined, vscode.ConfigurationTarget.Global);
            fs.rmSync(broken, { force: true });
            fs.rmSync(path.join(root, 'dist'), { recursive: true, force: true });
            fs.rmSync(path.join(root, '.bino'), { recursive: true, force: true });
        }
    });
});
