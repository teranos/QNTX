import { describe, test, expect } from 'bun:test';
import {
    getRootGlyphIds,
    getLeafGlyphIds,
    isPortFree,
    isConnectedGraph,
    computeGridPositions,
} from './edge-graph';

// ── Tim (happy path) ────────────────────────────────────────────────

describe('Tim: edge-graph basics', () => {
    describe('getRootGlyphIds', () => {
        test('single chain: first node is root', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'prompt1', direction: 'right' },
            ];
            expect(getRootGlyphIds(edges)).toEqual(['ax1']);
        });

        test('fan-in: multiple roots converge', () => {
            const edges = [
                { from: 'py1', to: 'prompt1', direction: 'right' },
                { from: 'py2', to: 'prompt1', direction: 'right' },
            ];
            const roots = getRootGlyphIds(edges);
            expect(roots).toContain('py1');
            expect(roots).toContain('py2');
            expect(roots.length).toBe(2);
        });

        test('single edge', () => {
            const edges = [{ from: 'a', to: 'b', direction: 'right' }];
            expect(getRootGlyphIds(edges)).toEqual(['a']);
        });
    });

    describe('getLeafGlyphIds', () => {
        test('single chain: last node is leaf', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'py1', to: 'prompt1', direction: 'right' },
            ];
            expect(getLeafGlyphIds(edges)).toEqual(['prompt1']);
        });

        test('fan-out: multiple leaves', () => {
            const edges = [
                { from: 'ax1', to: 'py1', direction: 'right' },
                { from: 'ax1', to: 'py2', direction: 'bottom' },
            ];
            const leaves = getLeafGlyphIds(edges);
            expect(leaves).toContain('py1');
            expect(leaves).toContain('py2');
            expect(leaves.length).toBe(2);
        });
    });

    describe('isPortFree', () => {
        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right' },
            { from: 'py1', to: 'result1', direction: 'bottom' },
        ];

        test('occupied outgoing port', () => {
            expect(isPortFree('ax1', 'right', 'outgoing', edges)).toBe(false);
        });

        test('occupied incoming port', () => {
            expect(isPortFree('py1', 'right', 'incoming', edges)).toBe(false);
        });

        test('free outgoing port on different direction', () => {
            expect(isPortFree('ax1', 'bottom', 'outgoing', edges)).toBe(true);
        });

        test('free incoming port on different direction', () => {
            // py1 has incoming on 'right' (from ax1), bottom incoming is free
            expect(isPortFree('py1', 'bottom', 'incoming', edges)).toBe(true);
            expect(isPortFree('py1', 'top', 'incoming', edges)).toBe(true);
        });

        test('node not in edges at all', () => {
            expect(isPortFree('unknown', 'right', 'outgoing', edges)).toBe(true);
            expect(isPortFree('unknown', 'right', 'incoming', edges)).toBe(true);
        });

        test('empty edges: all ports free', () => {
            expect(isPortFree('ax1', 'right', 'outgoing', [])).toBe(true);
        });
    });

    describe('isConnectedGraph', () => {
        test('simple chain is connected', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'b', to: 'c', direction: 'right' },
            ];
            expect(isConnectedGraph(edges)).toBe(true);
        });

        test('single edge is connected', () => {
            expect(isConnectedGraph([
                { from: 'a', to: 'b', direction: 'right' },
            ])).toBe(true);
        });

        test('fan-out is connected', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'a', to: 'c', direction: 'bottom' },
            ];
            expect(isConnectedGraph(edges)).toBe(true);
        });

        test('fan-in is connected', () => {
            const edges = [
                { from: 'a', to: 'c', direction: 'right' },
                { from: 'b', to: 'c', direction: 'right' },
            ];
            expect(isConnectedGraph(edges)).toBe(true);
        });

        test('empty edges: not connected', () => {
            expect(isConnectedGraph([])).toBe(false);
        });
    });

    describe('computeGridPositions', () => {
        test('empty edges: empty map', () => {
            expect(computeGridPositions([]).size).toBe(0);
        });

        test('single right edge: same row, adjacent cols', () => {
            const edges = [{ from: 'a', to: 'b', direction: 'right' }];
            const pos = computeGridPositions(edges);
            expect(pos.get('a')).toEqual({ row: 1, col: 1 });
            expect(pos.get('b')).toEqual({ row: 1, col: 2 });
        });

        test('single bottom edge: same col, adjacent rows', () => {
            const edges = [{ from: 'a', to: 'b', direction: 'bottom' }];
            const pos = computeGridPositions(edges);
            expect(pos.get('a')).toEqual({ row: 1, col: 1 });
            expect(pos.get('b')).toEqual({ row: 2, col: 1 });
        });

        test('right chain: all same row, incrementing cols', () => {
            const edges = [
                { from: 'ax', to: 'py', direction: 'right' },
                { from: 'py', to: 'prompt', direction: 'right' },
            ];
            const pos = computeGridPositions(edges);
            expect(pos.get('ax')).toEqual({ row: 1, col: 1 });
            expect(pos.get('py')).toEqual({ row: 1, col: 2 });
            expect(pos.get('prompt')).toEqual({ row: 1, col: 3 });
        });
    });
});

// ── Spike (edge cases) ──────────────────────────────────────────────

