import * as vscode from 'vscode';
import * as path from 'path';
import * as readline from 'readline';
import { spawnBino } from './bino';
import { WorkspaceIndexer } from './indexer';

export const BINO_TASK_TYPE = 'bino' as const;

/** The kinds `bino build` turns into PDFs. */
export const BUILD_KINDS = ['ReportArtefact', 'DocumentArtefact'];

export interface BinoTaskDefinition extends vscode.TaskDefinition {
    type: 'bino';
    command: 'build';
    /** metadata.name of one artefact; all artefacts when unset. */
    artefact?: string;
}

/** One line of `bino build --log-format json`. */
export interface BuildLogRecord {
    level: string;
    msg: string;
    file?: string;
    line?: number;
    column?: number;
    artefact?: string;
    path?: string;
}

export interface BuildResult {
    exitCode: number;
    /** The PDFs the build reported. */
    pdfs: string[];
    /** Problems published to the bino-build collection. */
    problems: number;
}

/** One task for all artefacts, then one per artefact. */
export function buildTaskDefinitions(artefacts: string[]): BinoTaskDefinition[] {
    const names = [...new Set(artefacts)].sort();
    return [
        { type: BINO_TASK_TYPE, command: 'build' },
        ...names.map(artefact => ({ type: BINO_TASK_TYPE, command: 'build' as const, artefact })),
    ];
}

export function taskName(def: BinoTaskDefinition): string {
    return def.artefact ? `build ${def.artefact}` : 'build all artefacts';
}

export function buildArgs(def: BinoTaskDefinition): string[] {
    const args = ['build', '--log-format', 'json', '--no-color'];
    if (def.artefact) {
        args.push('--artefact', def.artefact);
    }
    return args;
}

/** Parse a JSON log line; any other line (the text summary, an old bino) is undefined. */
export function parseLogLine(line: string): BuildLogRecord | undefined {
    const text = line.trim();
    if (!text.startsWith('{')) {
        return undefined;
    }
    try {
        const rec = JSON.parse(text);
        if (rec && typeof rec.level === 'string' && typeof rec.msg === 'string') {
            return rec as BuildLogRecord;
        }
    } catch {
        // not JSON after all
    }
    return undefined;
}

/** The record as a terminal line, CRLF-terminated. */
export function formatRecord(rec: BuildLogRecord): string {
    const where = rec.file ? `${rec.file}${rec.line ? `:${rec.line}${rec.column ? `:${rec.column}` : ''}` : ''}: ` : '';
    const label = rec.level === 'error' ? 'error: ' : rec.level === 'warn' ? 'warning: ' : '';
    return `${label}${where}${rec.msg}`.replace(/\r?\n/g, '\r\n') + '\r\n';
}

/** A Problems entry for a record that names a file, resolved against cwd. */
export function recordToDiagnostic(rec: BuildLogRecord, cwd: string): { file: string; diagnostic: vscode.Diagnostic } | undefined {
    if (!rec.file || (rec.level !== 'error' && rec.level !== 'warn')) {
        return undefined;
    }
    const line = Math.max((rec.line ?? 1) - 1, 0);
    const column = Math.max((rec.column ?? 1) - 1, 0);
    const diagnostic = new vscode.Diagnostic(
        new vscode.Range(line, column, line, column),
        rec.msg,
        rec.level === 'error' ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning
    );
    diagnostic.source = 'bino build';
    return { file: path.resolve(cwd, rec.file), diagnostic };
}

/** Runs one `bino build` in the task terminal and feeds the Problems panel. */
class BuildTerminal implements vscode.Pseudoterminal {
    private readonly writeEmitter = new vscode.EventEmitter<string>();
    private readonly closeEmitter = new vscode.EventEmitter<number>();
    readonly onDidWrite = this.writeEmitter.event;
    readonly onDidClose = this.closeEmitter.event;
    private child: ReturnType<typeof spawnBino> | undefined;
    private progress: vscode.Progress<{ message?: string }> | undefined;
    private endProgress: (() => void) | undefined;

    constructor(
        private readonly def: BinoTaskDefinition,
        private readonly cwd: string,
        private readonly diagnostics: vscode.DiagnosticCollection,
        private readonly onFinished: (result: BuildResult) => void
    ) {}

