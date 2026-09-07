/**
 * ⍟ and ≡ are two glyphs, and ⍟'s id is written down in two places that have
 * to agree: the tray registration, and the migration that renamed what the
 * canvas already stored.
 *
 * Personas:
 * - Spike changes one and not the other, and a saved placement is orphaned —
 *   the canvas holds `i-glyph` while the code registers something else, so the
 *   glyph a person put on their canvas never comes back.
 *
 * This reads the files rather than importing them. Importing a glyph pulls the
 * client, the socket and the WASM module in behind it, and `mock.module` is
 * process-global — an earlier file's mock wins and this one would never run.
 */

import { describe, test, expect } from 'bun:test';
import { readFileSync } from 'fs';
import { join } from 'path';

const HERE = import.meta.dir;
const MIGRATION = join(HERE, '../../db/sqlite/migrations/061_rename_self_glyph_to_i.sql');

const i = readFileSync(join(HERE, 'i-glyph.ts'), 'utf8');
const am = readFileSync(join(HERE, 'am-glyph.ts'), 'utf8');
const migration = readFileSync(MIGRATION, 'utf8');

describe('the split', () => {
    test('⍟ registers under its word', () => {
        expect(i).toContain("id: 'i-glyph'");
        expect(i).toContain("symbol: '⍟'");
        expect(i).toContain("title: 'i'");
    });

    test('≡ is its own glyph', () => {
        expect(am).toContain("id: 'am-glyph'");
        expect(am).toContain("symbol: '≡'");
        expect(am).toContain("title: 'am'");
    });

    test('the migration renames to the id the tray registers', () => {
        expect(migration).toContain("'i-glyph'");
        expect(migration).toContain("'self-glyph'");
        // Every table a glyph id is still kept in, after 021 and 047.
        expect(migration).toContain('canvas_glyphs');
        expect(migration).toContain('composition_edges');
        expect(migration).toContain('minimized_windows');
    });

    test('nothing is called self afterwards', () => {
        expect(i).not.toContain('self-glyph');
        expect(am).not.toContain('self-glyph');
    });
});
