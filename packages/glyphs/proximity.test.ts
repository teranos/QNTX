/**
 * Tests for configurable dot geometry
 *
 * The dot is the glyph at rest. Its size used to be five private constants
 * inside GlyphProximity, written inline on every animation frame — unreachable
 * from a host app and unreachable from CSS (inline styles win). These tests pin
 * the config surface that replaced them.
 *
 * Personas:
 * - Tim: defaults and overrides through configureGlyphs
 * - Spike: config arrives after the proximity engine already exists
 * - Jenny: the expanded dot renders glyph.symbol natively (SYMRD)
 */

import { describe, test, expect, beforeAll, afterAll, afterEach } from 'bun:test';
import { configureGlyphs, getDotGeometry } from './config';
import { GlyphProximity, applyRestingDotGeometry } from './proximity';
import { glyphRun } from './run';
import { resetGlyphElement } from './manifestations/morphology';
import type { Glyph } from './glyph';

/** The geometry the hardcoded constants had. Changing these is a breaking change. */
const HISTORICAL = {
    minWidth: 10,
    minHeight: 10,
    maxWidth: 220,
    maxHeight: 32,
    borderRadiusMax: 2,
};

// happy-dom does not put requestAnimationFrame on globalThis; updateProximity
// needs it. Run frame callbacks synchronously so assertions can read the result.
const realRAF = globalThis.requestAnimationFrame;
const realCAF = globalThis.cancelAnimationFrame;

beforeAll(() => {
    globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
        cb(0);
        return 1;
    }) as typeof requestAnimationFrame;
    globalThis.cancelAnimationFrame = (() => {}) as typeof cancelAnimationFrame;
});

afterAll(() => {
    globalThis.requestAnimationFrame = realRAF;
    globalThis.cancelAnimationFrame = realCAF;
});

// Config is module-global — hand it back to the historical values so test order
// and other test files are unaffected.
afterEach(() => {
    configureGlyphs({ dotGeometry: HISTORICAL });
    document.body.innerHTML = '';
});

/** A tray container holding one dot, as updateProximity expects to find it. */
function makeTray(): { container: HTMLElement; dot: HTMLElement } {
    const container = document.createElement('div');
    const dot = document.createElement('div');
    dot.className = 'glyph-run-glyph';
    container.appendChild(dot);
    document.body.appendChild(container);
    return { container, dot };
}

const NO_ITEMS = new Map<string, Glyph>();

// ── Tim (happy path) ────────────────────────────────────────────────

describe('Tim: dot geometry config', () => {
    // MUST stay first: proves the untouched defaults, before any configureGlyphs call.
    test('defaults match the previously hardcoded constants', () => {
        expect(getDotGeometry()).toEqual(HISTORICAL);
    });

    test('minWidth overridable, rest fall back to defaults', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, minWidth: 16 });
    });

    test('minHeight overridable, rest fall back to defaults', () => {
        configureGlyphs({ dotGeometry: { minHeight: 18 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, minHeight: 18 });
    });

    test('maxWidth overridable, rest fall back to defaults', () => {
        configureGlyphs({ dotGeometry: { maxWidth: 400 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, maxWidth: 400 });
    });

    test('maxHeight overridable, rest fall back to defaults', () => {
        configureGlyphs({ dotGeometry: { maxHeight: 48 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, maxHeight: 48 });
    });

    test('borderRadiusMax overridable, rest fall back to defaults', () => {
        configureGlyphs({ dotGeometry: { borderRadiusMax: 6 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, borderRadiusMax: 6 });
    });

    test('zero is a literal value, not "unset"', () => {
        configureGlyphs({ dotGeometry: { borderRadiusMax: 0 } });
        expect(getDotGeometry().borderRadiusMax).toBe(0);
    });

    test('a second call merges, it does not replace', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16 } });
        configureGlyphs({ dotGeometry: { maxWidth: 400 } });
        expect(getDotGeometry()).toEqual({ ...HISTORICAL, minWidth: 16, maxWidth: 400 });
    });

    test('configureGlyphs without dotGeometry leaves geometry untouched', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16 } });
        configureGlyphs({ logSegment: 'TEST' });
        expect(getDotGeometry().minWidth).toBe(16);
    });

    test('resting geometry is applied to an element from config', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16, minHeight: 18, borderRadiusMax: 6 } });
        const el = document.createElement('div');
        applyRestingDotGeometry(el);
        expect(el.style.width).toBe('16px');
        expect(el.style.height).toBe('18px');
        expect(el.style.borderRadius).toBe('6px');
    });
});

// ── Spike (edge cases) ──────────────────────────────────────────────

