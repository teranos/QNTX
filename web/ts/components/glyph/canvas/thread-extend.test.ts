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

mock.module('./placement-mode', () => ({
    showMenuScrim: () => {},
    removeScrim: () => {},
}));

mock.module('@qntx/glyphs', () => ({
    createCursorElement: () => {
        const el = document.createElement('div');
        el.className = 'glyph-cursor';
        const sym = document.createElement('span');
        sym.className = 'glyph-cursor-symbol';
        sym.textContent = '〽';
        el.appendChild(sym);
        return el;
    },
    attachCursorToMouse: () => () => {},
}));

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

function createGlyph(container: HTMLElement, id: string, symbol: string, threadGlyph = false): HTMLElement {
    const glyph = document.createElement('div');
    glyph.className = threadGlyph ? 'canvas-thread-glyph canvas-glyph' : 'canvas-glyph';
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
    threadGlyphEl: HTMLElement,
    threadGlyphId: string,
    oldSpine: { id: string; color: string; nodes: string[] },
    result: { nodeIds: string[]; placeX: number; placeY: number; cursorElement: HTMLElement; symbolElement: HTMLElement | null },
): { id: string; color: string; nodes: string[] } {
    threadGlyphEl.style.left = `${result.placeX}px`;
    threadGlyphEl.style.top = `${result.placeY}px`;
    threadGlyphEl.style.visibility = '';
    result.cursorElement.remove();

    const newSpine = {
        id: `spine-${Math.random().toString(36).slice(2)}`,
        color: oldSpine.color,
        nodes: [...result.nodeIds, threadGlyphId],
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
        createGlyph(container, 'glyph-a', 'A');
        createGlyph(container, 'glyph-b', 'B');
        const threadGlyphEl = createGlyph(container, 'glyph-thread', '〽', true);

        const initialSpine = {
            id: 'spine-1',
            color: '#c45454',
            nodes: ['glyph-a', 'glyph-b', 'glyph-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        expect(getSpineByNode(canvasId, 'glyph-thread')?.id).toBe('spine-1');

        // Pick up 〽 — replicates canvas-workspace-builder click handler logic
        removeSpine(canvasId, initialSpine.id);
        threadGlyphEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadGlyphEl.querySelector('.glyph-symbol') as HTMLElement;

        let dropResult: any = null;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            dropResult = result;
            runExtendDrop(canvasId, container, threadGlyphEl, 'glyph-thread', initialSpine, result);
        }, () => {}, existingNodeIds);

        // User clicks empty canvas to drop
        simulateClick(500, 500);

        // The bug surfaces here if any of these fail:
        expect(dropResult).not.toBeNull();
        expect(dropResult.nodeIds).toEqual(['glyph-a', 'glyph-b']);

        // After drop: renderer must contain the new spine through all original nodes
        const survivingSpine = getSpineByNode(canvasId, 'glyph-thread');
        expect(survivingSpine).not.toBeNull();
        expect(survivingSpine!.nodes).toEqual(['glyph-a', 'glyph-b', 'glyph-thread']);

        // Each spine node must be findable in the container (otherwise the path can't render)
        for (const nodeId of survivingSpine!.nodes) {
            const el = container.querySelector(`[data-glyph-id="${nodeId}"]`);
            expect(el).not.toBeNull();
        }

        // 〽 must be visible again
        expect(threadGlyphEl.style.visibility).not.toBe('hidden');
    });

    test('After extend drop, the spine SVG path has non-empty d (catches the "vanished spine" visual bug)', () => {
        const canvasId = 'tim-svg';
        // This test stresses the actual render output: after addSpine, the path's `d`
        // attribute should contain at least one segment if the renderer has nodes it can
        // resolve to coordinates. If the spine is "visually gone" because getSymbolCenter
        // returns null for nodes, this assertion fails.
        createGlyph(container, 'glyph-a', 'A');
        createGlyph(container, 'glyph-b', 'B');
        const threadGlyphEl = createGlyph(container, 'glyph-thread', '〽', true);

        const initialSpine = {
            id: 'spine-1',
            color: '#c45454',
            nodes: ['glyph-a', 'glyph-b', 'glyph-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        removeSpine(canvasId, initialSpine.id);
        threadGlyphEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadGlyphEl.querySelector('.glyph-symbol') as HTMLElement;

        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            runExtendDrop(canvasId, container, threadGlyphEl, 'glyph-thread', initialSpine, result);
        }, () => {}, existingNodeIds);

        simulateClick(500, 500);

        // Tick the renderer RAF — this is when path d gets set from node positions
        tickRAF();

        const survivingSpine = getSpineByNode(canvasId, 'glyph-thread');
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
        createGlyph(container, 'axiom-a', 'A');
        const threadGlyphEl = createGlyph(container, 'axiom-thread', '〽', true);
        const captured = threadGlyphEl; // reference snapshot before pickup

        const initialSpine = {
            id: 'axiom-spine-1',
            color: '#c45454',
            nodes: ['axiom-a', 'axiom-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        const { unpinThreadGlyph, pinThreadGlyph } = await import('../thread-glyph');

        // Pick up: same element gets unpinned to cursor mode (reparented to body)
        removeSpine(canvasId, initialSpine.id);
        unpinThreadGlyph(threadGlyphEl);
        expect(threadGlyphEl.parentElement).toBe(document.body);
        expect(threadGlyphEl).toBe(captured); // identity preserved

        const symbolEl = threadGlyphEl.querySelector('.glyph-cursor-symbol') as HTMLElement;
        expect(symbolEl).not.toBeNull();

        let droppedElement: HTMLElement | null = null;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            droppedElement = result.cursorElement;
            pinThreadGlyph(threadGlyphEl, container, result.placeX, result.placeY, '#c45454', 'axiom-thread');
            addSpine(canvasId, container, {
                id: 'axiom-spine-2',
                color: '#c45454',
                nodes: [...result.nodeIds, 'axiom-thread'],
            });
        }, () => {}, ['axiom-a'], threadGlyphEl);

        simulateClick(700, 700);

        // The element returned from the drop is identical to the one we started with
        expect(droppedElement).toBe(captured);
        expect(threadGlyphEl).toBe(captured);
        expect(threadGlyphEl.parentElement).toBe(container); // back on canvas
        expect(threadGlyphEl.classList.contains('canvas-thread-glyph')).toBe(true);
        expect(threadGlyphEl.querySelector('.glyph-symbol')).not.toBeNull();
    });

    test('Axiom: no second element with the same data-glyph-id exists at any point', async () => {
        const canvasId = 'axiom-unique-id';
        createGlyph(container, 'unique-a', 'A');
        const threadGlyphEl = createGlyph(container, 'unique-thread', '〽', true);
        addSpine(canvasId, container, { id: 'unique-spine-1', color: '#c45454', nodes: ['unique-a', 'unique-thread'] });

        const { unpinThreadGlyph, pinThreadGlyph } = await import('../thread-glyph');

        removeSpine(canvasId, 'unique-spine-1');
        unpinThreadGlyph(threadGlyphEl);

        // During cursor mode: still exactly one element with this id
        const matchesMid = document.querySelectorAll('[data-glyph-id="unique-thread"]');
        expect(matchesMid.length).toBe(1);

        const symbolEl = threadGlyphEl.querySelector('.glyph-cursor-symbol') as HTMLElement;
        enterThreadBuildingMode(symbolEl, '#c45454', (result) => {
            pinThreadGlyph(threadGlyphEl, container, result.placeX, result.placeY, '#c45454', 'unique-thread');
        }, () => {}, ['unique-a'], threadGlyphEl);

        simulateClick(500, 500);

        // After drop: still exactly one
        const matchesAfter = document.querySelectorAll('[data-glyph-id="unique-thread"]');
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
        createGlyph(container, 'spike-a', 'A');
        createGlyph(container, 'spike-b', 'B');
        const threadGlyphEl = createGlyph(container, 'spike-thread', '〽', true);

        const initialSpine = {
            id: 'spike-spine-1',
            color: '#c45454',
            nodes: ['spike-a', 'spike-b', 'spike-thread'],
        };
        addSpine(canvasId, container, initialSpine);

        removeSpine(canvasId, initialSpine.id);
        threadGlyphEl.style.visibility = 'hidden';

        const existingNodeIds = initialSpine.nodes.slice(0, -1);
        const symbolEl = threadGlyphEl.querySelector('.glyph-symbol') as HTMLElement;

        enterThreadBuildingMode(symbolEl, '#c45454', () => {}, () => {
            // Cancel handler: re-add old spine, restore 〽 visibility
            addSpine(canvasId, container, initialSpine);
            threadGlyphEl.style.visibility = '';
        }, existingNodeIds);

        const keydown = new _KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
        document.dispatchEvent(keydown);

        const restored = getSpineByNode(canvasId, 'spike-thread');
        expect(restored?.id).toBe('spike-spine-1');
        expect(threadGlyphEl.style.visibility).not.toBe('hidden');
    });
});
