/**
 * Where a glyph lands when nothing says where.
 *
 * Personas:
 * - Tim: the emptiest place wins, and the tiers are ordered
 */

import { describe, expect, test } from 'bun:test';
import { findPlacement, overlapArea, placementCost } from './placement';

const rect = (x: number, y: number, width: number, height: number) => ({ x, y, width, height });

describe('overlapArea', () => {
    test('disjoint rects do not overlap', () => {
        expect(overlapArea(rect(0, 0, 10, 10), rect(20, 20, 10, 10))).toBe(0);
    });

    test('touching edges do not overlap', () => {
        expect(overlapArea(rect(0, 0, 10, 10), rect(10, 0, 10, 10))).toBe(0);
    });

    test('identical rects overlap entirely', () => {
        expect(overlapArea(rect(0, 0, 10, 10), rect(0, 0, 10, 10))).toBe(100);
    });

    test('a corner overlap is the intersecting area', () => {
        expect(overlapArea(rect(0, 0, 10, 10), rect(5, 5, 10, 10))).toBe(25);
    });

    test('a contained rect overlaps by its own area', () => {
        expect(overlapArea(rect(0, 0, 100, 100), rect(10, 10, 5, 5))).toBe(25);
    });
});

describe('Tim: placementCost', () => {
    // One occupant, 400 wide and 400 tall, its title bar the top 32px.
    const occupant = rect(100, 100, 400, 400);

    test('clear space costs nothing', () => {
        expect(placementCost(rect(600, 600, 200, 200), [occupant])).toBe(0);
    });

    // "titlebar overlap is worse than body overlap"
    test('covering a title bar costs more than covering body of the same area', () => {
        // Both candidates are 200x32 and overlap the occupant by 200x32.
        const onTitle = rect(150, 100, 200, 32);
        const onBody = rect(150, 300, 200, 32);

        expect(overlapArea(onTitle, occupant)).toBe(overlapArea(onBody, occupant));
        expect(placementCost(onTitle, [occupant])).toBeGreaterThan(placementCost(onBody, [occupant]));
    });

    // "body overlap is worse than empty space"
    test('covering body costs more than staying clear', () => {
        expect(placementCost(rect(150, 300, 200, 32), [occupant]))
            .toBeGreaterThan(placementCost(rect(600, 600, 200, 32), [occupant]));
    });

    // "we should prefer not to cover the left side of it, if we must overlap
    //  the right side of the titlebar is less worth perserving"
    test('covering the title costs more than covering the controls beside it', () => {
        // The occupant spans x 100..500; the controls hold the last 64px.
        const onTitle = rect(100, 100, 60, 32);
        const onControls = rect(440, 100, 60, 32);

        expect(overlapArea(onTitle, occupant)).toBe(overlapArea(onControls, occupant));
        expect(placementCost(onTitle, [occupant])).toBeGreaterThan(placementCost(onControls, [occupant]));
    });

    // "symbol is the highest tier, above title of titlebar"
    test('covering the symbol costs more than covering the title', () => {
        // Symbol holds x 100..132, title the span up to the controls at 436.
        const onSymbol = rect(100, 100, 30, 32);
        const onTitle = rect(200, 100, 30, 32);

        expect(overlapArea(onSymbol, occupant)).toBe(overlapArea(onTitle, occupant));
        expect(placementCost(onSymbol, [occupant])).toBeGreaterThan(placementCost(onTitle, [occupant]));
    });

    // "empty space is always preffered over everything else"
    test('the five tiers are ordered: symbol, title, bar, body, clear', () => {
        const symbol = placementCost(rect(100, 100, 30, 32), [occupant]);
        const title = placementCost(rect(200, 100, 30, 32), [occupant]);
        const bar = placementCost(rect(450, 100, 30, 32), [occupant]);
        const body = placementCost(rect(200, 300, 30, 32), [occupant]);
        const clear = placementCost(rect(700, 700, 30, 32), [occupant]);

        expect(symbol).toBeGreaterThan(title);
        expect(title).toBeGreaterThan(bar);
        expect(bar).toBeGreaterThan(body);
        expect(body).toBeGreaterThan(clear);
        expect(clear).toBe(0);
    });

    test('two occupants cost more than one', () => {
        const second = rect(600, 100, 400, 400);
        const across = rect(400, 300, 400, 100);
        expect(placementCost(across, [occupant, second])).toBeGreaterThan(placementCost(across, [occupant]));
    });
});

