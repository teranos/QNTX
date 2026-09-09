/**
 * Names kept after a rename.
 *
 * Personas:
 * - Tim: the new name is the one to call
 * - Spike: the old name still works, and is the same function
 */

import { describe, test, expect } from 'bun:test';
import { beginMorphToCanvasPlaced, beginRestoreMorph, beginMorphToDot, beginMinimizeMorph } from './morph-transaction';
import * as glyphs from './index';

describe('Tim: the new name', () => {
    test('beginMorphToCanvasPlaced is what the package exports', () => {
        expect(glyphs.beginMorphToCanvasPlaced).toBe(beginMorphToCanvasPlaced);
    });
});

describe('Spike: the old name', () => {
    // A deprecation that changes behaviour is a break wearing a warning label.
    // The old name is the same function, so anyone still on it is unaffected.
    test('beginRestoreMorph is the same function, not a copy of it', () => {
        expect(beginRestoreMorph).toBe(beginMorphToCanvasPlaced);
    });

    test('is still exported, so a consumer on 0.10.0 keeps compiling', () => {
        expect(glyphs.beginRestoreMorph).toBe(beginMorphToCanvasPlaced);
    });
});

describe('Tim: every morph names both ends', () => {
    // AXIOMAS.md: a morph is a transition between manifestations. The names now
    // say which two, in the words MANIFESTATIONS holds.
    test.each([
        ['morphDotToWindow', 'morphToWindow'],
        ['morphWindowToDot', 'morphFromWindow'],
        ['morphDotToWorkspace', 'morphToCanvas'],
        ['morphWorkspaceToDot', 'morphFromCanvas'],
        ['morphDotToPanel', 'morphToPanel'],
        ['morphPanelToDot', 'morphFromPanel'],
    ])('%s is exported, and %s is the same function', (now, before) => {
        const current = (glyphs as unknown as Record<string, unknown>)[now];
        expect(typeof current).toBe('function');
        expect((glyphs as unknown as Record<string, unknown>)[before]).toBe(current);
    });

    test('beginMinimizeMorph is beginMorphToDot', () => {
        expect(beginMinimizeMorph).toBe(beginMorphToDot);
    });
});
