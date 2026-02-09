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
    getAllCompositions
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(isGlyphInComposition('ax1')).toBe(true);
        });

        test('returns true when glyph is target', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
                x: 100,
                y: 200
            };

            addComposition(comp);
            expect(isGlyphInComposition('prompt1')).toBe(true);
        });

        test('returns false when glyph is not in any composition', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-prompt1',
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
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
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
                x: 100,
                y: 200
            };

            addComposition(comp);
            const found = findCompositionByGlyph('ax2');
            expect(found).toBe(null);
        });
    });

    describe('persistence integration', () => {
        test('compositions persist through state updates', () => {
            const comp1: CompositionState = {
                id: 'melded-ax1-prompt1',
                edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }],
                x: 100,
                y: 200
            };

            const comp2: CompositionState = {
                id: 'melded-py1-prompt2',
                edges: [{ from: 'py1', to: 'prompt2', direction: 'right', position: 0 }],
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

    // Phase 2: Multi-glyph chain state management
    describe('Multi-glyph chains', () => {
        test('3-glyph composition stores correctly', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-py1-prompt1',
                edges: [
                    { from: 'ax1', to: 'py1', direction: 'right', position: 0 },
                    { from: 'py1', to: 'prompt1', direction: 'right', position: 1 }
                ],
                x: 100,
                y: 200
            };

            addComposition(comp);

            const compositions = getAllCompositions();
            expect(compositions).toHaveLength(1);
            expect(compositions[0].edges).toHaveLength(2);
        });

        test('isGlyphInComposition works with 3-glyph chains', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-py1-prompt1',
                edges: [
                    { from: 'ax1', to: 'py1', direction: 'right', position: 0 },
                    { from: 'py1', to: 'prompt1', direction: 'right', position: 1 }
                ],
                x: 100,
                y: 200
            };

            addComposition(comp);

            expect(isGlyphInComposition('ax1')).toBe(true);
            expect(isGlyphInComposition('py1')).toBe(true);
            expect(isGlyphInComposition('prompt1')).toBe(true);
            expect(isGlyphInComposition('ax2')).toBe(false);
        });

        test('findCompositionByGlyph finds 3-glyph chains', () => {
            const comp: CompositionState = {
                id: 'melded-ax1-py1-prompt1',
                edges: [
                    { from: 'ax1', to: 'py1', direction: 'right', position: 0 },
                    { from: 'py1', to: 'prompt1', direction: 'right', position: 1 }
                ],
                x: 100,
                y: 200
            };

            addComposition(comp);

            expect(findCompositionByGlyph('ax1')).toEqual(comp);
            expect(findCompositionByGlyph('py1')).toEqual(comp);
            expect(findCompositionByGlyph('prompt1')).toEqual(comp);
        });

        test('extending composition adds edge to graph', () => {
            // Start with 2-glyph composition
            const comp: CompositionState = {
                id: 'melded-ax1-py1',
                edges: [{ from: 'ax1', to: 'py1', direction: 'right', position: 0 }],
                x: 100,
                y: 200
            };

            addComposition(comp);

            // Extend to 3 glyphs (this functionality needs implementation in Phase 3)
            const extended: CompositionState = {
                ...comp,
                edges: [
                    ...comp.edges,
                    { from: 'py1', to: 'prompt1', direction: 'right', position: 1 }
                ]
            };

            addComposition(extended);

            const result = findCompositionByGlyph('prompt1');
            expect(result?.edges).toHaveLength(2);
        });
    });
});
