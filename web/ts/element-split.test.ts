/**
 * ⍟ and ≡ are two elements, and ⍟'s id is written down in two places that have
 * to agree: the tray registration, and the migration that renamed what the
 * canvas already stored.
 *
 * Personas:
 * - Spike changes one and not the other, and a saved placement is orphaned —
 *   the canvas holds `i-element` while the code registers something else, so the
 *   element a person put on their canvas never comes back.
 *
 * This reads the files rather than importing them. Importing an element pulls the
 * client, the socket and the WASM module in behind it, and `mock.module` is
 * process-global — an earlier file's mock wins and this one would never run.
 */

import { describe, test, expect } from 'bun:test';
import { readFileSync } from 'fs';
import { join } from 'path';

const HERE = import.meta.dir;
const MIGRATIONS = join(HERE, '../../db/sqlite/migrations');

const i = readFileSync(join(HERE, 'i-element.ts'), 'utf8');
const am = readFileSync(join(HERE, 'am-element.ts'), 'utf8');
// 061 wrote self's id as i-glyph; 066 moves every fixed -glyph id to -element.
const toI = readFileSync(join(MIGRATIONS, '061_rename_self_glyph_to_i.sql'), 'utf8');
const toElement = readFileSync(join(MIGRATIONS, '066_a_glyph_is_an_element.sql'), 'utf8');

describe('the split', () => {
    test('⍟ registers under its word', () => {
        expect(i).toContain("id: 'i-element'");
        expect(i).toContain('symbol: I');
        expect(i).toContain("title: 'i'");
    });

    test('≡ is its own element', () => {
        expect(am).toContain("id: 'am-element'");
        expect(am).toContain('symbol: AM');
        expect(am).toContain("title: 'am'");
    });

    test('the migrations rename to the id the tray registers', () => {
        expect(toI).toContain("'i-glyph'");
        expect(toI).toContain("'self-glyph'");
        expect(toElement).toContain("LIKE '%-glyph'");
        expect(toElement).toContain("|| 'element'");
        // Every table an element id is still kept in, after 021 and 047.
        expect(toElement).toContain('UPDATE canvas_elements SET id');
        expect(toElement).toContain('UPDATE composition_edges SET from_element_id');
        expect(toElement).toContain('UPDATE composition_edges SET to_element_id');
        expect(toElement).toContain('UPDATE minimized_windows SET element_id');
    });

    test('nothing is called self afterwards', () => {
        expect(i).not.toContain('self-element');
        expect(am).not.toContain('self-element');
    });
});