describe('Tim: findPlacement', () => {
    const bounds = { width: 1000, height: 800 };

    // "a placement should never be placing outbound of the viewport"
    test('stays inside the bounds', () => {
        const p = findPlacement({ width: 200, height: 100 }, [], bounds);
        expect(p.x).toBeGreaterThanOrEqual(0);
        expect(p.y).toBeGreaterThanOrEqual(0);
        expect(p.x + 200).toBeLessThanOrEqual(bounds.width);
        expect(p.y + 100).toBeLessThanOrEqual(bounds.height);
    });

    // "the placement that has the highest empty wins, 100% is uncontested win"
    test('takes clear space over an occupied region', () => {
        const occupied = [rect(0, 0, 500, 800)];
        const p = findPlacement({ width: 200, height: 100 }, occupied, bounds);
        expect(placementCost({ ...p, width: 200, height: 100 }, occupied)).toBe(0);
    });

    test('the same candidates give the same answer', () => {
        const source = () => {
            const seq = [0.1, 0.9, 0.35, 0.6, 0.2, 0.8, 0.5, 0.05];
            return () => { const v = seq.shift()!; seq.push(v); return v; };
        };
        const a = findPlacement({ width: 100, height: 100 }, [], bounds, { attempts: 4, rng: source() });
        const b = findPlacement({ width: 100, height: 100 }, [], bounds, { attempts: 4, rng: source() });
        expect(a).toEqual(b);
    });

    // "contestation starts when there is no placement with 100% clear"
    test('a canvas with no clear space still yields a position', () => {
        const occupied = [rect(0, 0, 1000, 800)];
        const p = findPlacement({ width: 200, height: 100 }, occupied, bounds, { attempts: 40 });
        expect(Number.isFinite(p.x)).toBe(true);
        expect(Number.isFinite(p.y)).toBe(true);
    });

    test('a glyph larger than the canvas is placed at the origin', () => {
        const p = findPlacement({ width: 2000, height: 2000 }, [], bounds);
        expect(p).toEqual({ x: 0, y: 0 });
    });
});

// ── clampToViewport — mobile may be the primary screen ─────────────

import { clampToViewport } from './placement';

describe('Tim: a window answers to the viewport', () => {
    const phone = { width: 390, height: 844 };

    // The canvas→window default is 520×420 — wider than a phone. Off-screen
    // controls are unreachable; the box must shrink to fit.
    test('an oversized box shrinks to the viewport ratio', () => {
        const box = clampToViewport({ x: 0, y: 0, width: 520, height: 420 }, phone);
        expect(box.width).toBe(Math.floor(390 * 0.8));
        expect(box.height).toBe(420);
    });

    test('a remembered position off the right edge is pulled back in', () => {
        const box = clampToViewport({ x: 350, y: 100, width: 200, height: 150 }, phone);
        expect(box.x + box.width).toBeLessThanOrEqual(phone.width);
        expect(box.y).toBe(100);
    });
});

describe('Spike: clamping edge cases', () => {
    test('a box that fits is untouched', () => {
        const box = clampToViewport({ x: 40, y: 60, width: 380, height: 220 }, { width: 1280, height: 800 });
        expect(box).toEqual({ x: 40, y: 60, width: 380, height: 220 });
    });

    test('a negative position lands at the origin', () => {
        const box = clampToViewport({ x: -50, y: -20, width: 200, height: 150 }, { width: 1280, height: 800 });
        expect(box.x).toBe(0);
        expect(box.y).toBe(0);
    });

    test('a box larger than both axes shrinks to the ratio and stays fully visible', () => {
        const box = clampToViewport({ x: 500, y: 900, width: 2000, height: 2000 }, { width: 390, height: 844 });
        expect(box.width).toBe(Math.floor(390 * 0.8));
        expect(box.height).toBe(Math.floor(844 * 0.8));
        expect(box.x + box.width).toBeLessThanOrEqual(390);
        expect(box.y + box.height).toBeLessThanOrEqual(844);
    });
});
