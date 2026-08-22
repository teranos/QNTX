/**
 * The brow — the node's status line around the Dynamic Island.
 *
 * Personas:
 * - Tim: happy path — geometry from the inset, items painted well/unwell
 * - Spike: edge cases — no headroom, no shell, forced preview, empty row
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { browGeometry, shouldMountBrow, buildBrow, paintBrow, type StatusItem } from './brow';

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: geometry falls out of the safe-area inset', () => {
    // The island families share one relation: the safe line sits 11pt below
    // a 37pt island. 59pt of inset → the island's top is at 11.
    test('a 59px inset puts the band level with the island', () => {
        const g = browGeometry(59, 393);
        expect(g.bandTop).toBe(11);
        expect(g.bandHeight).toBe(37);
        expect(g.sliverHeight).toBe(11);
    });

    test('a 62px inset (the larger family) puts the band at 14', () => {
        const g = browGeometry(62, 402);
        expect(g.bandTop).toBe(14);
    });

    test('the ears end at the island column, margin included', () => {
        const g = browGeometry(59, 393);
        expect(g.gapLeft).toBe(Math.floor(393 / 2 - 63 - 8));
        expect(g.gapRight).toBe(Math.ceil(393 / 2 + 63 + 8));
        expect(g.gapRight - g.gapLeft).toBeGreaterThanOrEqual(126 + 16);
    });
});

describe('Tim: the row is painted from the node’s items', () => {
    const items: StatusItem[] = [
        { name: 'QNTX', glyph: '+' },
        { name: 'pulse', note: '0.4.2', glyph: '+' },
        { name: 'pty', glyph: '!' },
    ];

    test('first item takes the left ear, the rest the right', () => {
        const brow = buildBrow(59, 393);
        document.body.appendChild(brow.root);
        paintBrow(brow, items);

        expect(brow.leftEar.textContent).toContain('QNTX');
        expect(brow.rightEar.textContent).toContain('pulse');
        expect(brow.rightEar.textContent).toContain('pty');
        expect(brow.leftEar.textContent).not.toContain('pty');
    });

    test('well and unwell wear their classes; notes ride along dimmed', () => {
        const brow = buildBrow(59, 393);
        document.body.appendChild(brow.root);
        paintBrow(brow, items);

        expect(brow.leftEar.querySelector('.brow-well')).not.toBeNull();
        expect(brow.rightEar.querySelector('.brow-unwell')!.textContent).toContain('pty');
        expect(brow.rightEar.querySelector('.brow-note')!.textContent).toBe('0.4.2');
    });

    test('anything unwell turns the sliver solid', () => {
        const brow = buildBrow(59, 393);
        document.body.appendChild(brow.root);

        paintBrow(brow, [{ name: 'QNTX', glyph: '+' }]);
        expect(brow.sliver.classList.contains('brow-sliver-unwell')).toBe(false);

        paintBrow(brow, items);
        expect(brow.sliver.classList.contains('brow-sliver-unwell')).toBe(true);
    });

    test('repainting replaces, never accumulates', () => {
        const brow = buildBrow(59, 393);
        document.body.appendChild(brow.root);
        paintBrow(brow, items);
        paintBrow(brow, items);
        expect(brow.rightEar.querySelectorAll('.brow-item').length).toBe(2);
    });
});

describe('Spike: the brow only exists where it belongs', () => {
    test('no headroom, no brow', () => {
        expect(shouldMountBrow(0, true)).toBe(false);
    });

    test('a notch-sized inset outside the shell is not a brow', () => {
        expect(shouldMountBrow(59, false)).toBe(false);
    });

    test('shell plus island headroom mounts', () => {
        expect(shouldMountBrow(59, true)).toBe(true);
    });

    test('?brow forces a preview anywhere', () => {
        expect(shouldMountBrow(0, false, true)).toBe(true);
    });

    test('an empty row paints an empty, well brow', () => {
        const brow = buildBrow(59, 393);
        document.body.appendChild(brow.root);
        paintBrow(brow, []);
        expect(brow.leftEar.querySelectorAll('.brow-item').length).toBe(0);
        expect(brow.sliver.classList.contains('brow-sliver-unwell')).toBe(false);
    });
});
