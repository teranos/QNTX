#!/usr/bin/env bun
/**
 * Serves the specimens page for the browser gate: index.html as the node
 * serves it, with the specimens bundle (ts/specimens.ts) in main's place.
 */

// Nothing here answers the API. The gate does, from fixtures/, so every call
// an element makes is one the gate answered or one it names as missing.

import { extname, join } from 'path';

const web = join(import.meta.dir, '..');
const PORT = 5181;

const TYPES: Record<string, string> = {
    '.css': 'text/css',
    '.woff2': 'font/woff2',
    '.woff': 'font/woff',
    '.ttf': 'font/ttf',
    '.jpg': 'image/jpeg',
    '.png': 'image/png',
    '.svg': 'image/svg+xml',
};

/** The bundle, built once: every test opens the page, and the page is the same each time. */
let bundle: Promise<{ js: string; failed: string }> | null = null;

function specimens(): Promise<{ js: string; failed: string }> {
    if (!bundle) {
        bundle = Bun.build({ entrypoints: [join(web, 'ts', 'specimens.ts')], sourcemap: 'inline', throw: false }).then(async (built) => {
            if (!built.success) {
                return { js: '', failed: built.logs.map((entry) => String(entry)).join('\n') };
            }
            return { js: await built.outputs[0]!.text(), failed: '' };
        });
    }
    return bundle;
}

Bun.serve({
    port: PORT,
    async fetch(req) {
        const path = new URL(req.url).pathname;

        if (path === '/' || path === '/index.html') {
            const html = await Bun.file(join(web, 'index.html')).text();
            return new Response(html.replace('/ts/main.ts', '/js/specimens.js'), {
                headers: { 'Content-Type': 'text/html' },
            });
        }

        if (path === '/js/specimens.js') {
            const bundle = await specimens();
            if (bundle.failed) {
                return new Response(bundle.failed, { status: 500 });
            }
            return new Response(bundle.js, {
                headers: { 'Content-Type': 'text/javascript' },
            });
        }

        if ((path.startsWith('/css/') || path.startsWith('/fonts/') || path === '/qntx.jpg') && !path.includes('..')) {
            const file = Bun.file(join(web, path));
            if (!(await file.exists())) {
                return new Response(`${path} is not under web/`, { status: 404 });
            }
            return new Response(file, { headers: { 'Content-Type': TYPES[extname(path)] ?? 'application/octet-stream' } });
        }

        return new Response(`${req.method} ${path}: no node stands behind the specimens page, the gate answers the API from browser/fixtures`, { status: 404 });
    },
});

console.log(`QNTX specimens → http://localhost:${PORT}`);
