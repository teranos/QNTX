/**
 * Border as visual identity.
 *
 * "When I pick up note glyph as window and minimise it, it never loses
 *  color, i want the border to be treated similarly"
 *
 * Like color, the border lives on the Glyph datum and every manifestation
 * wears it — the dot a glyph minimizes into included.
 *
 * Personas:
 * - Tim: happy path — every manifestation wears the glyph's border
 * - Spike: no border on the datum → the manifestation's own border decides
 * - Jenny: window → tray adopt keeps the border, like color
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { glyphRun } from './run';
import { canvasPlaced } from './manifestations/canvas-placed';
import { resetGlyphElement } from './manifestations/morphology';
import type { Glyph } from './glyph';

const BORDER = '2px dashed red';

function makeGlyph(id: string, overrides: Partial<Glyph> = {}): Glyph {
    return {
        id,
        title: 'Note',
        renderContent: () => document.createElement('div'),
        ...overrides,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: every manifestation wears the border', () => {
    test('a dot born in the tray wears it', () => {
        const item = makeGlyph('border-dot-1', { border: BORDER });
        glyphRun.add(item, true);
        const dot = document.querySelector('[data-glyph-id="border-dot-1"]') as HTMLElement;

        expect(dot.style.border).toBe(BORDER);

        glyphRun.remove('border-dot-1');
    });

    test('a canvas-placed glyph wears it', () => {
        const { element } = canvasPlaced({
            glyph: makeGlyph('border-canvas-1', { border: BORDER }),
            className: 'canvas-test-glyph',
            defaults: { x: 0, y: 0, width: 100, height: 100 },
            logLabel: 'Test',
        });

        expect(element.style.border).toBe(BORDER);
    });

    // The minimize reset wipes cssText — identity comes back from the datum,
    // exactly the way color does.
    test('the dot a glyph resets into wears it', () => {
        const item = makeGlyph('border-reset-1', { border: BORDER, color: '#221100' });
        const el = document.createElement('div');
        el.style.cssText = 'width: 600px; border: 1px solid red;';
        document.body.appendChild(el);

        resetGlyphElement(el, item, 'test', () => {});

        expect(el.style.border).toBe(BORDER);
        expect(el.style.backgroundColor).not.toBe('');
    });
});

describe('Spike: no border on the datum', () => {
    test('a dot without one carries no inline border — CSS decides', () => {
        const item = makeGlyph('border-none-1');
        glyphRun.add(item, true);
        const dot = document.querySelector('[data-glyph-id="border-none-1"]') as HTMLElement;

        expect(dot.style.border).toBe('');

        glyphRun.remove('border-none-1');
    });
});

describe('Jenny: window → tray, the border never leaves', () => {
    test('adopt reapplies the border after the window wipes styles', () => {
        // The element arrives from minimizeCanvasWindowToTray with cssText
        // already wiped — adopt dresses it from the datum.
        const el = document.createElement('div');
        document.body.appendChild(el);
        const item = makeGlyph('border-adopt-1', { border: BORDER, color: '#221100' });

        glyphRun.adopt(el, item);

        expect(el.style.border).toBe(BORDER);
        expect(el.style.backgroundColor).not.toBe('');
        expect(el.className).toBe('glyph-run-glyph');

        glyphRun.remove('border-adopt-1');
    });
});
