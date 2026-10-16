// "There's no doubt in my mind that Elements owns it's own font sizes."
// "I WANT QNTX WEB TO FAIL"
// A stylesheet picks a size by its token, and a token says the package's number for it.

// ESLint stops a size in TypeScript (NO_FONT_SIZE) and cannot read a stylesheet: this reads every
// one under web/, and index.html, in `bun run lint`.

import { readdirSync, readFileSync } from 'fs';
import { join, relative } from 'path';
// The package's index holds the page's scale as it loads, which needs a DOM; its text module does not.
import { TEXT_SIZES, FIELD_TEXT_SIZE } from './node_modules/@teranos/elements/text.js';

const TOKEN = '--font-size-';

/** px: each token a stylesheet may pick, and the package's number for it. */
const PICKABLE: ReadonlyMap<string, number> = new Map([
    ...Object.entries(TEXT_SIZES),
    ['field', FIELD_TEXT_SIZE],
]);

// A declaration's name, as `name: value`. In markup the name follows the style attribute's quote.
function nameOf(declaration: string): string {
    const before = declaration.slice(0, declaration.indexOf(':')).split('"').join(' ').split("'").join(' ').trim();
    return before.slice(before.lastIndexOf(' ') + 1);
}

// A declaration that sizes text other than by the package's sizes. Inherit is no size of its
// own: the text stands at the size around it.
function ownSize(declaration: string): boolean {
    if (!declaration.includes(':')) return false;
    const name = nameOf(declaration);
    // In markup the value ends at the attribute's closing quote.
    const value = declaration.slice(declaration.indexOf(':') + 1).split('"').slice(0, 1).join('').trim();
    if (name === 'font') return value !== 'inherit';
    if (name === 'font-size') {
        if (value === 'inherit') return false;
        if (!value.startsWith('var(' + TOKEN) || !value.endsWith(')')) return true;
        return !PICKABLE.has(value.slice(('var(' + TOKEN).length, -1));
    }
    if (name.startsWith(TOKEN)) {
        const token = name.slice(TOKEN.length);
        return !PICKABLE.has(token) || value !== `${PICKABLE.get(token)}px`;
    }
    return false;
}

/** The lines of this stylesheet that size text other than by the package's sizes. */
export function sizesIn(text: string): number[] {
    const lines: number[] = [];
    text.split('\n').forEach((line, i) => {
        const said = line.trim();
        // A comment saying font-size sizes nothing.
        if (said.startsWith('/*') || said.startsWith('*')) return;
        const declarations = said.split('{').join(';').split('}').join(';').split(';');
        if (declarations.some(ownSize)) lines.push(i + 1);
    });
    return lines;
}

/** Every stylesheet and index.html under root, as `file:line` for each size in them. */
export function sizesUnder(root: string): string[] {
    const found: string[] = [];
    const read = (dir: string): void => {
        for (const entry of readdirSync(dir, { withFileTypes: true })) {
            if (entry.name === 'node_modules' || entry.name === 'dist') continue;
            const path = join(dir, entry.name);
            if (entry.isDirectory()) { read(path); continue; }
            if (!entry.name.endsWith('.css') && entry.name !== 'index.html') continue;
            for (const line of sizesIn(readFileSync(path, 'utf8'))) found.push(`${relative(root, path)}:${line}`);
        }
    };
    read(root);
    return found;
}

if (import.meta.main) {
    const found = sizesUnder(import.meta.dir);
    if (found.length > 0) {
        const sizes = [...PICKABLE.keys()].join(', ');
        for (const at of found) console.error(`${at}: text size is @teranos/elements'. A stylesheet picks one by its token, font-size: var(${TOKEN}<${sizes}>).`);
        console.error(`${found.length} sizes written in a stylesheet.`);
        process.exit(1);
    }
}
