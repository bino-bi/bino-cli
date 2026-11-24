import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';

interface GrammarContribution {
    scopeName: string;
    path: string;
    injectTo?: string[];
}

/** A token as VS Code's internal tokenizer command reports it. */
interface CapturedToken {
    c: string;
    t: string;
}

const extensionRoot = path.resolve(__dirname, '../..');

function grammars(): GrammarContribution[] {
    const pkg = JSON.parse(fs.readFileSync(path.join(extensionRoot, 'package.json'), 'utf8'));
    return pkg.contributes.grammars as GrammarContribution[];
}

function readGrammar(scopeName: string): Record<string, unknown> {
    const g = grammars().find(x => x.scopeName === scopeName);
    assert.ok(g, `no grammar contributed for ${scopeName}`);
    const raw = JSON.parse(fs.readFileSync(path.join(extensionRoot, g.path), 'utf8'));
    assert.strictEqual(raw.scopeName, scopeName);
    return raw;
}

suite('query grammar', () => {
    test('the SQL/PRQL injection is contributed to YAML', () => {
        const injection = grammars().find(g => g.scopeName === 'bino.query.injection');
        assert.deepStrictEqual(injection?.injectTo, ['source.yaml']);
        const text = JSON.stringify(readGrammar('bino.query.injection'));
        for (const scope of ['source.sql', 'source.prql.bino', 'meta.embedded.block.sql', 'meta.embedded.block.prql']) {
            assert.ok(text.includes(scope), `injection does not reference ${scope}`);
        }
    });

    test('the PRQL grammar is valid', () => {
        const prql = readGrammar('source.prql.bino');
        const repo = prql.repository as Record<string, unknown>;
        for (const p of prql.patterns as { include: string }[]) {
            assert.ok(repo[p.include.replace(/^#/, '')], `missing repository entry ${p.include}`);
        }
    });

    test('query and prql blocks get SQL and PRQL scopes', async function () {
        const file = vscode.Uri.file(path.join(extensionRoot, 'src/test/fixtures/grammar/queries.yaml'));
        let tokens: CapturedToken[] | undefined;
        try {
            tokens = await vscode.commands.executeCommand<CapturedToken[]>('_workbench.captureSyntaxTokens', file);
        } catch {
            tokens = undefined;
        }
        if (!tokens?.length) {
            this.skip(); // the internal tokenizer command is not available in this VS Code
        }
        const scopesOf = (text: string, nth = 0) => tokens!.filter(t => t.c.trim() === text)[nth]?.t ?? '';
        assert.match(scopesOf('SELECT'), /meta\.embedded\.block\.sql.*keyword/);
        assert.match(scopesOf('SELECT', 1), /meta\.embedded\.line\.sql.*keyword/);
        assert.match(scopesOf('from'), /meta\.embedded\.block\.prql.*keyword\.control\.prql/);
        assert.match(scopesOf('|', 2), /keyword\.operator\.pipe\.prql/); // after the two block indicators
        // The YAML after a block is YAML again.
        assert.doesNotMatch(scopesOf('title'), /meta\.embedded/);
        assert.match(scopesOf('title'), /entity\.name\.tag\.yaml/);
    });
});