describe('Spike: edge-graph edge cases', () => {
    describe('isPortFree', () => {
        test('same glyph, same direction, different roles are independent', () => {
            const edges = [{ from: 'a', to: 'b', direction: 'right' }];
            // a has outgoing right occupied, but incoming right is free
            expect(isPortFree('a', 'right', 'outgoing', edges)).toBe(false);
            expect(isPortFree('a', 'right', 'incoming', edges)).toBe(true);
            // b has incoming right occupied, but outgoing right is free
            expect(isPortFree('b', 'right', 'incoming', edges)).toBe(false);
            expect(isPortFree('b', 'right', 'outgoing', edges)).toBe(true);
        });
    });

    describe('isConnectedGraph', () => {
        test('two disconnected pairs', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'c', to: 'd', direction: 'right' },
            ];
            expect(isConnectedGraph(edges)).toBe(false);
        });

        test('three nodes, one disconnected', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'c', to: 'd', direction: 'right' },
                { from: 'd', to: 'e', direction: 'right' },
            ];
            expect(isConnectedGraph(edges)).toBe(false);
        });

        test('bridged graph becomes connected', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'c', to: 'd', direction: 'right' },
                { from: 'b', to: 'c', direction: 'bottom' },
            ];
            expect(isConnectedGraph(edges)).toBe(true);
        });
    });

    describe('computeGridPositions', () => {
        test('mixed right + bottom: L-shape', () => {
            const edges = [
                { from: 'ax', to: 'py', direction: 'right' },
                { from: 'py', to: 'result', direction: 'bottom' },
            ];
            const pos = computeGridPositions(edges);
            expect(pos.get('ax')).toEqual({ row: 1, col: 1 });
            expect(pos.get('py')).toEqual({ row: 1, col: 2 });
            expect(pos.get('result')).toEqual({ row: 2, col: 2 });
        });

        test('top direction normalizes to row 1', () => {
            const edges = [{ from: 'a', to: 'b', direction: 'top' }];
            const pos = computeGridPositions(edges);
            // b goes above a, normalization shifts both to start at row 1
            expect(pos.get('b')!.row).toBeLessThan(pos.get('a')!.row);
            expect(Math.min(pos.get('a')!.row, pos.get('b')!.row)).toBe(1);
        });

        test('all nodes get positions', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'b', to: 'c', direction: 'right' },
                { from: 'b', to: 'd', direction: 'bottom' },
            ];
            const pos = computeGridPositions(edges);
            expect(pos.size).toBe(4);
            expect(pos.has('a')).toBe(true);
            expect(pos.has('b')).toBe(true);
            expect(pos.has('c')).toBe(true);
            expect(pos.has('d')).toBe(true);
        });

        test('fan-out produces distinct positions per branch', () => {
            const edges = [
                { from: 'a', to: 'b', direction: 'right' },
                { from: 'a', to: 'c', direction: 'bottom' },
            ];
            const pos = computeGridPositions(edges);
            expect(pos.size).toBe(3);
            // b goes right, c goes down — different cells
            const bPos = pos.get('b')!;
            const cPos = pos.get('c')!;
            expect(bPos.row === cPos.row && bPos.col === cPos.col).toBe(false);
        });
    });
});

// ── Jenny (complex scenarios) ───────────────────────────────────────

describe('Jenny: composition-shaped graphs', () => {
    test('ax → py → prompt chain with result below py', () => {
        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right' },
            { from: 'py1', to: 'prompt1', direction: 'right' },
            { from: 'py1', to: 'result1', direction: 'bottom' },
        ];

        expect(getRootGlyphIds(edges)).toEqual(['ax1']);
        expect(getLeafGlyphIds(edges)).toContain('prompt1');
        expect(getLeafGlyphIds(edges)).toContain('result1');

        // py1's right outgoing is taken (→ prompt1)
        expect(isPortFree('py1', 'right', 'outgoing', edges)).toBe(false);
        // py1's bottom outgoing is taken (→ result1)
        expect(isPortFree('py1', 'bottom', 'outgoing', edges)).toBe(false);
        // prompt1's right outgoing is free
        expect(isPortFree('prompt1', 'right', 'outgoing', edges)).toBe(true);

        expect(isConnectedGraph(edges)).toBe(true);

        const pos = computeGridPositions(edges);
        // ax1 and py1 same row, prompt1 same row as py1, result1 below py1
        expect(pos.get('ax1')!.row).toBe(pos.get('py1')!.row);
        expect(pos.get('py1')!.row).toBe(pos.get('prompt1')!.row);
        expect(pos.get('result1')!.row).toBeGreaterThan(pos.get('py1')!.row);
        expect(pos.get('result1')!.col).toBe(pos.get('py1')!.col);
    });

    test('two independent chains become disconnected after detach', () => {
        // Full graph: a → b → c → d
        const full = [
            { from: 'a', to: 'b', direction: 'right' },
            { from: 'b', to: 'c', direction: 'right' },
            { from: 'c', to: 'd', direction: 'right' },
        ];
        expect(isConnectedGraph(full)).toBe(true);

        // Remove middle edge (b → c): leaves {a,b} and {c,d} disconnected
        const afterDetach = [
            { from: 'a', to: 'b', direction: 'right' },
            { from: 'c', to: 'd', direction: 'right' },
        ];
        expect(isConnectedGraph(afterDetach)).toBe(false);
    });

    test('lateral root gets its own position when derived slot is occupied', () => {
        // Main chain: a → b → c (longer, placed first at cols 1,2,3)
        // Lateral: d → b (d is a second root, col 1 is taken by a)
        const edges = [
            { from: 'a', to: 'b', direction: 'right' },
            { from: 'b', to: 'c', direction: 'right' },
            { from: 'd', to: 'b', direction: 'right' },
        ];
        const pos = computeGridPositions(edges);
        expect(pos.size).toBe(4);
        expect(pos.has('d')).toBe(true);

        // d is placed — exact position depends on collision avoidance
        // but it must not overlap with a (both are roots)
        const aPos = pos.get('a')!;
        const dPos = pos.get('d')!;
        expect(aPos.row === dPos.row && aPos.col === dPos.col).toBe(false);
    });
});
