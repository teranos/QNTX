/**
 * Names kept after a rename.
 *
 * Personas:
 * - Tim: the new name is the one to call
 * - Spike: the old name still works, and is the same function
 */

import { describe, test, expect } from 'bun:test';
import { beginMorphToCanvasPlaced, beginRestoreMorph } from './morph-transaction';
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