describe('Spike: geometry is read at use time', () => {
    test('an engine built before configureGlyphs still uses the new geometry', () => {
        // Engine exists first — it must not capture geometry at construction.
        const proximity = new GlyphProximity();

        configureGlyphs({
            dotGeometry: { minWidth: 16, minHeight: 18, maxWidth: 400, maxHeight: 48, borderRadiusMax: 6 },
        });

        const { container, dot } = makeTray();

        // Pointer far away → proximity 0 → resting size.
        proximity.setPointerPosition(10000, 10000);
        proximity.updateProximity(container, NO_ITEMS, false);

        expect(dot.style.width).toBe('16px');
        expect(dot.style.height).toBe('18px');
        expect(dot.style.borderRadius).toBe('6px');
    });

    test('proximity 1 expands to the configured max', () => {
        configureGlyphs({
            dotGeometry: { minWidth: 16, minHeight: 18, maxWidth: 400, maxHeight: 48, borderRadiusMax: 6 },
        });

        const proximity = new GlyphProximity();
        const { container, dot } = makeTray();

        // Element rects are all-zero here, so pointer 0,0 sits inside the dot:
        // distance 0 → proximityRaw 1 → snap to fully expanded.
        proximity.setPointerPosition(0, 0);
        proximity.updateProximity(container, NO_ITEMS, false);

        expect(dot.style.width).toBe('400px');
        expect(dot.style.height).toBe('48px');
        expect(dot.style.borderRadius).toBe('0px');
    });

    test('a dot is born at the configured resting size, not the CSS size', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16, minHeight: 18, borderRadiusMax: 6 } });

        const item: Glyph = { id: 'dot-geometry-1', title: 'Dot Geometry', symbol: 'ax' };
        glyphRun.add(item, true);
        const dot = document.querySelector('[data-glyph-id="dot-geometry-1"]') as HTMLElement;

        expect(dot).not.toBeNull();
        expect(dot.style.width).toBe('16px');
        expect(dot.style.height).toBe('18px');

        glyphRun.remove('dot-geometry-1');
    });

    test('a dot returning to rest is re-sized from config after its styles are wiped', () => {
        configureGlyphs({ dotGeometry: { minWidth: 16, minHeight: 18, borderRadiusMax: 6 } });

        const item: Glyph = { id: 'dot-geometry-2', title: 'Dot Geometry', symbol: 'ax' };
        const el = document.createElement('div');
        el.style.width = '600px';
        el.style.height = '400px';
        document.body.appendChild(el);

        resetGlyphElement(el, item, 'test', () => {});

        expect(el.style.width).toBe('16px');
        expect(el.style.height).toBe('18px');
        expect(el.style.borderRadius).toBe('6px');
    });

    test('unconfigured geometry still morphs 10px → 220px', () => {
        const proximity = new GlyphProximity();
        const { container, dot } = makeTray();

        proximity.setPointerPosition(10000, 10000);
        proximity.updateProximity(container, NO_ITEMS, false);
        expect(dot.style.width).toBe('10px');
        expect(dot.style.height).toBe('10px');
        expect(dot.style.borderRadius).toBe('2px');

        proximity.setPointerPosition(0, 0);
        proximity.updateProximity(container, NO_ITEMS, false);
        expect(dot.style.width).toBe('220px');
        expect(dot.style.height).toBe('32px');
        expect(dot.style.borderRadius).toBe('0px');
    });
});

// ── Jenny (symbol in the expanded dot, SYMRD) ───────────────────────

describe('Jenny: the expanded dot shows the symbol', () => {
    /** A tray with one dot bound to an item, as the engine finds them. */
    function trayWithItem(item: Glyph): { container: HTMLElement; dot: HTMLElement; items: Map<string, Glyph> } {
        const { container, dot } = makeTray();
        dot.dataset.glyphId = item.id;
        return { container, dot, items: new Map([[item.id, item]]) };
    }

    // The tray dot used to have no symbol resting or expanded — hosts glued
    // it to the front of the title string. The engine renders it natively now.
    test('symbol and title together', () => {
        const proximity = new GlyphProximity();
        const { container, dot, items } = trayWithItem({
            id: 'sym-dot-1',
            title: 'Self',
            symbol: '⍟',
            renderContent: () => document.createElement('div'),
        });

        proximity.setPointerPosition(0, 0);
        proximity.updateProximity(container, items, false);

        expect(dot.textContent).toBe('⍟ Self');
    });

    test('no symbol, the title alone', () => {
        const proximity = new GlyphProximity();
        const { container, dot, items } = trayWithItem({
            id: 'sym-dot-2',
            title: 'Handlers',
            renderContent: () => document.createElement('div'),
        });

        proximity.setPointerPosition(0, 0);
        proximity.updateProximity(container, items, false);

        expect(dot.textContent).toBe('Handlers');
    });
});
