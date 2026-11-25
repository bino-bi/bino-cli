// Writes src/styles/engineDefaults.json: the built-in style and rule-set
// values of the template engine. The ComponentStyle and RuleSet editors show
// them as the inherited value of an empty field.
//
// Usage: node scripts/update-engine-defaults.mjs <bn-template-engine checkout>
import { build } from 'esbuild';
import { execFileSync } from 'child_process';
import { writeFileSync } from 'fs';
import path from 'path';

const engine = process.argv[2];
if (!engine) {
    console.error('usage: node scripts/update-engine-defaults.mjs <bn-template-engine checkout>');
    process.exit(2);
}
const result = await build({
    stdin: {
        contents: `export { SYSTEM_STYLE_DEFAULTS as style } from './src/stores/componentStyles';
export { SYSTEM_RULESET_DEFAULTS as ruleSet } from './src/stores/ruleSets';`,
        resolveDir: path.resolve(engine),
        loader: 'ts',
    },
    bundle: true,
    format: 'cjs',
    platform: 'node',
    write: false,
    logLevel: 'error',
});
const mod = { exports: {} };
new Function('module', 'exports', 'require', result.outputFiles[0].text)(mod, mod.exports, () => ({}));
let version = 'unknown';
try {
    version = execFileSync('git', ['-C', engine, 'describe', '--tags'], { encoding: 'utf8' }).trim();
} catch { /* not a git checkout */ }
const out = { engineVersion: version, style: mod.exports.style, ruleSet: mod.exports.ruleSet };
const target = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'src', 'styles', 'engineDefaults.json');
writeFileSync(target, JSON.stringify(out, null, 2) + '\n');
console.log(`wrote ${target} from engine ${version}`);
