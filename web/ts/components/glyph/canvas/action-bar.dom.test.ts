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

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>');
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.navigator = window.navigator as any;
    globalThis.AbortController = window.AbortController as any;
    globalThis.AbortSignal = window.AbortSignal as any;

    // Mock WAAPI methods (not available in JSDOM)
    window.Element.prototype.animate = function () {
        return { onfinish: null, finished: Promise.resolve() } as any;
    };
    window.Element.prototype.getAnimations = function () {
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
            ['glyph-1', 'glyph-2'],
            container,
            () => { },
            () => { }
        );

        const actionBar = container.querySelector('.canvas-action-bar') as HTMLElement;
        expect(actionBar).not.toBeNull();
        expect(actionBar.style.position).toBe('absolute');
        expect(actionBar.style.left).toBe('50%');
    });

    test('shows delete button for selected glyphs', () => {
        showActionBar(
            ['glyph-1'],
            container,
            () => { },
            () => { }
        );

        const deleteBtn = container.querySelector('.canvas-action-delete');
        expect(deleteBtn).not.toBeNull();
        expect(deleteBtn?.getAttribute('data-tooltip')).toContain('Delete');
    });

    test('removes action bar from DOM when hidden', () => {
        showActionBar(
            ['glyph-1'],
            container,
            () => { },
            () => { }
        );

        let actionBar = container.querySelector('.canvas-action-bar');
        expect(actionBar).not.toBeNull();

        hideActionBar(container);

        // Action bar should be removed (after animation or immediately)
        setTimeout(() => {
            actionBar = container.querySelector('.canvas-action-bar');
            expect(actionBar).toBeNull();
        }, 100);
    });
});

