import * as vscode from 'vscode';
import * as path from 'path';
import { DaemonClient, RegistryPackage, RegistrySearchItem } from './daemonClient';
import { DependenciesTreeProvider, PackageItem } from './dependenciesTree';

export interface RegistryCommandDeps {
    daemonClient: () => DaemonClient | undefined;
    projectRoot: () => string | undefined;
    runInTerminal: (args: string) => void;
    depsTree: DependenciesTreeProvider;
}

/**
 * Registers the package-registry commands. Read operations go through the
 * daemon's /registry routes; mutations (add/update/remove/install/login) run
 * the CLI in the integrated terminal — auth prompts and progress output live
 * there, and the daemon's registry-changed SSE event refreshes the tree.
 */
export function registerRegistryCommands(context: vscode.ExtensionContext, deps: RegistryCommandDeps): void {
    const { runInTerminal, depsTree } = deps;

    context.subscriptions.push(
        vscode.commands.registerCommand('bino.registrySearch', () => searchAndAdd(deps)),
        vscode.commands.registerCommand('bino.registryAddDependency', () => searchAndAdd(deps)),
        vscode.commands.registerCommand('bino.registryRefresh', () => depsTree.refresh()),
        vscode.commands.registerCommand('bino.registryInstall', () => runInTerminal('registry install')),
        vscode.commands.registerCommand('bino.registryUpdateAll', () => runInTerminal('registry update')),
        vscode.commands.registerCommand('bino.registryLogin', () => runInTerminal('registry login')),
        vscode.commands.registerCommand('bino.registryLogout', () => runInTerminal('registry logout')),
        vscode.commands.registerCommand('bino.registryPublish', () => publishPackage(deps)),

        vscode.commands.registerCommand('bino.registryUpdatePackage', (item?: PackageItem) => {
            if (item?.pkg) {
                runInTerminal(`registry update ${item.pkg.name}`);
            }
        }),
        vscode.commands.registerCommand('bino.registryRemovePackage', async (item?: PackageItem) => {
            if (!item?.pkg) {
                return;
            }
            const answer = await vscode.window.showWarningMessage(
                `Remove ${item.pkg.name} from the project's dependencies?`,
                { modal: true },
                'Remove'
            );
            if (answer === 'Remove') {
                runInTerminal(`registry remove ${item.pkg.name}`);
            }
        }),
        vscode.commands.registerCommand('bino.registryOpenPackageFile', async (item?: PackageItem, file?: string) => {
            const root = deps.projectRoot();
            const target = file ?? item?.pkg?.path;
            if (!target || !root) {
                return;
            }
            const uri = vscode.Uri.file(path.join(root, target));
            // A multi-file package's primary document is still a document, but
            // a lock written by an older bino may name something else, so fall
            // back to revealing rather than failing to open.
            try {
                await vscode.window.showTextDocument(uri, { preview: true });
            } catch {
                await vscode.commands.executeCommand('revealInExplorer', uri);
            }
        }),
        vscode.commands.registerCommand('bino.registryCopyRef', async (item?: PackageItem) => {
            if (!item?.pkg) {
                return;
            }
            await vscode.env.clipboard.writeText(refSnippetText(item.pkg));
            vscode.window.showInformationMessage(`Copied a ${item.pkg.name} reference to the clipboard.`);
        }),
        vscode.commands.registerCommand('bino.registryInsertRef', (item?: PackageItem) => insertComponentRef(deps, item)),
    );
}

/**
 * Publishes the open project. `bino publish` needs a --bump (a version is
 * minted server-side and cannot be taken back), so the bump is asked for here
 * rather than letting the command fail on a missing flag; everything after
 * that is the terminal shell-out the other mutations use.
 */
async function publishPackage(deps: RegistryCommandDeps): Promise<void> {
    const choice = await vscode.window.showQuickPick(
        [
            { label: 'Dry run', description: 'Validate against the registry without publishing', args: '--dry-run' },
            { label: 'patch', description: 'Bug fixes — 1.2.3 becomes 1.2.4', args: '--bump patch' },
            { label: 'minor', description: 'New definitions, still compatible — 1.2.3 becomes 1.3.0', args: '--bump minor' },
            { label: 'major', description: 'Breaking changes — 1.2.3 becomes 2.0.0', args: '--bump major' },
        ],
        { placeHolder: 'How should this publish bump the version?', title: 'Publish package' }
    );
    if (!choice) {
        return;
    }
    deps.runInTerminal(`publish ${choice.args}`);
}

