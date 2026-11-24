import * as vscode from 'vscode';
import { getBinoPath, INSTALL_DOCS_URL, runBino } from './bino';

export interface SetupCheckResult {
    binoFound: boolean;
    binoPath: string;
    version?: string;
    hasLspHelper: boolean;
    hasValidate: boolean;
    hasLsp: boolean;
    error?: string;
}

/**
 * Check if bino CLI is properly set up and available.
 */
export async function checkBinoSetup(): Promise<SetupCheckResult> {
    const binPath = getBinoPath();

    const result: SetupCheckResult = {
        binoFound: false,
        binoPath: binPath,
        hasLspHelper: false,
        hasValidate: false,
        hasLsp: false
    };

    try {
        // Check version
        const versionOutput = await execCommand(['version']);
        result.binoFound = true;
        result.version = versionOutput.trim();

        // Check lsp-helper index
        try {
            await execCommand(['lsp-helper', '--help']);
            result.hasLspHelper = true;
        } catch {
            result.hasLspHelper = false;
        }

        // Check lsp-helper validate
        try {
            const validateHelp = await execCommand(['lsp-helper', 'validate', '--help']);
            result.hasValidate = validateHelp.includes('validate');
        } catch {
            result.hasValidate = false;
        }

        // Check the language server (bino lsp) - the editor's primary integration.
        try {
            await execCommand(['lsp', '--help']);
            result.hasLsp = true;
        } catch {
            result.hasLsp = false;
        }

    } catch (err) {
        result.error = `${err}`;
    }

    return result;
}

/**
 * Show setup check results to user.
 */
export async function showSetupCheckResults(): Promise<void> {
    const result = await vscode.window.withProgress(
        {
            location: vscode.ProgressLocation.Notification,
            title: 'Checking Bino setup...',
            cancellable: false
        },
        async () => checkBinoSetup()
    );

    if (!result.binoFound) {
        const action = await vscode.window.showErrorMessage(
            `Bino CLI not found at '${result.binoPath}'. ${result.error || ''}`,
            'Configure Path',
            'Install Instructions'
        );

        if (action === 'Configure Path') {
            vscode.commands.executeCommand('workbench.action.openSettings', 'bino.binPath');
        } else if (action === 'Install Instructions') {
            vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
        }
        return;
    }

    const features: string[] = [];
    if (result.hasLsp) {
        features.push('language server');
    }
    if (result.hasValidate) {
        features.push('validation');
    }

    const featureStr = features.length > 0 ? `Features: ${features.join(', ')}` : 'Basic features only';

    const message = [
        `✓ Bino CLI found`,
        `Version: ${result.version || 'unknown'}`,
        `Path: ${result.binoPath}`,
        featureStr
    ].join('\n');

    if (!result.hasLsp) {
        vscode.window.showWarningMessage(
            `Bino found but the language server (\`bino lsp\`) is not available. Update bino for completion, hover, navigation, and diagnostics.\n${message}`
        );
    } else {
        vscode.window.showInformationMessage(message);
    }
}

async function execCommand(args: string[]): Promise<string> {
    try {
        return await runBino(args);
    } catch (err) {
        const e = err as { message: string; stderr?: string };
        throw new Error(e.stderr || e.message);
    }
}
