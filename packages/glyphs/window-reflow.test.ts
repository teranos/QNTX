/**
 * A glyph dragged against an edge gives way at that edge, either one. Both are
 * asked, because a fixed box measures its own room from the left alone.
 */

import { describe, test, expect } from 'bun:test';
import { reflowBox } from './window-reflow';
import { MIN_WINDOW_WIDTH } from './glyph';

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
        const squeezed = reflowBox(700, 400, VIEWPORT);
        expect(squeezed.width).toBe(300);

        // The natural width is what is asked with, so nothing ratchets down.
        const returned = reflowBox(300, 400, VIEWPORT);
        expect(returned.width).toBe(400);
    });

    test('a window is never narrower than a window may be', () => {
        const box = reflowBox(VIEWPORT + 500, 400, VIEWPORT);
        expect(box.width).toBe(MIN_WINDOW_WIDTH);
        expect(box.left).toBeLessThan(VIEWPORT);
    });

    test('a window stops giving at the width its content needs', () => {
        // Ten columns of a token list need more than a grab handle's worth.
        const box = reflowBox(900, 600, VIEWPORT, 300);
        expect(box.width).toBe(300);
        expect(box.left).toBe(VIEWPORT - 300);
    });

    test('content needing more than the screen gets the screen', () => {
        const box = reflowBox(0, 2000, VIEWPORT, 1400);
        expect(box.width).toBe(VIEWPORT);
    });
});
