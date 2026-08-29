/**
 * A glyph dragged against an edge gives way at that edge, either one.
 *
 * A fixed box positioned from the left has its available width measured from
 * the left alone, so it only ever felt the right edge. This is the arithmetic
 * that makes both edges the same.
 */

import { describe, test, expect } from 'bun:test';
import { reflowBox } from './window-reflow';

const VIEWPORT = 1000;

describe('Tim', () => {
    test('a window in open space keeps its own width', () => {
        const box = reflowBox(300, 400, VIEWPORT);
        expect(box.left).toBe(300);
        expect(box.width).toBe(400);
    });

    test('a window against neither edge is where it was put', () => {
        const box = reflowBox(0, 400, VIEWPORT);
        expect(box.left).toBe(0);
        expect(box.width).toBe(400);
    });
});

describe('Spike', () => {
    test('pushed past the right edge, the right edge holds and the width gives', () => {
        // Wants to sit at 800 and is 400 wide, so its right would be at 1200.
        const box = reflowBox(800, 400, VIEWPORT);
        expect(box.left).toBe(800);
        expect(box.width).toBe(200);
        expect(box.left + box.width).toBe(VIEWPORT);
    });

    test('pushed past the left edge, the left edge holds and the width gives', () => {
        // Wants to sit at -200 and is 400 wide, so its right would be at 200.
        const box = reflowBox(-200, 400, VIEWPORT);
        expect(box.left).toBe(0);
        expect(box.width).toBe(200);
    });

    test('a window wider than the screen is the screen', () => {
        const box = reflowBox(0, 1600, VIEWPORT);
        expect(box.left).toBe(0);
        expect(box.width).toBe(VIEWPORT);
    });
});

describe('Jenny', () => {
    test('the two edges give the same width for the same overshoot', () => {
        const right = reflowBox(VIEWPORT - 100, 400, VIEWPORT);
        const left = reflowBox(100 - 400, 400, VIEWPORT);
        expect(right.width).toBe(left.width);
    });

    test('coming back off an edge is the width it had before', () => {
        const squeezed = reflowBox(850, 400, VIEWPORT);
        expect(squeezed.width).toBe(150);

        // The natural width is what is asked with, so nothing ratchets down.
        const returned = reflowBox(300, 400, VIEWPORT);
        expect(returned.width).toBe(400);
    });

    test('a window is never narrower than something you can grab', () => {
        const box = reflowBox(VIEWPORT + 500, 400, VIEWPORT);
        expect(box.width).toBeGreaterThanOrEqual(120);
        expect(box.left).toBeLessThan(VIEWPORT);
    });
});