describe('Rectangle Selection - Tim (Happy Path)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let canvas: HTMLElement;
    let selectedGlyphIds: string[];

    beforeEach(() => {
        document.body.innerHTML = '';
        selectedGlyphIds = [];

        canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.setAttribute('data-glyph-id', 'canvas-workspace');
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

    function createMockGlyph(id: string, x: number, y: number, width = 100, height = 100): HTMLElement {
        const glyph = document.createElement('div');
        glyph.className = 'canvas-ax-glyph canvas-glyph';
        glyph.setAttribute('data-glyph-id', id);
        glyph.style.position = 'absolute';
        glyph.style.left = `${x}px`;
        glyph.style.top = `${y}px`;
        glyph.style.width = `${width}px`;
        glyph.style.height = `${height}px`;

        // Mock getBoundingClientRect for JSDOM
        glyph.getBoundingClientRect = () => ({
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

        return glyph;
    }

    function mockSelectGlyph(glyphId: string, _container: HTMLElement, addToSelection: boolean): void {
        if (addToSelection) {
            if (!selectedGlyphIds.includes(glyphId)) {
                selectedGlyphIds.push(glyphId);
            }
        } else {
            selectedGlyphIds = [glyphId];
        }

        const el = canvas.querySelector(`[data-glyph-id="${glyphId}"]`);
        if (el) {
            el.classList.add('canvas-glyph-selected');
        }
    }

    function mockDeselectAll(_container: HTMLElement): void {
        selectedGlyphIds = [];
        canvas.querySelectorAll('.canvas-glyph-selected').forEach(el => {
            el.classList.remove('canvas-glyph-selected');
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

    test('Tim selects a single glyph by dragging rectangle over it', () => {
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);
        const ax1 = createMockGlyph('ax1', 100, 100);
        canvas.appendChild(ax1);

        // Drag from (50, 50) to (250, 250) - covers the glyph at (100, 100, 100x100)
        simulateDrag(50, 50, 250, 250);

        expect(selectedGlyphIds).toEqual(['ax1']);
        expect(ax1.classList.contains('canvas-glyph-selected')).toBe(true);
    });

    test('Tim selects multiple glyphs with one drag', () => {
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);
        const ax1 = createMockGlyph('ax1', 100, 100);
        const ax2 = createMockGlyph('ax2', 300, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);

        // Drag from (50, 50) to (450, 250) - covers both glyphs
        simulateDrag(50, 50, 450, 250);

        expect(selectedGlyphIds).toContain('ax1');
        expect(selectedGlyphIds).toContain('ax2');
        expect(selectedGlyphIds.length).toBe(2);
    });

    test('Tim adds to existing selection with shift+drag', () => {
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);
        const ax1 = createMockGlyph('ax1', 100, 100);
        const ax2 = createMockGlyph('ax2', 300, 100);
        const ax3 = createMockGlyph('ax3', 500, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);
        canvas.appendChild(ax3);

        // First selection without shift
        simulateDrag(50, 50, 250, 250, false);
        expect(selectedGlyphIds).toEqual(['ax1']);

        // Second selection with shift
        simulateDrag(450, 50, 650, 250, true);
        expect(selectedGlyphIds).toContain('ax1');
        expect(selectedGlyphIds).toContain('ax3');
        expect(selectedGlyphIds.length).toBe(2);
    });

    test('Tim replaces selection when dragging without shift', () => {
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);
        const ax1 = createMockGlyph('ax1', 100, 100);
        const ax2 = createMockGlyph('ax2', 300, 100);
        canvas.appendChild(ax1);
        canvas.appendChild(ax2);

        // First selection
        simulateDrag(50, 50, 250, 250);
        expect(selectedGlyphIds).toEqual(['ax1']);

        // Second selection without shift - should replace
        simulateDrag(250, 50, 450, 250);
        expect(selectedGlyphIds).toEqual(['ax2']);
        expect(selectedGlyphIds).not.toContain('ax1');
    });
});

describe('Rectangle Selection - Spike (Edge Cases)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let canvas: HTMLElement;
    let selectedGlyphIds: string[];

    beforeEach(() => {
        document.body.innerHTML = '';
        selectedGlyphIds = [];

        canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.setAttribute('data-glyph-id', 'canvas-workspace');
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

    function createMockGlyph(id: string, x: number, y: number, width = 100, height = 100): HTMLElement {
        const glyph = document.createElement('div');
        glyph.className = 'canvas-ax-glyph canvas-glyph';
        glyph.setAttribute('data-glyph-id', id);
        glyph.style.position = 'absolute';
        glyph.style.left = `${x}px`;
        glyph.style.top = `${y}px`;
        glyph.style.width = `${width}px`;
        glyph.style.height = `${height}px`;

        glyph.getBoundingClientRect = () => ({
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

        return glyph;
    }

    function mockSelectGlyph(glyphId: string, _container: HTMLElement, addToSelection: boolean): void {
        if (addToSelection) {
            if (!selectedGlyphIds.includes(glyphId)) {
                selectedGlyphIds.push(glyphId);
            }
        } else {
            selectedGlyphIds = [glyphId];
        }

        const el = canvas.querySelector(`[data-glyph-id="${glyphId}"]`);
        if (el) {
            el.classList.add('canvas-glyph-selected');
        }
    }

    function mockDeselectAll(_container: HTMLElement): void {
        selectedGlyphIds = [];
        canvas.querySelectorAll('.canvas-glyph-selected').forEach(el => {
            el.classList.remove('canvas-glyph-selected');
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
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);
        const ax1 = createMockGlyph('ax1', 100, 100);
        canvas.appendChild(ax1);

        // Drag in empty area - doesn't cover any glyphs
        simulateDrag(400, 400, 500, 500);

        expect(selectedGlyphIds).toEqual([]);
    });

    test('Spike verifies composition container exclusion logic', () => {
        setupRectangleSelection(canvas, mockSelectGlyph, mockDeselectAll);

        // Create melded composition
        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        composition.setAttribute('data-glyph-id', 'melded-ax1-py1');
        composition.style.position = 'absolute';
        composition.style.left = '100px';
        composition.style.top = '100px';

        const ax1 = createMockGlyph('ax1', 0, 0);
        const py1 = createMockGlyph('py1', 150, 0);
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

        // Drag over composition - should select individual glyphs, not container
        simulateDrag(50, 50, 400, 250);

        // Should select the individual glyphs
        expect(selectedGlyphIds).toContain('ax1');
        expect(selectedGlyphIds).toContain('py1');
        // Should NOT select the composition container
        expect(selectedGlyphIds).not.toContain('melded-ax1-py1');
    });
});
