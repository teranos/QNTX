/**
 * @jest-environment jsdom
 *
 * DOM tests for canvas action bar and rectangle selection
 * Focus: DOM structure, positioning, animation behavior, and selection interactions
 *
 * These tests run only in CI with JSDOM environment (gated by USE_JSDOM=1)
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { showActionBar, hideActionBar } from './action-bar';
import { setupRectangleSelection } from './rectangle-selection';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Mock WAAPI methods (not available in JSDOM)
// animate() synchronously calls onfinish so assertions don't need setTimeout
if (USE_JSDOM) {
    const win = globalThis.window as any;
    win.Element.prototype.animate = function () {
        const anim = { onfinish: null as (() => void) | null, finished: Promise.resolve() };
        queueMicrotask(() => { anim.onfinish?.(); });
        return anim;
    };
    win.Element.prototype.getAnimations = function () {
        return [];
    };
}

describe('Canvas Action Bar DOM', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        container.style.position = 'relative';
        container.style.width = '800px';
        container.style.height = '600px';
        document.body.appendChild(container);
    });

    test('creates action bar element with correct positioning', () => {
        showActionBar(
            ['element-1', 'element-2'],
            container,
            () => { },
            () => { }
        );

        const actionBar = container.querySelector('.canvas-action-bar') as HTMLElement;
        expect(actionBar).not.toBeNull();
        expect(actionBar.style.position).toBe('absolute');
        expect(actionBar.style.left).toBe('50%');
    });

    test('shows delete button for selected elements', () => {
        showActionBar(
            ['element-1'],
            container,
            () => { },
            () => { }
        );

        const deleteBtn = container.querySelector('.canvas-action-delete');
        expect(deleteBtn).not.toBeNull();
        expect(deleteBtn?.getAttribute('data-tooltip')).toContain('Delete');
    });

    test('removes action bar from DOM when hidden', async () => {
        showActionBar(
            ['element-1'],
            container,
            () => { },
            () => { }
        );

        const actionBar = container.querySelector('.canvas-action-bar') as HTMLElement;
        expect(actionBar).not.toBeNull();

        // Ensure animate mock fires onfinish (other tests may overwrite prototype)
        actionBar.animate = function () {
            const anim = { onfinish: null as (() => void) | null, finished: Promise.resolve() };
            queueMicrotask(() => { anim.onfinish?.(); });
            return anim as any;
        };
        actionBar.getAnimations = () => [] as any;

        hideActionBar(container);

        await new Promise(resolve => setTimeout(resolve, 10));

        expect(container.querySelector('.canvas-action-bar')).toBeNull();
    });
});

describe('Rectangle Selection - Tim (Happy Path)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let canvas: HTMLElement;
    let selectedElementIds: string[];

    beforeEach(() => {
        document.body.innerHTML = '';
        selectedElementIds = [];

        canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.setAttribute('data-element-id', 'canvas-workspace');
        canvas.style.position = 'relative';
        canvas.style.width = '1000px';
        canvas.style.height = '800px';
        document.body.appendChild(canvas);

        // Mock canvas getBoundingClientRect for JSDOM
        canvas.getBoundingClientRect = () => ({
            left: 0,
            top: 0,
            right: 1000,
            bottom: 800,
            width: 1000,
            height: 800,
            x: 0,
            y: 0,
            toJSON: () => ({})
        } as DOMRect);
    });

    function createMockElement(id: string, x: number, y: number, width = 100, height = 100): HTMLElement {
        const item = document.createElement('div');
        item.className = 'canvas-ax-element canvas-element';
        item.setAttribute('data-element-id', id);
        item.style.position = 'absolute';
        item.style.left = `${x}px`;
        item.style.top = `${y}px`;
        item.style.width = `${width}px`;
        item.style.height = `${height}px`;

        // Mock getBoundingClientRect for JSDOM
        item.getBoundingClientRect = () => ({
            left: x,
            top: y,
            right: x + width,
            bottom: y + height,
            width,
            height,
            x,
            y,
            toJSON: () => ({})
        } as DOMRect);

        return item;
    }

    function mockSelectElement(elementId: string, _container: HTMLElement, addToSelection: boolean): void {
        if (addToSelection) {
            if (!selectedElementIds.includes(elementId)) {
                selectedElementIds.push(elementId);
            }
        } else {
            selectedElementIds = [elementId];
        }

        const el = canvas.querySelector(`[data-element-id="${elementId}"]`);
        if (el) {
            el.classList.add('canvas-element-selected');
        }
    }

    function mockDeselectAll(_container: HTMLElement): void {
        selectedElementIds = [];
        canvas.querySelectorAll('.canvas-element-selected').forEach(el => {
            el.classList.remove('canvas-element-selected');
        });
    }

    function simulateDrag(startX: number, startY: number, endX: number, endY: number, shiftKey = false): void {
        const mousedown = new window.MouseEvent('mousedown', {
            bubbles: true,
            clientX: startX,
            clientY: startY,
            shiftKey
        });
        canvas.dispatchEvent(mousedown);

        const mousemove = new window.MouseEvent('mousemove', {
            bubbles: true,
            clientX: endX,
            clientY: endY,
            shiftKey
        });
        canvas.dispatchEvent(mousemove);

        // Mock getBoundingClientRect on the selection rectangle that was just created
        const selectionRect = canvas.querySelector('.canvas-selection-rectangle') as HTMLElement;
        if (selectionRect) {
            const left = parseFloat(selectionRect.style.left || '0');
            const top = parseFloat(selectionRect.style.top || '0');
            const width = parseFloat(selectionRect.style.width || '0');
            const height = parseFloat(selectionRect.style.height || '0');

            selectionRect.getBoundingClientRect = () => ({
                left,
                top,
                right: left + width,
                bottom: top + height,
                width,
                height,
                x: left,
                y: top,
                toJSON: () => ({})
            } as DOMRect);
        }

        const mouseup = new window.MouseEvent('mouseup', {
            bubbles: true,
            clientX: endX,
            clientY: endY,
            shiftKey
        });
        canvas.dispatchEvent(mouseup);
    }

    test('Tim selects a single element by dragging rectangle over it', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);
        const ax1 = createMockElement('ax1', 100, 100);
        canvas.appendChild(ax1);

        // Drag from (50, 50) to (250, 250) - covers the element at (100, 100, 100x100)
        simulateDrag(50, 50, 250, 250);

        expect(selectedElementIds).toEqual(['ax1']);
        expect(ax1.classList.contains('canvas-element-selected')).toBe(true);
    });

    test('Tim selects multiple elements with one drag', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);
        const ax1 = createMockElement('ax1', 100, 100);
        const ax2 = createMockElement('ax2', 300, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);

        // Drag from (50, 50) to (450, 250) - covers both elements
        simulateDrag(50, 50, 450, 250);

        expect(selectedElementIds).toContain('ax1');
        expect(selectedElementIds).toContain('ax2');
        expect(selectedElementIds.length).toBe(2);
    });

    test('Tim adds to existing selection with shift+drag', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);
        const ax1 = createMockElement('ax1', 100, 100);
        const ax2 = createMockElement('ax2', 300, 100);
        const ax3 = createMockElement('ax3', 500, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);
        canvas.appendChild(ax3);

        // First selection without shift
        simulateDrag(50, 50, 250, 250, false);
        expect(selectedElementIds).toEqual(['ax1']);

        // Second selection with shift
        simulateDrag(450, 50, 650, 250, true);
        expect(selectedElementIds).toContain('ax1');
        expect(selectedElementIds).toContain('ax3');
        expect(selectedElementIds.length).toBe(2);
    });

    test('Tim replaces selection when dragging without shift', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);
        const ax1 = createMockElement('ax1', 100, 100);
        const ax2 = createMockElement('ax2', 300, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);

        // First selection
        simulateDrag(50, 50, 250, 250);
        expect(selectedElementIds).toEqual(['ax1']);

        // Second selection without shift - should replace
        simulateDrag(250, 50, 450, 250);
        expect(selectedElementIds).toEqual(['ax2']);
        expect(selectedElementIds).not.toContain('ax1');
    });
});

describe('Rectangle Selection - Spike (Edge Cases)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let canvas: HTMLElement;
    let selectedElementIds: string[];

    beforeEach(() => {
        document.body.innerHTML = '';
        selectedElementIds = [];

        canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.setAttribute('data-element-id', 'canvas-workspace');
        canvas.style.position = 'relative';
        canvas.style.width = '1000px';
        canvas.style.height = '800px';
        document.body.appendChild(canvas);

        // Mock canvas getBoundingClientRect for JSDOM
        canvas.getBoundingClientRect = () => ({
            left: 0,
            top: 0,
            right: 1000,
            bottom: 800,
            width: 1000,
            height: 800,
            x: 0,
            y: 0,
            toJSON: () => ({})
        } as DOMRect);
    });

    function createMockElement(id: string, x: number, y: number, width = 100, height = 100): HTMLElement {
        const item = document.createElement('div');
        item.className = 'canvas-ax-element canvas-element';
        item.setAttribute('data-element-id', id);
        item.style.position = 'absolute';
        item.style.left = `${x}px`;
        item.style.top = `${y}px`;
        item.style.width = `${width}px`;
        item.style.height = `${height}px`;

        item.getBoundingClientRect = () => ({
            left: x,
            top: y,
            right: x + width,
            bottom: y + height,
            width,
            height,
            x,
            y,
            toJSON: () => ({})
        } as DOMRect);

        return item;
    }

    function mockSelectElement(elementId: string, _container: HTMLElement, addToSelection: boolean): void {
        if (addToSelection) {
            if (!selectedElementIds.includes(elementId)) {
                selectedElementIds.push(elementId);
            }
        } else {
            selectedElementIds = [elementId];
        }

        const el = canvas.querySelector(`[data-element-id="${elementId}"]`);
        if (el) {
            el.classList.add('canvas-element-selected');
        }
    }

    function mockDeselectAll(_container: HTMLElement): void {
        selectedElementIds = [];
        canvas.querySelectorAll('.canvas-element-selected').forEach(el => {
            el.classList.remove('canvas-element-selected');
        });
    }

    function simulateDrag(startX: number, startY: number, endX: number, endY: number, shiftKey = false): void {
        const mousedown = new window.MouseEvent('mousedown', {
            bubbles: true,
            clientX: startX,
            clientY: startY,
            shiftKey
        });
        canvas.dispatchEvent(mousedown);

        const mousemove = new window.MouseEvent('mousemove', {
            bubbles: true,
            clientX: endX,
            clientY: endY,
            shiftKey
        });
        canvas.dispatchEvent(mousemove);

        // Mock getBoundingClientRect on the selection rectangle that was just created
        const selectionRect = canvas.querySelector('.canvas-selection-rectangle') as HTMLElement;
        if (selectionRect) {
            const left = parseFloat(selectionRect.style.left || '0');
            const top = parseFloat(selectionRect.style.top || '0');
            const width = parseFloat(selectionRect.style.width || '0');
            const height = parseFloat(selectionRect.style.height || '0');

            selectionRect.getBoundingClientRect = () => ({
                left,
                top,
                right: left + width,
                bottom: top + height,
                width,
                height,
                x: left,
                y: top,
                toJSON: () => ({})
            } as DOMRect);
        }

        const mouseup = new window.MouseEvent('mouseup', {
            bubbles: true,
            clientX: endX,
            clientY: endY,
            shiftKey
        });
        canvas.dispatchEvent(mouseup);
    }

    test('Spike drags empty rectangle and nothing gets selected', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);
        const ax1 = createMockElement('ax1', 100, 100);
        canvas.appendChild(ax1);

        // Drag in empty area - doesn't cover any elements
        simulateDrag(400, 400, 500, 500);

        expect(selectedElementIds).toEqual([]);
    });

    test('Spike verifies composition container exclusion logic', () => {
        setupRectangleSelection(canvas, mockSelectElement, mockDeselectAll);

        // Create melded composition
        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        composition.setAttribute('data-element-id', 'melded-ax1-py1');
        composition.style.position = 'absolute';
        composition.style.left = '100px';
        composition.style.top = '100px';

        const ax1 = createMockElement('ax1', 0, 0);
        const py1 = createMockElement('py1', 150, 0);
        composition.appendChild(ax1);
        composition.appendChild(py1);
        canvas.appendChild(composition);

        // Mock getBoundingClientRect for composition
        composition.getBoundingClientRect = () => ({
            left: 100,
            top: 100,
            right: 350,
            bottom: 200,
            width: 250,
            height: 100,
            x: 100,
            y: 100,
            toJSON: () => ({})
        } as DOMRect);

        // Drag over composition - should select individual elements, not container
        simulateDrag(50, 50, 400, 250);

        // Should select the individual elements
        expect(selectedElementIds).toContain('ax1');
        expect(selectedElementIds).toContain('py1');
        // Should NOT select the composition container
        expect(selectedElementIds).not.toContain('melded-ax1-py1');
    });
});
