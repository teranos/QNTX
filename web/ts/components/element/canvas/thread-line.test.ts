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

// JSDOM lacks elementFromPoint; thread-line uses it for hit-testing through the cursor.
// In these tests the cursor never overlays an element, so null is the correct stub.
if (!document.elementFromPoint) {
    (document as any).elementFromPoint = () => null;
}

// Mock createElementNS for SVG elements
const origCreateElementNS = document.createElementNS?.bind(document);
if (!origCreateElementNS || typeof origCreateElementNS !== 'function') {
    (document as any).createElementNS = (_ns: string, tag: string) => {
        const el = document.createElement(tag);
        el.setAttribute = el.setAttribute.bind(el);
        return el;
    };
}

/** Create a fake element with a .symbol inside, append to container */
function createItem(container: HTMLElement, id: string, symbol: string): HTMLElement {
    const item = document.createElement('div');
    item.className = 'canvas-element';
    item.dataset.elementId = id;
    item.style.position = 'absolute';
    item.style.left = '100px';
    item.style.top = '100px';
    item.style.width = '80px';
    item.style.height = '40px';

    const sym = document.createElement('span');
    sym.className = 'symbol';
    sym.textContent = symbol;
    item.appendChild(sym);

    container.appendChild(item);
    return item;
}

function getSymbol(item: HTMLElement): HTMLElement {
    return item.querySelector('.symbol') as HTMLElement;
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
        const elementA = createItem(container, 'element-a', 'A');
        const symA = getSymbol(elementA);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(symA, '#c45454', (r) => { result = r; }, () => {});

        // Click empty canvas to finish
        simulateClick(500, 500);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toEqual(['element-a']);
        expect(result!.placeX).toBe(500);
        expect(result!.placeY).toBe(500);
    });

    test('Tim extends an existing thread — pre-populated nodes appear in result', () => {
        createItem(container, 'element-a', 'A');
        createItem(container, 'element-b', 'B');
        const elementB = container.querySelector('[data-element-id="element-b"]') as HTMLElement;
        const symB = getSymbol(elementB);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symB, '#c45454',
            (r) => { result = r; },
            () => {},
            ['element-a', 'element-b'],
        );

        // Click empty canvas to finish (no new nodes added)
        simulateClick(600, 600);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toContain('element-a');
        expect(result!.nodeIds).toContain('element-b');
        expect(result!.nodeIds.length).toBe(2);
        expect(result!.nodeIds[0]).toBe('element-a');
        expect(result!.nodeIds[1]).toBe('element-b');
    });

    test('Tim extends a thread — existing node order is preserved', () => {
        createItem(container, 'element-x', 'X');
        createItem(container, 'element-y', 'Y');
        createItem(container, 'element-z', 'Z');
        const elementZ = container.querySelector('[data-element-id="element-z"]') as HTMLElement;
        const symZ = getSymbol(elementZ);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symZ, '#a83232',
            (r) => { result = r; },
            () => {},
            ['element-x', 'element-y', 'element-z'],
        );

        simulateClick(800, 800);

        expect(result).not.toBeNull();
        expect(result!.nodeIds).toEqual(['element-x', 'element-y', 'element-z']);
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
        createItem(container, 'element-a', 'A');
        const elementA = container.querySelector('[data-element-id="element-a"]') as HTMLElement;
        const symA = getSymbol(elementA);

        let cancelled = false;
        enterThreadBuildingMode(
            symA, '#c45454',
            () => {},
            () => { cancelled = true; },
            ['element-a'],
        );

        simulateEscape();

        expect(cancelled).toBe(true);
    });

    test('Spike extends with a missing element in existingNodeIds — skips missing', () => {
        createItem(container, 'element-a', 'A');
        // element-missing does not exist in DOM
        const elementA = container.querySelector('[data-element-id="element-a"]') as HTMLElement;
        const symA = getSymbol(elementA);

        let result: ThreadBuildResult | null = null;
        enterThreadBuildingMode(
            symA, '#c45454',
            (r) => { result = r; },
            () => {},
            ['element-a', 'element-missing'],
        );

        simulateClick(500, 500);

        expect(result).not.toBeNull();
        // element-missing has no DOM element, so its symbol wasn't found → not in nodes
        expect(result!.nodeIds).toContain('element-a');
        expect(result!.nodeIds).not.toContain('element-missing');
        expect(result!.nodeIds.length).toBe(1);
    });
});
