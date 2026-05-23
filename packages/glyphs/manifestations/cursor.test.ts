/**
 * Tests for cursor manifestation
 *
 * Personas:
 * - Tim: Happy path — create, attach, prepare, commit
 * - Spike: Edge cases — full cursor→canvas-placed lifecycle
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import {
    createCursorElement,
    prepareCursorForPlacement,
    commitCursorPlacement,
} from './cursor';
import { canvasPlaced } from './canvas-placed';
import { applyCanvasGlyphLayout } from '../canvas-drag';
import type { Glyph } from '../glyph';

let el: HTMLElement;

beforeEach(() => {
    document.body.innerHTML = '';
    el = createCursorElement('ax', 'Ax');
});

describe('Tim: cursor lifecycle', () => {
    test('createCursorElement sets cursor styles', () => {
        expect(el.classList.contains('glyph-cursor')).toBe(true);
        expect(el.style.position).toBe('fixed');
        expect(el.style.pointerEvents).toBe('none');
        expect(el.style.zIndex).toBe('10003');
    });

    test('createCursorElement contains symbol span', () => {
        const sym = el.querySelector('.glyph-cursor-symbol');
        expect(sym).not.toBeNull();
        expect(sym!.textContent).toBe('ax');
    });

    test('prepareCursorForPlacement returns symbol span', () => {
        const sym = prepareCursorForPlacement(el);
        expect(sym).not.toBeNull();
        expect(sym!.textContent).toBe('ax');
    });

    test('commitCursorPlacement strips cursor class', () => {
        commitCursorPlacement(el);
        expect(el.classList.contains('glyph-cursor')).toBe(false);
    });
});

describe('Spike: full cursor → canvas-placed lifecycle', () => {
    test('cursor element survives the full placement flow', () => {
        // 1. Placement mode: cursor element on body, tracking mouse
        document.body.appendChild(el);
        el.style.left = '400px';
        el.style.top = '300px';
        expect(el.parentNode).toBe(document.body);

        // 2. User clicks: prepareCursorForPlacement extracts symbol
        const symbolSpan = prepareCursorForPlacement(el);
        expect(symbolSpan).not.toBeNull();

        // 3. spawnGlyph creates glyph with cursorElement + symbolElement
        const glyph: Glyph = {
            id: 'test-ax-001',
            title: 'AX Query',
            symbol: 'ax',
            x: 120,
            y: 80,
            cursorElement: el,
            symbolElement: symbolSpan!,
        };

        // 4. entry.render(glyph) calls canvasPlaced — reuses cursorElement
        const { element } = canvasPlaced({
            glyph,
            className: 'canvas-ax-glyph',
            defaults: { x: 200, y: 200, width: 400, height: 200 },
            logLabel: 'AxGlyph',
        });

        // canvasPlaced MUST return the same DOM element
        expect(element).toBe(el);

        // canvasPlaced sets className, wiping cursor class
        expect(element.classList.contains('canvas-glyph')).toBe(true);
        expect(element.classList.contains('glyph-cursor')).toBe(false);

        // applyCanvasGlyphLayout set canvas coordinates
        expect(element.style.left).toBe('120px');
        expect(element.style.top).toBe('80px');

        // But position:fixed from cursor is still inline
        expect(element.style.position).toBe('fixed');

        // 5. morphCursorToPlaced: reparent to canvas
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);
        canvas.appendChild(element);
        expect(element.parentNode).toBe(canvas);

        // 6. Animation plays (can't test in happy-dom), then onfinish:
        commitCursorPlacement(element);

        // After commit: element MUST still be in the canvas
        expect(element.parentNode).toBe(canvas);

        // position:fixed cleared — CSS position:absolute takes over
        expect(element.style.position).toBe('');

        // Canvas layout MUST survive
        expect(element.style.left).toBe('120px');
        expect(element.style.top).toBe('80px');
        expect(element.style.width).toBe('400px');

        // Element must not be hidden
        expect(element.style.display).not.toBe('none');
        expect(element.style.visibility).not.toBe('hidden');
        expect(element.style.opacity).not.toBe('0');

        // Element must have children (title bar, content, etc.)
        expect(element.children.length).toBeGreaterThan(0);
    });
});
