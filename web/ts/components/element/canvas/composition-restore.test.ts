/**
 * Opening a canvas restores its own compositions and no other canvas's.
 *
 * Personas:
 * - Spike: Edge cases — a composition on another canvas, or holding a minimized element, is left as it is
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { buildEdgesFromChain } from '@teranos/elements';
import { buildCanvasWorkspace } from './canvas-workspace-builder';
import { uiState } from '../../../state/ui';

(window as any).Element.prototype.animate = function() {
    return { finished: Promise.resolve(), onfinish: null } as any;
};
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;
(window as any).matchMedia = () => ({ matches: false });

const rootComposition = { id: 'comp-root', edges: buildEdgesFromChain(['root-a', 'root-b']), x: 0, y: 0 };

/** Restore runs after the elements render: two turns of the loop. */
const restored = () => new Promise((done) => setTimeout(done, 20));

beforeEach(() => {
    document.body.innerHTML = '';
    uiState.setCanvasElements([
        { id: 'root-a', symbol: 'py', x: 0, y: 0, canvas_id: '' },
        { id: 'root-b', symbol: 'py', x: 200, y: 0, canvas_id: '' },
    ]);
    uiState.setCanvasCompositions([rootComposition]);
    uiState.clearMinimizedWindows();
});

describe('Spike: compositions stay on their canvas', () => {
    test('opening a subcanvas leaves the root canvas its composition', async () => {
        document.body.appendChild(buildCanvasWorkspace('sub-1', []));
        await restored();

        expect(uiState.getCanvasCompositions().map((c) => c.id)).toEqual(['comp-root']);
    });

    test('a composition one of whose elements is minimized is kept, not removed as stale', async () => {
        uiState.addMinimizedWindow('root-b');
        document.body.appendChild(buildCanvasWorkspace('canvas-workspace', []));
        await restored();

        expect(uiState.getCanvasCompositions().map((c) => c.id)).toEqual(['comp-root']);
    });

    test('a composition whose element is gone from every canvas is removed', async () => {
        uiState.setCanvasElements([{ id: 'root-a', symbol: 'py', x: 0, y: 0, canvas_id: '' }]);
        document.body.appendChild(buildCanvasWorkspace('canvas-workspace', []));
        await restored();

        expect(uiState.getCanvasCompositions()).toEqual([]);
    });
});
