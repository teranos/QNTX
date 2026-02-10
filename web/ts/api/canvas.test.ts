/**
 * Tests for canvas state merge logic.
 *
 * mergeCanvasState is the pure function that reconciles local (IndexedDB) state
 * with backend (SQLite) state on startup. Local wins on ID conflict.
 */

import { describe, test, expect } from 'bun:test';
import { mergeCanvasState } from './canvas';
import type { CanvasGlyphState, CompositionState } from '../state/ui';

// --- helpers ---

function glyph(id: string, overrides: Partial<CanvasGlyphState> = {}): CanvasGlyphState {
    return { id, symbol: 'ax', x: 0, y: 0, ...overrides };
}

function composition(id: string, overrides: Partial<CompositionState> = {}): CompositionState {
    return { id, edges: [], x: 0, y: 0, ...overrides };
}

const empty = { glyphs: [] as CanvasGlyphState[], compositions: [] as CompositionState[] };

// --- unit tests ---

describe('mergeCanvasState', () => {
    test('backend-only glyphs are appended to local state', () => {
        const local = { ...empty, glyphs: [glyph('a')] };
        const backend = { ...empty, glyphs: [glyph('a'), glyph('b')] };

        const result = mergeCanvasState(local, backend);

        expect(result.glyphs.map(g => g.id)).toEqual(['a', 'b']);
        expect(result.mergedGlyphs).toBe(1);
    });

    test('local wins on ID conflict -- position preserved', () => {
        const local = { ...empty, glyphs: [glyph('a', { x: 50, y: 50 })] };
        const backend = { ...empty, glyphs: [glyph('a', { x: 0, y: 0 })] };

        const result = mergeCanvasState(local, backend);

        expect(result.glyphs).toHaveLength(1);
        expect(result.glyphs[0].x).toBe(50);
        expect(result.mergedGlyphs).toBe(0);
    });

    test('empty backend changes nothing', () => {
        const local = { ...empty, glyphs: [glyph('a')] };

        const result = mergeCanvasState(local, empty);

        expect(result.glyphs).toBe(local.glyphs); // same reference, no copy
        expect(result.mergedGlyphs).toBe(0);
    });

    test('empty local receives all backend items', () => {
        const backend = { ...empty, glyphs: [glyph('a'), glyph('b')] };

        const result = mergeCanvasState(empty, backend);

        expect(result.glyphs.map(g => g.id)).toEqual(['a', 'b']);
        expect(result.mergedGlyphs).toBe(2);
    });

    test('compositions merge independently of glyphs', () => {
        const local = {
            glyphs: [glyph('g1')],
            compositions: [composition('c1')],
        };
        const backend = {
            glyphs: [glyph('g1'), glyph('g2')],
            compositions: [composition('c1'), composition('c2'), composition('c3')],
        };

        const result = mergeCanvasState(local, backend);

        expect(result.mergedGlyphs).toBe(1);
        expect(result.mergedComps).toBe(2);
        expect(result.compositions.map(c => c.id)).toEqual(['c1', 'c2', 'c3']);
    });

    test('both empty returns empty', () => {
        const result = mergeCanvasState(empty, empty);

        expect(result.glyphs).toHaveLength(0);
        expect(result.compositions).toHaveLength(0);
        expect(result.mergedGlyphs).toBe(0);
        expect(result.mergedComps).toBe(0);
    });
});

// --- Jenny's tube journey ---
//
// Jenny is a researcher at UCL. Kofi, a collaborator in Accra, worked overnight
// building an analysis pipeline on their shared QNTX instance: an AX query glyph
// feeding into a Python transform, wired to a prompt glyph, with a composition
// melding them into a single strip.
//
// Jenny gets off her bike at King's Cross, opens QNTX on her phone. Her local
// IndexedDB has only the AX glyph she left there yesterday. The backend has
// everything Kofi built overnight.
//
// She should see her glyph plus all of Kofi's work -- no duplicates, her local
// position preserved for the shared AX glyph.

describe('Jenny opens QNTX at King\'s Cross', () => {
    // Jenny's phone -- one AX glyph from yesterday, positioned where she left it
    const jennyLocal = {
        glyphs: [
            glyph('ax-jenny', { symbol: 'ax', x: 120, y: 80 }),
        ],
        compositions: [],
    };

    // Backend after Kofi's overnight session in Accra
    const backendAfterKofi = {
        glyphs: [
            glyph('ax-jenny', { symbol: 'ax', x: 0, y: 0 }),    // same glyph, Kofi may have moved it
            glyph('py-kofi', { symbol: 'py', x: 200, y: 0 }),    // Kofi's python transform
            glyph('prompt-kofi', { symbol: 'prompt', x: 400, y: 0 }), // Kofi's prompt glyph
        ],
        compositions: [
            composition('strip-kofi', {
                edges: [
                    { from: 'ax-jenny', to: 'py-kofi', direction: 'right' as const, position: 0 },
                    { from: 'py-kofi', to: 'prompt-kofi', direction: 'right' as const, position: 1 },
                ],
            }),
        ],
    };

    test('Jenny sees her glyph plus Kofi\'s overnight work', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterKofi);

        // All three glyphs present
        expect(merged.glyphs).toHaveLength(3);
        expect(merged.glyphs.map(g => g.id)).toEqual(['ax-jenny', 'py-kofi', 'prompt-kofi']);
    });

    test('Jenny\'s local position is preserved, not overwritten by Kofi\'s move', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterKofi);

        const jennyGlyph = merged.glyphs.find(g => g.id === 'ax-jenny')!;
        expect(jennyGlyph.x).toBe(120);
        expect(jennyGlyph.y).toBe(80);
    });

    test('Kofi\'s composition arrives intact', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterKofi);

        expect(merged.compositions).toHaveLength(1);
        expect(merged.compositions[0].id).toBe('strip-kofi');
        expect(merged.compositions[0].edges).toHaveLength(2);
    });

    test('merge counts reflect what was new to Jenny', () => {
        const merged = mergeCanvasState(jennyLocal, backendAfterKofi);

        expect(merged.mergedGlyphs).toBe(2);  // py-kofi + prompt-kofi
        expect(merged.mergedComps).toBe(1);    // strip-kofi
    });

    // TODO(#canvas-live-sync): Jenny won't see changes Kofi makes AFTER she opens
    // the app. That requires a WebSocket `canvas_update` broadcast -- backend emits
    // glyph/composition mutations to all connected clients for live merge.
});