    open(): void {
        this.diagnostics.clear();
        const problems = new Map<string, vscode.Diagnostic[]>();
        const pdfs: string[] = [];
        const args = buildArgs(this.def);
        this.writeEmitter.fire(`> bino ${args.join(' ')}\r\n`);
        void vscode.window.withProgress(
            { location: vscode.ProgressLocation.Notification, title: `Bino: ${taskName(this.def)}`, cancellable: true },
            (progress, token) => new Promise<void>(resolve => {
                this.progress = progress;
                this.endProgress = resolve;
                token.onCancellationRequested(() => this.close());
            })
        );

        const onLine = (line: string) => {
            const rec = parseLogLine(line);
            if (!rec) {
                this.writeEmitter.fire(line + '\r\n');
                if (line.trim()) {
                    this.progress?.report({ message: line.trim() });
                }
                return;
            }
            this.writeEmitter.fire(formatRecord(rec));
            if (rec.level === 'info') {
                this.progress?.report({ message: rec.msg });
            }
            if (rec.path && rec.artefact) {
                pdfs.push(rec.path);
            }
            const entry = recordToDiagnostic(rec, this.cwd);
            if (entry) {
                problems.set(entry.file, [...(problems.get(entry.file) ?? []), entry.diagnostic]);
                this.diagnostics.set(vscode.Uri.file(entry.file), problems.get(entry.file));
            }
        };

        const finish = (exitCode: number) => {
            if (!this.child) {
                return;
            }
            this.child = undefined;
            this.endProgress?.();
            this.writeEmitter.fire(exitCode === 0 ? '\r\nBuild complete.\r\n' : `\r\nBuild failed (exit code ${exitCode}).\r\n`);
            let count = 0;
            problems.forEach(list => { count += list.length; });
            this.onFinished({ exitCode, pdfs, problems: count });
            this.closeEmitter.fire(exitCode);
        };

        const child = spawnBino(args, this.cwd);
        this.child = child;
        readline.createInterface({ input: child.stdout }).on('line', onLine);
        readline.createInterface({ input: child.stderr }).on('line', onLine);
        child.on('error', err => {
            this.writeEmitter.fire(`Could not run bino: ${err.message}\r\n`);
            finish(-1);
        });
        child.on('close', code => finish(code ?? -1));
    }

    close(): void {
        this.child?.kill();
    }
}

/** Provides the `bino` build tasks and shows what a build produced. */
export class BinoTaskProvider implements vscode.TaskProvider, vscode.Disposable {
    private readonly diagnostics = vscode.languages.createDiagnosticCollection('bino-build');

    constructor(
        private readonly indexer: WorkspaceIndexer,
        private readonly outputChannel: vscode.LogOutputChannel
    ) {}

    provideTasks(): vscode.Task[] {
        const artefacts = this.indexer.getDocuments(BUILD_KINDS).map(d => d.name);
        return buildTaskDefinitions(artefacts).flatMap(def => this.createTask(def) ?? []);
    }

    resolveTask(task: vscode.Task): vscode.Task | undefined {
        const def = task.definition as BinoTaskDefinition;
        return def.type === BINO_TASK_TYPE && def.command === 'build' ? this.createTask(def) : undefined;
    }

    /** Start a build as a task. */
    async runBuild(artefact?: string): Promise<void> {
        const task = this.createTask({ type: BINO_TASK_TYPE, command: 'build', artefact });
        if (task) {
            await vscode.tasks.executeTask(task);
        }
    }

    private createTask(def: BinoTaskDefinition): vscode.Task | undefined {
        if (!vscode.workspace.isTrusted) {
            return undefined;
        }
        const cwd = this.indexer.getWorkspaceProjectRoot();
        if (!cwd) {
            return undefined;
        }
        const folder = vscode.workspace.getWorkspaceFolder(vscode.Uri.file(cwd));
        const task = new vscode.Task(
            def,
            folder ?? vscode.TaskScope.Workspace,
            taskName(def),
            'bino',
            new vscode.CustomExecution(async () => new BuildTerminal(def, cwd, this.diagnostics, result => this.reportResult(def, result)))
        );
        task.group = vscode.TaskGroup.Build;
        task.presentationOptions = { reveal: vscode.TaskRevealKind.Silent };
        return task;
    }

    private async reportResult(def: BinoTaskDefinition, result: BuildResult): Promise<void> {
        this.outputChannel.info(`[Build] ${taskName(def)} finished with exit code ${result.exitCode}`);
        if (result.exitCode !== 0) {
            const actions = result.problems > 0 ? ['Show Problems'] : [];
            const choice = await vscode.window.showErrorMessage('Bino build failed. See the task terminal for details.', ...actions);
            if (choice === 'Show Problems') {
                await vscode.commands.executeCommand('workbench.actions.view.problems');
            }
            return;
        }
        if (result.pdfs.length === 0) {
            void vscode.window.showInformationMessage('Bino build complete.');
            return;
        }
        const choice = await vscode.window.showInformationMessage('Bino build complete.', 'Open PDF');
        if (choice !== 'Open PDF') {
            return;
        }
        const pdf = result.pdfs.length === 1
            ? result.pdfs[0]
            : (await vscode.window.showQuickPick(result.pdfs.map(p => ({ label: path.basename(p), description: p })), { title: 'Open PDF' }))?.description;
        if (pdf) {
            await openPdf(pdf);
        }
    }

    dispose(): void {
        this.diagnostics.dispose();
    }
}

/** Open a PDF in the system viewer; in a remote window the file is not on this machine, so open it in VS Code. */
async function openPdf(file: string): Promise<void> {
    const uri = vscode.Uri.file(file);
    if (vscode.env.remoteName) {
        await vscode.commands.executeCommand('vscode.open', uri);
        return;
    }
    await vscode.env.openExternal(uri);
}
