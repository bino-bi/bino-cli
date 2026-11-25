import * as assert from 'assert';
import * as vscode from 'vscode';

const DESIGNER = 'bino.designer';
const TREE = 'bino.treeEditor';
const STYLE = 'bino.styleEditor';
const RULESET = 'bino.ruleSetEditor';

function fixtureUri(): vscode.Uri {
    const folder = vscode.workspace.workspaceFolders?.[0];
    assert.ok(folder, 'no workspace folder');
    return vscode.Uri.joinPath(folder.uri, 'designer-components.yaml');
}

function findCustomTab(viewType: string, uri: vscode.Uri): vscode.Tab | undefined {
    for (const group of vscode.window.tabGroups.all) {
        for (const tab of group.tabs) {
            const input = tab.input;
            if (input instanceof vscode.TabInputCustom && input.viewType === viewType && input.uri.toString() === uri.toString()) {
                return tab;
            }
        }
    }
    return undefined;
}

async function waitForCustomTab(viewType: string, uri: vscode.Uri): Promise<vscode.Tab> {
    for (let i = 0; i < 50; i++) {
        const tab = findCustomTab(viewType, uri);
        if (tab) { return tab; }
        await new Promise(r => setTimeout(r, 100));
    }
    assert.fail(`no ${viewType} tab for ${uri.fsPath}`);
}

suite('custom editors', () => {
    suiteSetup(async () => {
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        assert.ok(ext, 'extension not found');
        await ext.activate();
    });

    teardown(async () => {
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    });

    test('designer opens as a custom editor and reopens after close', async () => {
        const uri = fixtureUri();
        await vscode.commands.executeCommand('vscode.openWith', uri, DESIGNER);
        const tab = await waitForCustomTab(DESIGNER, uri);

        await vscode.window.tabGroups.close(tab);
        assert.strictEqual(findCustomTab(DESIGNER, uri), undefined);

        await vscode.commands.executeCommand('vscode.openWith', uri, DESIGNER);
        await waitForCustomTab(DESIGNER, uri);
    });

    test('Open in Designer opens the custom editor for the component at the cursor', async () => {
        const uri = fixtureUri();
        const editor = await vscode.window.showTextDocument(uri);
        const pos = editor.document.positionAt(editor.document.getText().indexOf('kind: Table'));
        editor.selection = new vscode.Selection(pos, pos);

        await vscode.commands.executeCommand('bino.openInDesigner');
        await waitForCustomTab(DESIGNER, uri);
    });

    test('Open Tree Editor opens the custom editor for the active YAML file', async () => {
        const uri = fixtureUri();
        await vscode.window.showTextDocument(uri);

        await vscode.commands.executeCommand('bino.openTreeEditor');
        await waitForCustomTab(TREE, uri);
    });

    test('designer edits through the document can be undone', async () => {
        const uri = fixtureUri();
        await vscode.commands.executeCommand('vscode.openWith', uri, DESIGNER);
        await waitForCustomTab(DESIGNER, uri);
        const doc = await vscode.workspace.openTextDocument(uri);
        const before = doc.getText();

        const edit = new vscode.WorkspaceEdit();
        edit.insert(uri, new vscode.Position(0, 0), '# note\n');
        assert.ok(await vscode.workspace.applyEdit(edit));
        assert.notStrictEqual(doc.getText(), before);

        await vscode.commands.executeCommand('undo');
        assert.strictEqual(doc.getText(), before);
        await vscode.commands.executeCommand('workbench.action.files.revert');
    });

    for (const viewType of [STYLE, RULESET]) {
        test(`${viewType} opens, reopens after close, and its document edits can be undone`, async () => {
            const folder = vscode.workspace.workspaceFolders?.[0];
            assert.ok(folder);
            const uri = vscode.Uri.joinPath(folder.uri, 'style-rules.yaml');
            await vscode.commands.executeCommand('vscode.openWith', uri, viewType);
            const tab = await waitForCustomTab(viewType, uri);
            await vscode.window.tabGroups.close(tab);
            assert.strictEqual(findCustomTab(viewType, uri), undefined);
            await vscode.commands.executeCommand('vscode.openWith', uri, viewType);
            await waitForCustomTab(viewType, uri);

            const doc = await vscode.workspace.openTextDocument(uri);
            const before = doc.getText();
            const edit = new vscode.WorkspaceEdit();
            edit.insert(uri, new vscode.Position(0, 0), '# note\n');
            assert.ok(await vscode.workspace.applyEdit(edit));
            await vscode.commands.executeCommand('undo');
            assert.strictEqual(doc.getText(), before);
            await vscode.commands.executeCommand('workbench.action.files.revert');
        });
    }

    test('the open commands open the editor for the document at the cursor', async () => {
        const folder = vscode.workspace.workspaceFolders?.[0];
        assert.ok(folder);
        const uri = vscode.Uri.joinPath(folder.uri, 'style-rules.yaml');
        const editor = await vscode.window.showTextDocument(uri);
        const pos = editor.document.positionAt(editor.document.getText().indexOf('kind: RuleSet'));
        editor.selection = new vscode.Selection(pos, pos);
        await vscode.commands.executeCommand('bino.openRuleSetEditor');
        await waitForCustomTab(RULESET, uri);

        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
        await vscode.window.showTextDocument(uri);
        await vscode.commands.executeCommand('bino.openStyleEditor');
        await waitForCustomTab(STYLE, uri);
    });
});
