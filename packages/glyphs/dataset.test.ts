import { describe, test, expect, beforeEach } from 'bun:test';
import {
    isInWindowState,
    setWindowState,
    getLastPosition,
    setLastPosition,
    hasProximityText,
    setProximityText,
    getGlyphId,
    setGlyphId,
    setCanvasOrigin,
    getCanvasOrigin,
    clearCanvasOrigin,
    getGlyphSymbol,
    setGlyphSymbol,
} from './dataset';

let el: HTMLElement;

beforeEach(() => {
    el = document.createElement('div');
});

// ── Tim (happy path) ────────────────────────────────────────────────

describe('Tim: dataset helpers', () => {
    describe('window state', () => {
        test('default is not window state', () => {
            expect(isInWindowState(el)).toBe(false);
        });

        test('set true then check', () => {
            setWindowState(el, true);
            expect(isInWindowState(el)).toBe(true);
        });

        test('set true then false clears', () => {
            setWindowState(el, true);
            setWindowState(el, false);
            expect(isInWindowState(el)).toBe(false);
            expect(el.dataset.windowState).toBeUndefined();
        });
    });

    describe('last position', () => {
        test('no position by default', () => {
            expect(getLastPosition(el)).toBeNull();
        });

        test('set and get', () => {
            setLastPosition(el, 100, 200);
            expect(getLastPosition(el)).toEqual({ x: 100, y: 200 });
        });

        test('fractional coordinates', () => {
            setLastPosition(el, 10.5, 20.75);
            expect(getLastPosition(el)).toEqual({ x: 10.5, y: 20.75 });
        });
    });

    describe('proximity text', () => {
        test('default is false', () => {
            expect(hasProximityText(el)).toBe(false);
        });

        test('set true then check', () => {
            setProximityText(el, true);
            expect(hasProximityText(el)).toBe(true);
        });

        test('set true then false clears', () => {
            setProximityText(el, true);
            setProximityText(el, false);
            expect(hasProximityText(el)).toBe(false);
            expect(el.dataset.hasText).toBeUndefined();
        });
    });

    describe('glyph ID', () => {
        test('no ID by default', () => {
            expect(getGlyphId(el)).toBeNull();
        });

        test('set and get', () => {
            setGlyphId(el, 'py-abc123');
            expect(getGlyphId(el)).toBe('py-abc123');
        });

        test('uses data-glyph-id attribute', () => {
            setGlyphId(el, 'test-id');
            expect(el.getAttribute('data-glyph-id')).toBe('test-id');
        });
    });

    describe('glyph symbol', () => {
        test('no symbol by default', () => {
            expect(getGlyphSymbol(el)).toBeUndefined();
        });

        test('set and get', () => {
            setGlyphSymbol(el, 'PY');
            expect(getGlyphSymbol(el)).toBe('PY');
        });

        test('set undefined clears', () => {
            setGlyphSymbol(el, 'AX');
            setGlyphSymbol(el, undefined);
            expect(getGlyphSymbol(el)).toBeUndefined();
        });
    });

    describe('canvas origin', () => {
        test('no origin by default', () => {
            expect(getCanvasOrigin(el)).toBeNull();
        });

        test('set and get full origin', () => {
            setCanvasOrigin(el, { x: 50, y: 100, width: 300, height: 200, canvasId: 'canvas-1' });
            expect(getCanvasOrigin(el)).toEqual({
                x: 50, y: 100, width: 300, height: 200, canvasId: 'canvas-1',
            });
        });

        test('clear removes all origin data', () => {
            setCanvasOrigin(el, { x: 50, y: 100, width: 300, height: 200, canvasId: 'canvas-1' });
            clearCanvasOrigin(el);
            expect(getCanvasOrigin(el)).toBeNull();
            expect(el.dataset.canvasOriginX).toBeUndefined();
            expect(el.dataset.canvasOriginY).toBeUndefined();
            expect(el.dataset.canvasOriginW).toBeUndefined();
            expect(el.dataset.canvasOriginH).toBeUndefined();
            expect(el.dataset.canvasOriginId).toBeUndefined();
        });
    });
});

// ── Spike (edge cases) ──────────────────────────────────────────────

describe('Spike: dataset edge cases', () => {
    test('getLastPosition returns null for partial data (only x)', () => {
        el.dataset.lastX = '100';
        expect(getLastPosition(el)).toBeNull();
    });

    test('getLastPosition returns null for partial data (only y)', () => {
        el.dataset.lastY = '200';
        expect(getLastPosition(el)).toBeNull();
    });

    test('getCanvasOrigin returns null if canvasId missing', () => {
        el.dataset.canvasOriginX = '10';
        el.dataset.canvasOriginY = '20';
        el.dataset.canvasOriginW = '300';
        el.dataset.canvasOriginH = '200';
        // canvasOriginId not set
        expect(getCanvasOrigin(el)).toBeNull();
    });

    test('getCanvasOrigin returns null if any dimension is NaN', () => {
        setCanvasOrigin(el, { x: 50, y: 100, width: 300, height: 200, canvasId: 'c1' });
        el.dataset.canvasOriginW = 'garbage';
        expect(getCanvasOrigin(el)).toBeNull();
    });

    test('setLastPosition with zero coordinates', () => {
        setLastPosition(el, 0, 0);
        expect(getLastPosition(el)).toEqual({ x: 0, y: 0 });
    });

    test('setLastPosition with negative coordinates', () => {
        setLastPosition(el, -50, -100);
        expect(getLastPosition(el)).toEqual({ x: -50, y: -100 });
    });

    test('setWindowState false on fresh element is a no-op', () => {
        setWindowState(el, false);
        expect(isInWindowState(el)).toBe(false);
    });

    test('overwrite glyph ID', () => {
        setGlyphId(el, 'first');
        setGlyphId(el, 'second');
        expect(getGlyphId(el)).toBe('second');
    });

    test('overwrite canvas origin', () => {
        setCanvasOrigin(el, { x: 1, y: 2, width: 3, height: 4, canvasId: 'old' });
        setCanvasOrigin(el, { x: 10, y: 20, width: 30, height: 40, canvasId: 'new' });
        expect(getCanvasOrigin(el)).toEqual({
            x: 10, y: 20, width: 30, height: 40, canvasId: 'new',
        });
    });
});
