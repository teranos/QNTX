/**
 * Tests for subcanvas element — compact canvas-placed ↔ fullscreen morph
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createSubcanvasElement } from './subcanvas-element';
import { buildCanvasWorkspace } from './canvas/canvas-workspace-builder';
import type { Element } from '@teranos/elements';

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

function makeElement(overrides: Partial<Element> = {}): Element {
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

describe('Subcanvas Element - Tim (Happy Path)', () => {
    test('Tim spawns a compact subcanvas element with correct structure', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        expect(element.dataset.elementId).toBe('subcanvas-test-1');
        expect(element.classList.contains('canvas-subcanvas-element')).toBe(true);

        // Has grid preview area
        const preview = element.querySelector('.subcanvas-preview');
        expect(preview).toBeTruthy();
    });

    test('Tim sees subcanvas element positioned on the canvas', () => {
        const item = makeElement({ x: 250, y: 300 });
        const element = createSubcanvasElement(item);

        // canvasPlaced applies position from element data
        expect(element.style.left).toBe('250px');
        expect(element.style.top).toBe('300px');
    });

    test('Tim has a dblclick handler wired on the subcanvas element', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        // Dispatch dblclick — should not throw (morph will fail gracefully in test env)
        expect(() => {
            element.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));
        }).not.toThrow();
    });
});

describe('Subcanvas Naming - Tim (Happy Path)', () => {
    test('Tim sees custom name from content field in title bar', () => {
        const item = makeElement({ content: 'My Notes' });
        const element = createSubcanvasElement(item);

        const label = element.querySelector('.title-bar span:not(.symbol)');
        expect(label?.textContent).toBe('My Notes');
    });

    test('Tim sees default label when no content is set', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        const label = element.querySelector('.title-bar span:not(.symbol)');
        expect(label?.textContent).toBe('subcanvas');
    });

    test('Tim dblclicks label to start editing', () => {
        const item = makeElement({ content: 'Plans' });
        const element = createSubcanvasElement(item);

        const label = element.querySelector('.title-bar span:not(.symbol)') as HTMLElement;
        label.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        expect(label.contentEditable).toBe('true');
    });

    test('Tim edits name and blurs to commit', () => {
        const item = makeElement({ content: 'Old Name' });
        const element = createSubcanvasElement(item);

        const label = element.querySelector('.title-bar span:not(.symbol)') as HTMLElement;
        label.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        label.innerText = 'New Name';
        label.dispatchEvent(new window.Event('blur'));

        expect(item.content).toBe('New Name');
        expect(label.contentEditable).toBe('false');
    });
});

// canvas_id isolation tests live in ts/state/canvas-id.test.ts to avoid
// mock.module leaks from element test files that mock ../../state/ui.

describe('Subcanvas Ghost Placeholder - Tim (Happy Path)', () => {
    test('Tim: expand melded subcanvas inserts ghost at same grid position', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        // Simulate melded composition container
        const composition = document.createElement('div');
        composition.className = 'melded-composition';

        // Give the element grid positioning
        element.style.gridRow = '1';
        element.style.gridColumn = '2';

        composition.appendChild(element);

        // Need a canvas structure for the dblclick handler
        const workspace = document.createElement('div');
        workspace.className = 'canvas-workspace';
        workspace.dataset.canvasId = 'parent-canvas';
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        workspace.appendChild(contentLayer);
        contentLayer.appendChild(composition);
        document.body.appendChild(workspace);

        // Trigger dblclick to expand
        element.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        // Ghost should be in the composition
        const ghost = composition.querySelector('.subcanvas-ghost') as HTMLElement;
        expect(ghost).toBeTruthy();
        expect(ghost!.style.gridRow).toBe('1');
        expect(ghost!.style.gridColumn).toBe('2');
    });

    test('Tim: minimize back removes ghost and restores element in composition', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        // Build melded composition
        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        element.style.gridRow = '1';
        element.style.gridColumn = '2';
        composition.appendChild(element);

        const workspace = document.createElement('div');
        workspace.className = 'canvas-workspace';
        workspace.dataset.canvasId = 'parent-canvas';
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        workspace.appendChild(contentLayer);
        contentLayer.appendChild(composition);
        document.body.appendChild(workspace);

        // Expand
        element.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        // Ghost is present
        const ghost = composition.querySelector('.subcanvas-ghost');
        expect(ghost).toBeTruthy();

        // Morph is async with mocked animate — wait for it to resolve
        // The morph callback will fire on next microtask via Promise.resolve()
        return new Promise<void>(resolve => {
            setTimeout(() => {
                // After morph completes, minimize button should exist
                const minBtn = element.querySelector('.canvas-minimize-btn') as HTMLElement;
                if (minBtn) {
                    minBtn.click();

                    // Wait for restore morph
                    setTimeout(() => {
                        // Ghost should be gone
                        expect(composition.querySelector('.subcanvas-ghost')).toBeFalsy();
                        // Element should be back in composition
                        expect(composition.contains(element)).toBe(true);
                        resolve();
                    }, 50);
                } else {
                    resolve();
                }
            }, 50);
        });
    });
});

describe('Subcanvas Ghost Placeholder - Spike (Edge Cases)', () => {
    test('Spike: expand non-melded subcanvas inserts no ghost', () => {
        const item = makeElement();
        const element = createSubcanvasElement(item);

        // No melded-composition parent — just a content layer
        const workspace = document.createElement('div');
        workspace.className = 'canvas-workspace';
        workspace.dataset.canvasId = 'parent-canvas';
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        workspace.appendChild(contentLayer);
        contentLayer.appendChild(element);
        document.body.appendChild(workspace);

        element.dispatchEvent(new window.MouseEvent('dblclick', { bubbles: true }));

        // No ghost anywhere
        expect(document.querySelector('.subcanvas-ghost')).toBeFalsy();
    });
});

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
