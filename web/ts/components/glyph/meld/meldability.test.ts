/**
 * Port-aware meldability registry tests
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import {
    areClassesCompatible,
    getInitiatorClasses,
    getTargetClasses,
    getCompatibleTargets,
    getGlyphClass,
    getLeafGlyphIds,
    getRootGlyphIds,
    getMeldOptions,
    computeGridPositions,
    type EdgeDirection
} from './meldability';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

describe('Port-aware MELDABILITY registry', () => {
    describe('areClassesCompatible', () => {
        test('ax → prompt returns right', () => {
            expect(areClassesCompatible('canvas-ax-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('ax → py returns right', () => {
            expect(areClassesCompatible('canvas-ax-glyph', 'canvas-py-glyph')).toBe('right');
        });

        test('py → prompt returns right', () => {
            expect(areClassesCompatible('canvas-py-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('py → py returns right', () => {
            expect(areClassesCompatible('canvas-py-glyph', 'canvas-py-glyph')).toBe('right');
        });

        test('py → result returns bottom', () => {
            expect(areClassesCompatible('canvas-py-glyph', 'canvas-result-glyph')).toBe('bottom');
        });

        test('prompt → result returns bottom', () => {
            expect(areClassesCompatible('canvas-prompt-glyph', 'canvas-result-glyph')).toBe('bottom');
        });

        test('note → prompt returns bottom (note sits above prompt)', () => {
            expect(areClassesCompatible('canvas-note-glyph', 'canvas-prompt-glyph')).toBe('bottom');
        });

        test('prompt → prompt returns null (incompatible)', () => {
            expect(areClassesCompatible('canvas-prompt-glyph', 'canvas-prompt-glyph')).toBe(null);
        });

        test('result → anything returns null (result has no output ports)', () => {
            expect(areClassesCompatible('canvas-result-glyph', 'canvas-py-glyph')).toBe(null);
        });

        test('unknown class returns null', () => {
            expect(areClassesCompatible('unknown', 'canvas-py-glyph')).toBe(null);
        });
    });

    describe('getInitiatorClasses', () => {
        test('includes ax, py, prompt, note', () => {
            const classes = getInitiatorClasses();
            expect(classes).toContain('canvas-ax-glyph');
            expect(classes).toContain('canvas-py-glyph');
            expect(classes).toContain('canvas-prompt-glyph');
            expect(classes).toContain('canvas-note-glyph');
        });
    });

    describe('getTargetClasses', () => {
        test('includes prompt, py, result (all targets across all ports)', () => {
            const classes = getTargetClasses();
            expect(classes).toContain('canvas-prompt-glyph');
            expect(classes).toContain('canvas-py-glyph');
            expect(classes).toContain('canvas-result-glyph');
        });
    });

    describe('getCompatibleTargets', () => {
        test('py can target prompt, py, and result', () => {
            const targets = getCompatibleTargets('canvas-py-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets).toContain('canvas-result-glyph');
        });

        test('ax can target prompt and py', () => {
            const targets = getCompatibleTargets('canvas-ax-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets.length).toBe(2);
        });

        test('unknown class returns empty', () => {
            expect(getCompatibleTargets('unknown')).toEqual([]);
        });
    });

    describe('getGlyphClass', () => {
        test('extracts glyph class from element', () => {
            const el = document.createElement('div');
            el.className = 'canvas-py-glyph canvas-glyph extra-class';
            expect(getGlyphClass(el)).toBe('canvas-py-glyph');
        });

        test('returns null when no glyph class found', () => {
            const el = document.createElement('div');
            el.className = 'some-other-class';
            expect(getGlyphClass(el)).toBe(null);
        });
    });

    describe('getLeafGlyphIds', () => {
        test('finds leaf in simple chain', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'prompt1', direction: 'right' }
            ];
            expect(getLeafGlyphIds(edges)).toEqual(['prompt1']);
        });

        test('finds multiple leaves in fan-out', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'ax1', to: 'py2', direction: 'right' }
            ];
            const leaves = getLeafGlyphIds(edges);
            expect(leaves).toContain('py1');
            expect(leaves).toContain('py2');
            expect(leaves.length).toBe(2);
        });

        test('single edge: leaf is the to node', () => {
            const edges = [{ from: 'ax1', to: 'prompt1', direction: 'right' }];
            expect(getLeafGlyphIds(edges)).toEqual(['prompt1']);
        });
    });

    describe('getRootGlyphIds', () => {
        test('finds root in simple chain', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'prompt1', direction: 'right' }
            ];
            expect(getRootGlyphIds(edges)).toEqual(['ax1']);
        });

        test('finds multiple roots in fan-in', () => {
            const edges = [
                { from: 'py1', to: 'prompt1', direction: 'right' },
                { from: 'py2', to: 'prompt1', direction: 'right' }
            ];
            const roots = getRootGlyphIds(edges);
            expect(roots).toContain('py1');
            expect(roots).toContain('py2');
            expect(roots.length).toBe(2);
        });
    });

    describe('getMeldOptions', () => {
        test('prompt can append to ax-py composition (py leaf, right port)', () => {
            const composition = document.createElement('div');
            const ax = document.createElement('div');
            ax.className = 'canvas-ax-glyph';
            ax.setAttribute('data-glyph-id', 'ax1');
            const py = document.createElement('div');
            py.className = 'canvas-py-glyph';
            py.setAttribute('data-glyph-id', 'py1');
            composition.appendChild(ax);
            composition.appendChild(py);

            const edges = [{ from: 'ax1', to: 'py1', direction: 'right' }];

            const options = getMeldOptions('canvas-prompt-glyph', composition, edges);
            expect(options.length).toBeGreaterThan(0);

            const appendOption = options.find(o => o.incomingRole === 'to');
            expect(appendOption).toBeDefined();
            expect(appendOption!.glyphId).toBe('py1');
            expect(appendOption!.direction).toBe('right');
        });

        test('ax can prepend to py-prompt composition (py root, right port)', () => {
            const composition = document.createElement('div');
            const py = document.createElement('div');
            py.className = 'canvas-py-glyph';
            py.setAttribute('data-glyph-id', 'py1');
            const prompt = document.createElement('div');
            prompt.className = 'canvas-prompt-glyph';
            prompt.setAttribute('data-glyph-id', 'prompt1');
            composition.appendChild(py);
            composition.appendChild(prompt);

            const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

            const options = getMeldOptions('canvas-ax-glyph', composition, edges);

            const prependOption = options.find(o => o.incomingRole === 'from');
            expect(prependOption).toBeDefined();
            expect(prependOption!.glyphId).toBe('py1');
            expect(prependOption!.direction).toBe('right');
        });

        test('result can attach below py leaf (bottom port)', () => {
            const composition = document.createElement('div');
            const ax = document.createElement('div');
            ax.className = 'canvas-ax-glyph';
            ax.setAttribute('data-glyph-id', 'ax1');
            const py = document.createElement('div');
            py.className = 'canvas-py-glyph';
            py.setAttribute('data-glyph-id', 'py1');
            composition.appendChild(ax);
            composition.appendChild(py);

            const edges = [{ from: 'ax1', to: 'py1', direction: 'right' }];

            const options = getMeldOptions('canvas-result-glyph', composition, edges);

            const bottomOption = options.find(o => o.direction === 'bottom');
            expect(bottomOption).toBeDefined();
            expect(bottomOption!.glyphId).toBe('py1');
            expect(bottomOption!.incomingRole).toBe('to');
        });

        test('incompatible glyph returns no options', () => {
            const composition = document.createElement('div');
            const ax = document.createElement('div');
            ax.className = 'canvas-ax-glyph';
            ax.setAttribute('data-glyph-id', 'ax1');
            composition.appendChild(ax);

            const edges = [{ from: 'ax1', to: 'py1', direction: 'right' }];

            // ix glyph has no meld compatibility
            const options = getMeldOptions('canvas-ix-glyph', composition, edges);
            expect(options).toEqual([]);
        });
    });

    describe('computeGridPositions', () => {
        test('single right edge → row 1, cols 1-2', () => {
            const edges = [{ from: 'ax1', to: 'py1', direction: 'right' }];
            const positions = computeGridPositions(edges);
            expect(positions.get('ax1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('py1')).toEqual({ row: 1, col: 2 });
        });

        test('single bottom edge → col 1, rows 1-2', () => {
            const edges = [{ from: 'py1', to: 'result1', direction: 'bottom' }];
            const positions = computeGridPositions(edges);
            expect(positions.get('py1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('result1')).toEqual({ row: 2, col: 1 });
        });

        test('mixed right+bottom → ax{1,1} py{1,2} result{2,2}', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'result1', direction: 'bottom' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('ax1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('py1')).toEqual({ row: 1, col: 2 });
            expect(positions.get('result1')).toEqual({ row: 2, col: 2 });
        });

        test('chain ax→py→prompt with py→result → 4 positions on 2D grid', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'prompt1', direction: 'right' },
                { from: 'py1', to: 'result1', direction: 'bottom' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('ax1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('py1')).toEqual({ row: 1, col: 2 });
            expect(positions.get('prompt1')).toEqual({ row: 1, col: 3 });
            expect(positions.get('result1')).toEqual({ row: 2, col: 2 });
        });

        test('empty edges → empty map', () => {
            expect(computeGridPositions([]).size).toBe(0);
        });
    });
});
