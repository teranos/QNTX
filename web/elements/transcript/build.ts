// Builds the transcript element into dist/element-module.js, the file published.

import { compile } from 'svelte/compiler';
import { join } from 'path';

const src = join(import.meta.dir, 'src');
const components = ['App', 'Transcript', 'Warp'];

// Each .svelte is compiled next to itself, so the bundler sees plain JavaScript.
for (const name of components) {
    const path = join(src, name + '.svelte');
    const result = compile(await Bun.file(path).text(), { filename: path, generate: 'client', css: 'injected' });
    let code = result.js.code;
    for (const other of components) {
        code = code.replaceAll("'./" + other + ".svelte'", "'./" + other + ".svelte.js'");
    }
    await Bun.write(join(src, name + '.svelte.js'), code);
}

const entry = join(src, 'element-module.ts');
const staged = join(src, 'element-module.staged.ts');
await Bun.write(staged, (await Bun.file(entry).text()).replaceAll("'./App.svelte'", "'./App.svelte.js'"));

const built = await Bun.build({
    entrypoints: [staged],
    outdir: join(import.meta.dir, 'dist'),
    naming: 'element-module.js',
    target: 'browser',
    format: 'esm',
});
if (!built.success) {
    for (const log of built.logs) console.error(log);
    process.exit(1);
}
console.log('built dist/element-module.js');
