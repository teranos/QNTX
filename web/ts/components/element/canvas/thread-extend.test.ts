/**
 * Tests for thread extension — picking up 〽 and dropping at a new endpoint.
 *
 * Tim: Happy path — pickup, drop, spine survives with all nodes
 * Spike: Edge cases — pickup then cancel, no new clicks before drop
 *
 * The bug we're catching: after extend drop, the renderer should have
 * a single spine containing [...existing non-thread nodes, 〽-id]. If
 * the renderer ends up with zero spines, or a spine with empty/missing
 * nodes, this test fails.
 */

import { describe, test, expect, mock, beforeEach } from 'bun:test';
import { enterThreadBuildingMode } from './thread-line';
import { addSpine, removeSpine, getSpineByNode } from './spine-renderer';

// Mock browser APIs that happy-dom doesn't fully provide
if (globalThis.window?.Element?.prototype) {
    (globalThis.window as any).Element.prototype.animate = function() {
        return { finished: Promise.resolve(), onfinish: null, cancel: () => {} } as any;
    };
}
// Run RAF synchronously once per call so we can inspect rendered path output
let _rafCb: FrameRequestCallback | null = null;
globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
    _rafCb = cb;
    return 0;
};
globalThis.cancelAnimationFrame = () => { _rafCb = null; };
function tickRAF(): void {
    const cb = _rafCb;
    _rafCb = null;
    if (cb) cb(performance.now());
}

mock.module('@teranos/elements', () => ({
    createCursorElement: (symbol: string, elementType: string) => {
        const el = document.createElement('div');
        el.className = 'cursor';
        el.setAttribute('data-element-type', elementType);
        const sym = document.createElement('span');
        sym.className = 'cursor-symbol';
        sym.textContent = symbol;
        el.appendChild(sym);
        return el;
    },
    attachCursorToMouse: () => () => {},
}));

// JSDOM lacks elementFromPoint; thread-line uses it for hit-testing through the cursor.
// In these tests the cursor never overlays an element at drop time, so null is the correct stub.
if (!document.elementFromPoint) {
    (document as any).elementFromPoint = () => null;
}

const origCreateElementNS = document.createElementNS?.bind(document);
if (!origCreateElementNS || typeof origCreateElementNS !== 'function') {
    (document as any).createElementNS = (_ns: string, tag: string) => {
        const el = document.createElement(tag);
        el.setAttribute = el.setAttribute.bind(el);
        return el;
    };
}

const _MouseEvent = (globalThis as any).MouseEvent ?? (globalThis as any).window?.MouseEvent ?? class extends Event { clientX = 0; clientY = 0; button = 0; constructor(type: string, init?: any) { super(type, init); Object.assign(this, init); } };
const _KeyboardEvent = (globalThis as any).KeyboardEvent ?? (globalThis as any).window?.KeyboardEvent ?? class extends Event { key = ''; constructor(type: string, init?: any) { super(type, init); Object.assign(this, init); } };

