/**
 * Composition State Management Tests
 *
 * Tests for composition persistence across page refresh.
 * Ensures melded glyphs survive browser reload.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { uiState, type CompositionState } from './ui';
import {
    addComposition,
    removeComposition,
    isGlyphInComposition,
    findCompositionByGlyph,
    getAllCompositions,
    getCompositionType
} from './compositions';

describe('Composition State Management', () => {
    beforeEach(() => {
        // Clear compositions before each test
        uiState.setCanvasCompositions([]);
    });

    describe('addComposition', () => {
        test('adds new composition to storage', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);

            const compositions = getAllCompositions();
            expect(compositions).toHaveLength(1);
            expect(compositions[0]).toEqual(comp);
        });

        test('updates existing composition', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);

            // Update position
            const updated = { ...comp, x: 150, y: 250 };
            addComposition(updated);

            const compositions = getAllCompositions();
            expect(compositions).toHaveLength(1);
            expect(compositions[0].x).toBe(150);
            expect(compositions[0].y).toBe(250);
        });
    });

    describe('removeComposition', () => {
        test('removes composition from storage', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(getAllCompositions()).toHaveLength(1);

            removeComposition(comp.id);
            expect(getAllCompositions()).toHaveLength(0);
        });

        test('handles removing non-existent composition', () => {
            removeComposition('does-not-exist');
            expect(getAllCompositions()).toHaveLength(0);
        });
    });

    describe('isGlyphInComposition', () => {
        test('returns true when glyph is initiator', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(isGlyphInComposition('ax1')).toBe(true);
        });

        test('returns true when glyph is target', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(isGlyphInComposition('prompt1')).toBe(true);
        });

        test('returns false when glyph is not in any composition', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(isGlyphInComposition('ax2')).toBe(false);
        });
    });

    describe('findCompositionByGlyph', () => {
        test('finds composition by initiator id', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            const found = findCompositionByGlyph('ax1');
            expect(found).toEqual(comp);
        });

        test('finds composition by target id', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            const found = findCompositionByGlyph('prompt1');
            expect(found).toEqual(comp);
        });

        test('returns null when glyph not in any composition', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            addComposition(comp);
            const found = findCompositionByGlyph('ax2');
            expect(found).toBe(null);
        });
    });

    describe('getCompositionType', () => {
        test('detects ax-prompt composition', () => {
            const initiator = document.createElement('div');
            initiator.className = 'canvas-ax-glyph';
            const target = document.createElement('div');
            target.className = 'canvas-prompt-glyph';

            const type = getCompositionType(initiator, target);
            expect(type).toBe('ax-prompt');
        });

        test('detects ax-py composition', () => {
            const initiator = document.createElement('div');
            initiator.className = 'canvas-ax-glyph';
            const target = document.createElement('div');
            target.className = 'canvas-py-glyph';

            const type = getCompositionType(initiator, target);
            expect(type).toBe('ax-py');
        });

        test('detects py-prompt composition', () => {
            const initiator = document.createElement('div');
            initiator.className = 'canvas-py-glyph';
            const target = document.createElement('div');
            target.className = 'canvas-prompt-glyph';

            const type = getCompositionType(initiator, target);
            expect(type).toBe('py-prompt');
        });

        test('returns null for incompatible glyphs', () => {
            const initiator = document.createElement('div');
            initiator.className = 'canvas-prompt-glyph';
            const target = document.createElement('div');
            target.className = 'canvas-prompt-glyph';

            const type = getCompositionType(initiator, target);
            expect(type).toBe(null);
        });
    });

    describe('persistence integration', () => {
        test('compositions persist through state updates', () => {
            const comp1: CompositionState = {
                id: 'melded-ax1-prompt1',
                type: 'ax-prompt',
                initiatorId: 'ax1',
                targetId: 'prompt1',
                x: 100,
                y: 200
            };

            const comp2: CompositionState = {
                id: 'melded-py1-prompt2',
                type: 'py-prompt',
                initiatorId: 'py1',
                targetId: 'prompt2',
                x: 300,
                y: 400
            };

            addComposition(comp1);
            addComposition(comp2);

            const compositions = getAllCompositions();
            expect(compositions).toHaveLength(2);
            expect(compositions).toContainEqual(comp1);
            expect(compositions).toContainEqual(comp2);
        });
    });
});
