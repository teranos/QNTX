/**
 * Text size is @teranos/elements'. A size written in a stylesheet stops the build.
 *
 * "There's no doubt in my mind that Elements owns it's own font sizes."
 * "I WANT QNTX WEB TO FAIL"
 *
 * ESLint stops one written in TypeScript (eslint.config.js, NO_FONT_SIZE) and cannot read a
 * stylesheet: this reads every one under web/, and index.html. Run by `bun run lint`.
 */

import { readdirSync, readFileSync } from 'fs';
import { join, relative } from 'path';

/** The lines of this stylesheet that size text: font-size, a font-size token, or the font shorthand. */
export function sizesIn(text: string): number[] {
    const lines: number[] = [];
    text.split('\n').forEach((line, i) => {
        const said = line.trim();
        // A comment saying font-size sizes nothing.
        if (said.startsWith('/*') || said.startsWith('*')) return;
        const shorthand = said.startsWith('font:') || said.includes(' font:') || said.includes('{font:') || said.includes(';font:');
        if (said.includes('font-size') || shorthand) lines.push(i + 1);
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
        for (const at of found) console.error(`${at}: text size is @teranos/elements' (sizeText, TEXT_SIZES). A stylesheet does not size text.`);
        console.error(`${found.length} sizes written in a stylesheet.`);
        process.exit(1);
    }
}
