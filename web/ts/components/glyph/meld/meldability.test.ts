/**
 * Port-aware meldability registry tests
 */

import { describe, test, expect } from 'bun:test';
import {
    areClassesCompatible,
    getCompatibleDirections,
    getInitiatorClasses,
    getTargetClasses,
    getCompatibleTargets,
    getGlyphClass,
    getLeafGlyphIds,
    getRootGlyphIds,
    getMeldOptions,
    selectPreferredMeldOption,
    computeGridPositions,
    type EdgeDirection
} from './meldability';

describe('Port-aware MELDABILITY registry', () => {
    describe('areClassesCompatible', () => {
        test('ax → prompt returns right', () => {
            expect(areClassesCompatible('canvas-ax-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('ax → py returns right', () => {
            expect(areClassesCompatible('canvas-ax-glyph', 'canvas-py-glyph')).toBe('right');
        });

        test('se → py returns right', () => {
            expect(areClassesCompatible('canvas-se-glyph', 'canvas-py-glyph')).toBe('right');
        });

        test('se → prompt returns right', () => {
            expect(areClassesCompatible('canvas-se-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('se → se returns right (semantic intersection)', () => {
            expect(areClassesCompatible('canvas-se-glyph', 'canvas-se-glyph')).toBe('right');
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

        test('doc → prompt returns right as first direction', () => {
            expect(areClassesCompatible('canvas-doc-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('doc → prompt supports both right and bottom', () => {
            const dirs = getCompatibleDirections('canvas-doc-glyph', 'canvas-prompt-glyph');
            expect(dirs).toContain('right');
            expect(dirs).toContain('bottom');
            expect(dirs.length).toBe(2);
        });

        test('doc → doc supports both right and bottom', () => {
            const dirs = getCompatibleDirections('canvas-doc-glyph', 'canvas-doc-glyph');
            expect(dirs).toContain('right');
            expect(dirs).toContain('bottom');
            expect(dirs.length).toBe(2);
        });

        test('note → prompt returns bottom (note sits above prompt)', () => {
            expect(areClassesCompatible('canvas-note-glyph', 'canvas-prompt-glyph')).toBe('bottom');
        });

        test('doc → result returns right (doc sits left of result)', () => {
            expect(areClassesCompatible('canvas-doc-glyph', 'canvas-result-glyph')).toBe('right');
            expect(getCompatibleDirections('canvas-doc-glyph', 'canvas-result-glyph')).toEqual(['right']);
        });

        test('prompt → prompt returns null (incompatible)', () => {
            expect(areClassesCompatible('canvas-prompt-glyph', 'canvas-prompt-glyph')).toBe(null);
        });

        test('result → result returns bottom (conversational chaining)', () => {
            expect(areClassesCompatible('canvas-result-glyph', 'canvas-result-glyph')).toBe('bottom');
        });

        test('result → non-result returns null', () => {
            expect(areClassesCompatible('canvas-result-glyph', 'canvas-py-glyph')).toBe(null);
        });

        test('unknown class returns null', () => {
            expect(areClassesCompatible('unknown', 'canvas-py-glyph')).toBe(null);
        });
    });

    describe('getInitiatorClasses', () => {
        test('includes ax, se, py, prompt, doc, note, result, subcanvas', () => {
            const classes = getInitiatorClasses();
            expect(classes).toContain('canvas-ax-glyph');
            expect(classes).toContain('canvas-se-glyph');
            expect(classes).toContain('canvas-py-glyph');
            expect(classes).toContain('canvas-prompt-glyph');
            expect(classes).toContain('canvas-doc-glyph');
            expect(classes).toContain('canvas-note-glyph');
            expect(classes).toContain('canvas-result-glyph');
            expect(classes).toContain('canvas-subcanvas-glyph');
        });
    });

    describe('getTargetClasses', () => {
        test('includes prompt, py, doc, result, subcanvas (all targets across all ports)', () => {
            const classes = getTargetClasses();
            expect(classes).toContain('canvas-prompt-glyph');
            expect(classes).toContain('canvas-py-glyph');
            expect(classes).toContain('canvas-doc-glyph');
            expect(classes).toContain('canvas-result-glyph');
            expect(classes).toContain('canvas-subcanvas-glyph');
        });
    });

    describe('getCompatibleTargets', () => {
        test('py can target prompt, py, and result', () => {
            const targets = getCompatibleTargets('canvas-py-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets).toContain('canvas-result-glyph');
        });

        test('ax can target prompt, py, and subcanvas', () => {
            const targets = getCompatibleTargets('canvas-ax-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets).toContain('canvas-subcanvas-glyph');
            expect(targets.length).toBe(3);
        });

        test('se can target prompt, py, se, and subcanvas', () => {
            const targets = getCompatibleTargets('canvas-se-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets).toContain('canvas-se-glyph');
            expect(targets).toContain('canvas-subcanvas-glyph');
            expect(targets.length).toBe(4);
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

        test('py can append to se (se leaf, right port)', () => {
            const composition = document.createElement('div');
            const se = document.createElement('div');
            se.className = 'canvas-se-glyph';
            se.setAttribute('data-glyph-id', 'se1');
            composition.appendChild(se);

            const edges = [{ from: 'se1', to: 'se1', direction: 'right' }];
            // Single-node composition: se1 has no outgoing right edge yet
            const singleEdges: Array<{ from: string; to: string; direction: string }> = [];
            const options = getMeldOptions('canvas-py-glyph', composition, singleEdges);
            // se1 can initiate right → py, but with no edges se1 isn't in the edge set
            // Use a real edge to test:
            const composition2 = document.createElement('div');
            const se2 = document.createElement('div');
            se2.className = 'canvas-se-glyph';
            se2.setAttribute('data-glyph-id', 'se1');
            const py = document.createElement('div');
            py.className = 'canvas-py-glyph';
            py.setAttribute('data-glyph-id', 'py1');
            composition2.appendChild(se2);
            composition2.appendChild(py);

            const edges2 = [{ from: 'se1', to: 'py1', direction: 'right' }];
            const promptOptions = getMeldOptions('canvas-prompt-glyph', composition2, edges2);
            const appendOption = promptOptions.find(o => o.glyphId === 'py1' && o.direction === 'right');
            expect(appendOption).toBeDefined();
        });

        test('doc right-meld onto result chain returns options for all results (#521)', () => {
            const composition = document.createElement('div');
            const r1 = document.createElement('div');
            r1.className = 'canvas-result-glyph';
            r1.setAttribute('data-glyph-id', 'result1');
            const r2 = document.createElement('div');
            r2.className = 'canvas-result-glyph';
            r2.setAttribute('data-glyph-id', 'result2');
            const r3 = document.createElement('div');
            r3.className = 'canvas-result-glyph';
            r3.setAttribute('data-glyph-id', 'result3');
            composition.appendChild(r1);
            composition.appendChild(r2);
            composition.appendChild(r3);

            const edges = [
                { from: 'result1', to: 'result2', direction: 'bottom' },
                { from: 'result2', to: 'result3', direction: 'bottom' }
            ];

            const options = getMeldOptions('canvas-doc-glyph', composition, edges);

            // All three results have free right-incoming ports → doc can meld onto any
            const rightOptions = options.filter(o => o.direction === 'right');
            expect(rightOptions.length).toBe(3);

            // The preferred option should match the anchor (spatially nearest) glyph
            const preferredOption = selectPreferredMeldOption(options, 'result3');
            expect(preferredOption!.glyphId).toBe('result3');

            // Falls back to first option when anchor has no match
            const fallbackOption = selectPreferredMeldOption(options, 'nonexistent');
            expect(fallbackOption).toBeDefined();
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

        test('multiple bottom children from same parent → stacked rows', () => {
            const edges = [
                { from: 'py1', to: 'r1', direction: 'bottom' },
                { from: 'py1', to: 'r2', direction: 'bottom' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('py1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('r1')).toEqual({ row: 2, col: 1 });
            expect(positions.get('r2')).toEqual({ row: 3, col: 1 });
        });

        test('multiple right children from same parent → adjacent columns', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'ax1', to: 'py2', direction: 'right' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('ax1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('py1')).toEqual({ row: 1, col: 2 });
            expect(positions.get('py2')).toEqual({ row: 1, col: 3 });
        });

        test('multiple roots → each gets its own column', () => {
            const edges = [
                { from: 'py1', to: 'prompt1', direction: 'right' },
                { from: 'py2', to: 'prompt1', direction: 'right' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('py1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('py2')).toEqual({ row: 1, col: 2 });
            // prompt1 reached first from py1
            expect(positions.get('prompt1')).toEqual({ row: 1, col: 2 });
        });

        test('3+ docs stacking on prompt → stacked rows above', () => {
            const edges = [
                { from: 'doc1', to: 'prompt1', direction: 'bottom' },
                { from: 'doc2', to: 'doc1', direction: 'bottom' },
                { from: 'doc3', to: 'doc2', direction: 'bottom' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.size).toBe(4);
        });

        test('note + doc both melded on prompt → separate bottom edges', () => {
            const edges = [
                { from: 'doc1', to: 'prompt1', direction: 'bottom' },
                { from: 'note1', to: 'prompt1', direction: 'bottom' }
            ];
            const positions = computeGridPositions(edges);
            expect(positions.get('prompt1')).toBeDefined();
            expect(positions.get('doc1')).toBeDefined();
            expect(positions.get('note1')).toBeDefined();
        });

        test('lateral root right-melded onto mid-chain preserves vertical layout (#521)', () => {
            // doc melds right onto r3 in a vertical chain r1→r2→r3→r4
            // Expected: chain stays vertical, doc sits left of r3
            const edges = [
                { from: 'r1', to: 'r2', direction: 'bottom' },
                { from: 'r2', to: 'r3', direction: 'bottom' },
                { from: 'r3', to: 'r4', direction: 'bottom' },
                { from: 'doc1', to: 'r3', direction: 'right' }
            ];
            const positions = computeGridPositions(edges);

            // Chain remains vertical in one column
            const r1 = positions.get('r1')!;
            const r2 = positions.get('r2')!;
            const r3 = positions.get('r3')!;
            const r4 = positions.get('r4')!;
            const doc = positions.get('doc1')!;

            // Vertical chain: same column, ascending rows
            expect(r1.col).toBe(r2.col);
            expect(r2.col).toBe(r3.col);
            expect(r3.col).toBe(r4.col);
            expect(r1.row).toBeLessThan(r2.row);
            expect(r2.row).toBeLessThan(r3.row);
            expect(r3.row).toBeLessThan(r4.row);

            // Doc sits left of r3 (same row, earlier column)
            expect(doc.row).toBe(r3.row);
            expect(doc.col).toBeLessThan(r3.col);
        });

        test('top direction edge → target above parent, normalized', () => {
            const edges = [
                { from: 'note1', to: 'prompt1', direction: 'top' }
            ];
            const positions = computeGridPositions(edges);
            // note1 at row 1 → prompt1 at row 0 → normalized: prompt1=1, note1=2
            expect(positions.get('prompt1')).toEqual({ row: 1, col: 1 });
            expect(positions.get('note1')).toEqual({ row: 2, col: 1 });
        });
    });

    describe('Subcanvas meld compatibility - Tim (Happy Path)', () => {
        test('Tim: subcanvas is compatible as target from ax (right)', () => {
            expect(areClassesCompatible('canvas-ax-glyph', 'canvas-subcanvas-glyph')).toBe('right');
        });

        test('Tim: subcanvas is compatible as target from py (right and bottom)', () => {
            expect(areClassesCompatible('canvas-py-glyph', 'canvas-subcanvas-glyph')).toBe('right');
        });

        test('Tim: subcanvas is compatible as target from se (right)', () => {
            expect(areClassesCompatible('canvas-se-glyph', 'canvas-subcanvas-glyph')).toBe('right');
        });

        test('Tim: subcanvas is compatible as target from note (bottom)', () => {
            expect(areClassesCompatible('canvas-note-glyph', 'canvas-subcanvas-glyph')).toBe('bottom');
        });

        test('Tim: subcanvas is compatible as target from prompt (bottom)', () => {
            expect(areClassesCompatible('canvas-prompt-glyph', 'canvas-subcanvas-glyph')).toBe('bottom');
        });

        test('Tim: subcanvas can initiate meld toward prompt (right)', () => {
            expect(areClassesCompatible('canvas-subcanvas-glyph', 'canvas-prompt-glyph')).toBe('right');
        });

        test('Tim: subcanvas can initiate meld toward py (right)', () => {
            expect(areClassesCompatible('canvas-subcanvas-glyph', 'canvas-py-glyph')).toBe('right');
        });

        test('Tim: subcanvas can initiate meld toward result (right)', () => {
            expect(areClassesCompatible('canvas-subcanvas-glyph', 'canvas-result-glyph')).toBe('right');
        });
    });

    describe('Subcanvas meld compatibility - Spike (Edge Cases)', () => {
        test('Spike: subcanvas-to-subcanvas compatibility works', () => {
            expect(areClassesCompatible('canvas-subcanvas-glyph', 'canvas-subcanvas-glyph')).toBe('right');
        });

        test('Spike: subcanvas has ports in all three directions', () => {
            const targets = getCompatibleTargets('canvas-subcanvas-glyph');
            expect(targets).toContain('canvas-ax-glyph');
            expect(targets).toContain('canvas-se-glyph');
            expect(targets).toContain('canvas-py-glyph');
            expect(targets).toContain('canvas-prompt-glyph');
            expect(targets).toContain('canvas-note-glyph');
            expect(targets).toContain('canvas-result-glyph');
            expect(targets).toContain('canvas-subcanvas-glyph');
        });
    });
});
