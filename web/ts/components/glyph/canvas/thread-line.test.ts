/**
 * Tests for thread building mode — creation and extension.
 *
 * Tim: Happy path — create thread, extend thread
 * Spike: Edge cases — cancel extend, missing nodes
 */

import { describe, test, expect, mock, beforeEach } from 'bun:test';
import { enterThreadBuildingMode, type ThreadBuildResult } from './thread-line';

// Mock browser APIs
if (globalThis.window?.Element?.prototype) {
    (globalThis.window as any).Element.prototype.animate = function() {
        return { finished: Promise.resolve(), onfinish: null, cancel: () => {} } as any;
    };
}
globalThis.requestAnimationFrame = (_cb: FrameRequestCallback) => 0;
globalThis.cancelAnimationFrame = () => {};

// Mock placement-mode scrim
mock.module('./placement-mode', () => ({
    showMenuScrim: () => {},
    removeScrim: () => {},
}));

// Mock @qntx/glyphs cursor
mock.module('@qntx/glyphs', () => ({
    createCursorElement: () => {
        const el = document.createElement('div');
        el.className = 'cursor-element';
        return el;
    },
    attachCursorToMouse: () => () => {},
}));

// Mock createElementNS for SVG elements
const origCreateElementNS = document.createElementNS?.bind(document);
if (!origCreateElementNS || typeof origCreateElementNS !== 'function') {
    (document as any).createElementNS = (_ns: string, tag: string) => {
        const el = document.createElement(tag);
        el.setAttribute = el.setAttribute.bind(el);
        return el;
    };
}

/** Create a fake glyph with a .glyph-symbol inside, append to container */
function createGlyph(container: HTMLElement, id: string, symbol: string): HTMLElement {
    const glyph = document.createElement('div');
    glyph.className = 'canvas-glyph';
    glyph.dataset.glyphId = id;
    glyph.style.position = 'absolute';
    glyph.style.left = '100px';
    glyph.style.top = '100px';
    glyph.style.width = '80px';
    glyph.style.height = '40px';

    const sym = document.createElement('span');
    sym.className = 'glyph-symbol';
    sym.textContent = symbol;
    glyph.appendChild(sym);

    container.appendChild(glyph);
    return glyph;
}

function getSymbol(glyph: HTMLElement): HTMLElement {
    return glyph.querySelector('.glyph-symbol') as HTMLElement;
}

// happy-dom exposes event constructors on window, not globalThis
const _MouseEvent = (globalThis as any).MouseEvent ?? (globalThis as any).window?.MouseEvent ?? class extends Event { clientX = 0; clientY = 0; button = 0; constructor(type: string, init?: any) { super(type, init); Object.assign(this, init); } };
const _KeyboardEvent = (globalThis as any).KeyboardEvent ?? (globalThis as any).window?.KeyboardEvent ?? class extends Event { key = ''; constructor(type: string, init?: any) { super(type, init); Object.assign(this, init); } };

/** Fire mousedown on document (simulates click in thread building mode) */
function simulateClick(x: number, y: number): void {
    const mousedown = new _MouseEvent('mousedown', {
        button: 0, clientX: x, clientY: y, bubbles: true,
    });
    document.dispatchEvent(mousedown);
}

function simulateEscape(): void {
    const keydown = new _KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
    document.dispatchEvent(keydown);
}

describe('Thread Building Mode - Tim (Happy Path)', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        container.className = 'canvas-content-layer';
        document.body.appendChild(container);
    });

    test('Tim creates a new thread from a single origin symbol', () => {
        const glyphA = createGlyph(container, 'glyph-a', 'A');
        const symA = getSymbol(glyphA);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(symA, '#c45454', (r) => { result = r; }, () => {});

        // Click empty canvas to finish
        simulateClick(500, 500);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toEqual(['glyph-a']);
        expect(result!.placeX).toBe(500);
        expect(result!.placeY).toBe(500);
    });

    test('Tim extends an existing thread — pre-populated nodes appear in result', () => {
        createGlyph(container, 'glyph-a', 'A');
        createGlyph(container, 'glyph-b', 'B');
        const glyphB = container.querySelector('[data-glyph-id="glyph-b"]') as HTMLElement;
        const symB = getSymbol(glyphB);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symB, '#c45454',
            (r) => { result = r; },
            () => {},
            ['glyph-a', 'glyph-b'],
        );

        // Click empty canvas to finish (no new nodes added)
        simulateClick(600, 600);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toContain('glyph-a');
        expect(result!.nodeIds).toContain('glyph-b');
        expect(result!.nodeIds.length).toBe(2);
        expect(result!.nodeIds[0]).toBe('glyph-a');
        expect(result!.nodeIds[1]).toBe('glyph-b');
    });

    test('Tim extends a thread — existing node order is preserved', () => {
        createGlyph(container, 'glyph-x', 'X');
        createGlyph(container, 'glyph-y', 'Y');
        createGlyph(container, 'glyph-z', 'Z');
        const glyphZ = container.querySelector('[data-glyph-id="glyph-z"]') as HTMLElement;
        const symZ = getSymbol(glyphZ);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symZ, '#a83232',
            (r) => { result = r; },
            () => {},
            ['glyph-x', 'glyph-y', 'glyph-z'],
        );

        simulateClick(800, 800);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toEqual(['glyph-x', 'glyph-y', 'glyph-z']);
    });
});

describe('Thread Building Mode - Spike (Edge Cases)', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        container.className = 'canvas-content-layer';
        document.body.appendChild(container);
    });

    test('Spike cancels extend with Escape — onCancel fires', () => {
        createGlyph(container, 'glyph-a', 'A');
        const glyphA = container.querySelector('[data-glyph-id="glyph-a"]') as HTMLElement;
        const symA = getSymbol(glyphA);

        let cancelled = false;
        enterThreadBuildingMode(
            symA, '#c45454',
            () => {},
            () => { cancelled = true; },
            ['glyph-a'],
        );

        simulateEscape();

        expect(cancelled).toBe(true);
    });

    test('Spike extends with a missing glyph in existingNodeIds — skips missing', () => {
        createGlyph(container, 'glyph-a', 'A');
        // glyph-missing does not exist in DOM
        const glyphA = container.querySelector('[data-glyph-id="glyph-a"]') as HTMLElement;
        const symA = getSymbol(glyphA);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symA, '#c45454',
            (r) => { result = r; },
            () => {},
            ['glyph-a', 'glyph-missing'],
        );

        simulateClick(500, 500);

        expect(result).not.toBeNull();
        // glyph-missing has no DOM element, so its symbol wasn't found → not in nodes
        expect(result!.nodeIds).toContain('glyph-a');
        expect(result!.nodeIds).not.toContain('glyph-missing');
        expect(result!.nodeIds.length).toBe(1);
    });
});