function createItem(container: HTMLElement, id: string, symbol: string, threadElement = false): HTMLElement {
    const item = document.createElement('div');
    item.className = threadElement ? 'canvas-thread-element canvas-element' : 'canvas-element';
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

function simulateClick(x: number, y: number): void {
    const mousedown = new _MouseEvent('mousedown', {
        button: 0, clientX: x, clientY: y, bubbles: true,
    });
    document.dispatchEvent(mousedown);
}

/**
 * Replicate the extend-drop completion logic from canvas-workspace-builder.ts.
 * If this function diverges from the real handler, the test stops being valid.
 */
function runExtendDrop(
    canvasId: string,
    container: HTMLElement,
    threadElementEl: HTMLElement,
    threadElementId: string,
    oldSpine: { id: string; color: string; nodes: string[] },
    result: { nodeIds: string[]; placeX: number; placeY: number; cursorElement: HTMLElement; symbolElement: HTMLElement | null },
): { id: string; color: string; nodes: string[] } {
    threadElementEl.style.left = `${result.placeX}px`;
    threadElementEl.style.top = `${result.placeY}px`;
    threadElementEl.style.visibility = '';
    result.cursorElement.remove();

    const newSpine = {
        id: `spine-${Math.random().toString(36).slice(2)}`,
        color: oldSpine.color,
        nodes: [...result.nodeIds, threadElementId],
    };
    addSpine(canvasId, container, newSpine);
    return newSpine;
}

describe('Thread Extend - Tim (Happy Path)', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        container.className = 'canvas-content-layer';
        document.body.appendChild(container);
    });

    test('Tim picks up 〽 and drops at new spot — exactly one spine remains, containing all original nodes plus 〽', () => {
        const canvasId = 'tim-basic';
        // Initial state: A, B, 〽 connected by a spine
        createItem(container, 'element-a', 'A');
        createItem(container, 'element-b', 'B');
        const threadElementEl = createItem(container, 'element-thread', '〽', true);

        const initialSpine = {
            id: 'spine-1',
            color: '#c45454',
            nodes: ['element-a', 'element-b', 'element-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        expect(getSpineByNode(canvasId, 'element-thread')?.id).toBe('spine-1');

        // Pick up 〽 — replicates canvas-workspace-builder click handler logic
        removeSpine(canvasId, initialSpine.id);
        threadElementEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadElementEl.querySelector('.symbol') as HTMLElement;

        let dropResult: any = null;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            dropResult = result;
            runExtendDrop(canvasId, container, threadElementEl, 'element-thread', initialSpine, result);
        }, () => {}, existingNodeIds);

        // User clicks empty canvas to drop
        simulateClick(500, 500);

        // The bug surfaces here if any of these fail:
        expect(dropResult).not.toBeNull();
        expect(dropResult.nodeIds).toEqual(['element-a', 'element-b']);

        // After drop: renderer must contain the new spine through all original nodes
        const survivingSpine = getSpineByNode(canvasId, 'element-thread');
        expect(survivingSpine).not.toBeNull();
        expect(survivingSpine!.nodes).toEqual(['element-a', 'element-b', 'element-thread']);

        // Each spine node must be findable in the container (otherwise the path can't render)
        for (const nodeId of survivingSpine!.nodes) {
            const el = container.querySelector(`[data-element-id="${nodeId}"]`);
            expect(el).not.toBeNull();
        }

        // 〽 must be visible again
        expect(threadElementEl.style.visibility).not.toBe('hidden');
    });

    test('After extend drop, the spine SVG path has non-empty d (catches the "vanished spine" visual bug)', () => {
        const canvasId = 'tim-svg';
        // This test stresses the actual render output: after addSpine, the path's `d`
        // attribute should contain at least one segment if the renderer has nodes it can
        // resolve to coordinates. If the spine is "visually gone" because getSymbolCenter
        // returns null for nodes, this assertion fails.
        createItem(container, 'element-a', 'A');
        createItem(container, 'element-b', 'B');
        const threadElementEl = createItem(container, 'element-thread', '〽', true);

        const initialSpine = {
            id: 'spine-1',
            color: '#c45454',
            nodes: ['element-a', 'element-b', 'element-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        removeSpine(canvasId, initialSpine.id);
        threadElementEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadElementEl.querySelector('.symbol') as HTMLElement;

        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            runExtendDrop(canvasId, container, threadElementEl, 'element-thread', initialSpine, result);
        }, () => {}, existingNodeIds);

        simulateClick(500, 500);

        // Tick the renderer RAF — this is when path d gets set from node positions
        tickRAF();

        const survivingSpine = getSpineByNode(canvasId, 'element-thread');
        expect(survivingSpine).not.toBeNull();

        // The SVG path must exist and have a non-empty d attribute, else it's invisible
        const svg = container.querySelector('svg');
        expect(svg).not.toBeNull();
        const paths = svg!.querySelectorAll('path');
        expect(paths.length).toBeGreaterThan(0);
        const d = paths[0].getAttribute('d');
        expect(d).not.toBeNull();
        expect(d).not.toBe('');
    });
});

