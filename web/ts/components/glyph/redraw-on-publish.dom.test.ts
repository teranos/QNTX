/**
 * Tests for a glyph already drawn on the canvas when its module is replaced.
 *
 * Replacing a registry entry reaches the next render. What is already drawn
 * came from the module being replaced and keeps showing it — a publish that
 * changes nothing on the canvas somebody is looking at is the failure this is
 * about.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { registerGlyphType, replacePluginGlyphType, getGlyphTypeBySymbol } from './glyph-registry';
import { redrawPlacedGlyphs } from './canvas/canvas-workspace-builder';
import { uiState } from '../../state/ui';
import { storeCleanup } from '@qntx/glyphs';
import type { Glyph } from '@qntx/glyphs';

const SYMBOL = '\u{1F52C}'; // 🔬, held by no built-in

function entry(saying: string, onTearDown?: () => void) {
    return {
        symbol: SYMBOL,
        className: 'canvas-plugin-glyph plugin-scope',
        title: 'Scope',
        label: 'scope',
        pluginName: 'scope',
        render: ((glyph: Glyph) => {
            const el = document.createElement('div');
            el.dataset.glyphId = glyph.id;
            el.className = 'canvas-plugin-glyph plugin-scope';
            el.textContent = saying;
            if (onTearDown) storeCleanup(el, onTearDown);
            return el;
        }) as unknown as (glyph: Glyph) => HTMLElement,
    };
}

/**
 * Put this module behind the symbol. The registry is one map for the page, so
 * the first test to run registers and every one after it replaces.
 */
function standing(saying: string, onTearDown?: () => void): void {
    const now = entry(saying, onTearDown);
    if (getGlyphTypeBySymbol(SYMBOL)) {
        expect(replacePluginGlyphType(now)).toBe(true);
        return;
    }
    registerGlyphType(now);
}

/** A canvas with one glyph of this type drawn on it, from the entry registered. */
async function canvasHolding(id: string): Promise<HTMLElement> {
    const canvas = document.createElement('div');
    canvas.className = 'canvas-workspace';
    document.body.replaceChildren(canvas);

    uiState.addCanvasGlyph({ id, symbol: SYMBOL, x: 10, y: 20 });

    const el = document.createElement('div');
    el.dataset.glyphId = id;
    el.className = 'canvas-plugin-glyph plugin-scope';
    el.textContent = 'first';
    canvas.appendChild(el);
    return canvas;
}

describe('redrawPlacedGlyphs', () => {
    beforeEach(() => {
        document.body.replaceChildren();
    });

    test('a glyph already on the canvas is drawn again from the module now registered', async () => {
        standing('first');
        const canvas = await canvasHolding('scope-1');

        standing('second');
        const redrawn = await redrawPlacedGlyphs(SYMBOL);

        expect(redrawn).toBe(1);
        const el = canvas.querySelector('[data-glyph-id="scope-1"]');
        expect(el?.textContent).toBe('second');
    });

    test('what the replaced module registered is torn down before its element goes', async () => {
        let tornDown = 0;
        standing('first', () => { tornDown++; });
        await canvasHolding('scope-2');

        // The element the canvas starts with is the test's own, so it is drawn
        // through the entry once to put the module's cleanup on it.
        await redrawPlacedGlyphs(SYMBOL);
        standing('second');
        await redrawPlacedGlyphs(SYMBOL);

        expect(tornDown).toBe(1);
    });

    test('a symbol nothing registered redraws nothing rather than throwing', async () => {
        expect(await redrawPlacedGlyphs('\u{1F6F8}')).toBe(0);
    });
});
