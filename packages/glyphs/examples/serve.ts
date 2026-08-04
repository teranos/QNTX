#!/usr/bin/env bun
/**
 * Runs the package's examples. Bundles main.ts on each request so an edit
 * is a reload, and serves index.html beside it. No host stylesheets — the
 * page carries the package's own black and white.
 */

import { join } from 'path';

const dir = import.meta.dir;
const PORT = 5180;

Bun.serve({
    port: PORT,
    async fetch(req) {
        const path = new URL(req.url).pathname;

        if (path === '/main.js') {
            const built = await Bun.build({ entrypoints: [join(dir, 'main.ts')], sourcemap: 'inline' });
            if (!built.success) {
                return new Response(built.logs.join('\n'), { status: 500 });
            }
            return new Response(await built.outputs[0].text(), {
                headers: { 'Content-Type': 'text/javascript' },
            });
        }

        return new Response(Bun.file(join(dir, 'index.html')), {
            headers: { 'Content-Type': 'text/html' },
        });
    },
});

console.log(`@qntx/glyphs examples → http://localhost:${PORT}`);