/** Search-as-you-type over the registry, ending in a terminal `registry add`. */
async function searchAndAdd(deps: RegistryCommandDeps): Promise<void> {
    const client = deps.daemonClient();
    if (!client?.isConnected || !client.hasCapability('registry-search')) {
        const action = await vscode.window.showWarningMessage(
            'Registry search needs a connected, up-to-date daemon.',
            'Restart Daemon'
        );
        if (action === 'Restart Daemon') {
            await vscode.commands.executeCommand('bino.restartDaemon');
        }
        return;
    }

    const picked = await new Promise<RegistrySearchItem | undefined>((resolve) => {
        const qp = vscode.window.createQuickPick<vscode.QuickPickItem & { item?: RegistrySearchItem }>();
        qp.title = 'Search Bino Registry';
        qp.placeholder = 'Type to search packages (e.g. kpi, @acme, table)…';
        qp.matchOnDescription = true;
        qp.matchOnDetail = true;

        let timer: ReturnType<typeof setTimeout> | undefined;
        let generation = 0;
        const runSearch = async (value: string) => {
            const gen = ++generation;
            qp.busy = true;
            const result = await client.searchRegistry(value, { perPage: 25 });
            if (gen !== generation) {
                return; // superseded by newer input
            }
            qp.busy = false;
            if (!result || result.error) {
                qp.items = [{ label: '$(error) Search failed', detail: result?.error ?? 'daemon unreachable' }];
                return;
            }
            qp.items = result.items.map((item) => ({
                label: item.package,
                description: `${item.kind} · ${item.latestVersion}`,
                detail: item.description,
                item,
            }));
        };

        qp.onDidChangeValue((value) => {
            if (timer) {
                clearTimeout(timer);
            }
            timer = setTimeout(() => void runSearch(value), 300);
        });
        qp.onDidAccept(() => {
            resolve(qp.selectedItems[0]?.item);
            qp.hide();
        });
        qp.onDidHide(() => {
            resolve(undefined);
            qp.dispose();
        });
        qp.show();
        void runSearch('');
    });
    if (!picked) {
        return;
    }

    const action = await vscode.window.showQuickPick(
        [
            { label: `Add ${picked.package} (follow latest)`, args: `registry add ${picked.package}` },
            { label: `Add ${picked.package}@${picked.latestVersion} (pin)`, args: `registry add ${picked.package}@${picked.latestVersion}` },
        ],
        { placeHolder: `Add ${picked.package} to this project?` }
    );
    if (action) {
        deps.runInTerminal(action.args);
    }
}

/** Inserts a `- kind/ref/params` child snippet for an installed package at the cursor. */
async function insertComponentRef(deps: RegistryCommandDeps, item?: PackageItem): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.languageId !== 'yaml') {
        vscode.window.showWarningMessage('Open a bino YAML manifest to insert a component reference.');
        return;
    }

    let pkg = item?.pkg;
    if (!pkg) {
        const client = deps.daemonClient();
        const result = client?.isConnected ? await client.getRegistryPackages() : undefined;
        const installed = (result?.packages ?? []).filter((p) => p.installed && p.kind);
        if (installed.length === 0) {
            vscode.window.showInformationMessage('No installed registry packages to reference.');
            return;
        }
        const picked = await vscode.window.showQuickPick(
            installed.map((p) => ({ label: p.name, description: `${p.kind} · ${p.version}`, pkg: p })),
            { placeHolder: 'Insert a reference to which package?' }
        );
        pkg = picked?.pkg;
    }
    if (!pkg) {
        return;
    }
    await editor.insertSnippet(buildRefSnippet(pkg));
}

/** Plain-text form of a ref child (for the clipboard). */
function refSnippetText(pkg: RegistryPackage): string {
    return `- kind: ${pkg.kind ?? 'Table'}\n  ref: "${pkg.name}"\n`;
}

/**
 * Snippet form of a ref child: required-without-default params are prefilled
 * as tabstops, select params as choice tabstops.
 */
function buildRefSnippet(pkg: RegistryPackage): vscode.SnippetString {
    const snippet = new vscode.SnippetString();
    snippet.appendText(`- kind: ${pkg.kind ?? 'Table'}\n`);
    snippet.appendText(`  ref: "${pkg.name}"\n`);
    const required = (pkg.params ?? []).filter((p) => p.required && p.default === undefined);
    if (required.length > 0) {
        snippet.appendText('  params:\n');
        for (const param of required) {
            snippet.appendText(`    ${param.name}: `);
            if (param.options && param.options.length > 0) {
                snippet.appendChoice(param.options);
            } else {
                snippet.appendTabstop();
            }
            snippet.appendText('\n');
        }
    }
    return snippet;
}
