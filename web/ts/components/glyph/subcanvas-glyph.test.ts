/**
 * Tests for subcanvas glyph — compact canvas-placed ↔ fullscreen morph
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createSubcanvasGlyph } from './subcanvas-glyph';
import { buildCanvasWorkspace } from './canvas/canvas-workspace-builder';
import type { Glyph } from './glyph';

// Mock animate for morph transitions
(window as any).Element.prototype.animate = function() {
    return { finished: Promise.resolve(), onfinish: null } as any;
};

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock matchMedia for rectangle selection guard
(window as any).matchMedia = () => ({ matches: false });

function makeGlyph(overrides: Partial<Glyph> = {}): Glyph {
    return {
        id: 'subcanvas-test-1',
        title: 'Subcanvas',
        symbol: '⌗',
        x: 100,
        y: 100,
        width: 180,
        height: 120,
        renderContent: () => document.createElement('div'),
        ...overrides,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Subcanvas Glyph - Tim (Happy Path)', () => {
    test('Tim spawns a compact subcanvas glyph with correct structure', () => {
        const glyph = makeGlyph();
        const element = createSubcanvasGlyph(glyph);

        expect(element.dataset.glyphId).toBe('subcanvas-test-1');
        expect(element.classList.contains('canvas-subcanvas-glyph')).toBe(true);

        // Has grid preview area
        const preview = element.querySelector('.subcanvas-preview');
        expect(preview).toBeTruthy();
    });

    test('Tim sees subcanvas glyph positioned on the canvas', () => {
        const glyph = makeGlyph({ x: 250, y: 300 });
        const element = createSubcanvasGlyph(glyph);

        // canvasPlaced applies position from glyph data
        expect(element.style.left).toBe('250px');
        expect(element.style.top).toBe('300px');
    });

    test('Tim has a dblclick handler wired on the subcanvas element', () => {
        const glyph = makeGlyph();
        const element = createSubcanvasGlyph(glyph);

        // Dispatch dblclick — should not throw (morph will fail gracefully in test env)
        expect(() => {
            element.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));
        }).not.toThrow();
    });
});

// canvas_id isolation tests live in ts/state/canvas-id.test.ts to avoid
// mock.module leaks from glyph test files that mock ../../state/ui.

describe('Subcanvas Workspace dblclick Boundary - Tim (Happy Path)', () => {
    test('Tim dblclicks inside workspace and event does not bubble past it', () => {
        const workspace = buildCanvasWorkspace('subcanvas-test-1', []);

        // Wrap in a parent that tracks dblclick bubbling
        const parent = document.createElement('div');
        let parentReceived = false;
        parent.addEventListener('dblclick', () => { parentReceived = true; });
        parent.appendChild(workspace);
        document.body.appendChild(parent);

        // dblclick on workspace content layer
        const contentLayer = workspace.querySelector('.canvas-content-layer')!;
        contentLayer.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        // Parent should NOT receive the event
        expect(parentReceived).toBe(false);
    });
});