describe('Thread Extend - Axiom (DOM identity)', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        container.className = 'canvas-content-layer';
        document.body.appendChild(container);
    });

    test('Axiom: the 〽 element after drop is identical (===) to the one before pickup', async () => {
        const canvasId = 'axiom-identity';
        createItem(container, 'axiom-a', 'A');
        const threadElementEl = createItem(container, 'axiom-thread', '〽', true);
        const captured = threadElementEl; // reference snapshot before pickup

        const initialSpine = {
            id: 'axiom-spine-1',
            color: '#c45454',
            nodes: ['axiom-a', 'axiom-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        const { unpinThreadElement, pinThreadElement } = await import('../thread-element');

        // Pick up: same element gets unpinned to cursor mode (reparented to body)
        removeSpine(canvasId, initialSpine.id);
        unpinThreadElement(threadElementEl);
        expect(threadElementEl.parentElement).toBe(document.body);
        expect(threadElementEl).toBe(captured); // identity preserved

        const symbolEl = threadElementEl.querySelector('.cursor-symbol') as HTMLElement;
        expect(symbolEl).not.toBeNull();

        let droppedElement: HTMLElement | null = null;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            droppedElement = result.cursorElement;
            pinThreadElement(threadElementEl, container, result.placeX, result.placeY, '#c45454', 'axiom-thread');
            addSpine(canvasId, container, {
                id: 'axiom-spine-2',
                color: '#c45454',
                nodes: [...result.nodeIds, 'axiom-thread'],
            });
        }, () => {}, ['axiom-a'], threadElementEl);

        simulateClick(700, 700);

        // The element returned from the drop is identical to the one we started with
        expect(droppedElement).toBe(captured);
        expect(threadElementEl).toBe(captured);
        expect(threadElementEl.parentElement).toBe(container); // back on canvas
        expect(threadElementEl.classList.contains('canvas-thread-element')).toBe(true);
        expect(threadElementEl.querySelector('.symbol')).not.toBeNull();
    });

    test('Axiom: no second element with the same data-element-id exists at any point', async () => {
        const canvasId = 'axiom-unique-id';
        createItem(container, 'unique-a', 'A');
        const threadElementEl = createItem(container, 'unique-thread', '〽', true);
        addSpine(canvasId, container, { id: 'unique-spine-1', color: '#c45454', nodes: ['unique-a', 'unique-thread'] });

        const { unpinThreadElement, pinThreadElement } = await import('../thread-element');

        removeSpine(canvasId, 'unique-spine-1');
        unpinThreadElement(threadElementEl);

        // During cursor mode: still exactly one element with this id
        const matchesMid = document.querySelectorAll('[data-element-id="unique-thread"]');
        expect(matchesMid.length).toBe(1);

        const symbolEl = threadElementEl.querySelector('.cursor-symbol') as HTMLElement;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            pinThreadElement(threadElementEl, container, result.placeX, result.placeY, '#c45454', 'unique-thread');
        }, () => {}, ['unique-a'], threadElementEl);

        simulateClick(500, 500);

        // After drop: still exactly one
        const matchesAfter = document.querySelectorAll('[data-element-id="unique-thread"]');
        expect(matchesAfter.length).toBe(1);
    });
});

describe('Thread Extend - Spike (Edge Cases)', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        container.className = 'canvas-content-layer';
        document.body.appendChild(container);
    });

    test('Spike picks up 〽 and presses Escape — original spine restored, 〽 visible', () => {
        const canvasId = 'spike-cancel';
        createItem(container, 'spike-a', 'A');
        createItem(container, 'spike-b', 'B');
        const threadElementEl = createItem(container, 'spike-thread', '〽', true);

        const initialSpine = {
            id: 'spike-spine-1',
            color: '#c45454',
            nodes: ['spike-a', 'spike-b', 'spike-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        removeSpine(canvasId, initialSpine.id);
        threadElementEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadElementEl.querySelector('.symbol') as HTMLElement;

        enterThreadBuildingMode(symbolEl, '#c45454', () => {}, () => {
            // Cancel handler: re-add old spine, restore 〽 visibility
            addSpine(canvasId, container, initialSpine);
            threadElementEl.style.visibility = '';
        }, existingNodeIds);

        const keydown = new _KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
        document.dispatchEvent(keydown);

        const restored = getSpineByNode(canvasId, 'spike-thread');
        expect(restored?.id).toBe('spike-spine-1');
        expect(threadElementEl.style.visibility).not.toBe('hidden');
    });
});
