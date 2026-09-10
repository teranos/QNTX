/**
 * The id of a standing watcher is said in two languages.
 *
 * Go declares it, the node broadcasts under it, and the page routes on it. The
 * two are not generated from one source — typegen's package list lives in
 * another repo — so this reads the Go and fails if they ever say different
 * things. A silent drift here is a page that never wakes and never says why,
 * which is exactly the failure /g/ was built to remove.
 */

import { describe, test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { STANDING_GLYPH_PUBLISHED } from './plugin-provided-glyphs';

const STANDING_GO = resolve(import.meta.dir, '../../../../ats/watcher/standing.go');

/** What Go declares a const to be, by its name. */
function goConst(source: string, name: string): string | undefined {
    for (const line of source.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed.startsWith(`const ${name} `) && !trimmed.startsWith(`${name} `)) continue;
        const opened = trimmed.indexOf('"');
        if (opened === -1) continue;
        const closed = trimmed.indexOf('"', opened + 1);
        if (closed === -1) continue;
        return trimmed.slice(opened + 1, closed);
    }
    return undefined;
}

describe('standing watcher ids', () => {
    test('the page routes on the id Go broadcasts under', () => {
        const source = readFileSync(STANDING_GO, 'utf8');
        const declared = goConst(source, 'StandingGlyphPublished');

        expect(declared).toBeDefined();
        expect(STANDING_GLYPH_PUBLISHED).toBe(declared!);
    });

    test('the id is in the table, not only declared beside it', () => {
        const source = readFileSync(STANDING_GO, 'utf8');
        expect(source).toContain('ID:   StandingGlyphPublished');
    });
});
