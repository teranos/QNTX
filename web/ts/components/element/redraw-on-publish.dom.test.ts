/**
 * Tests for an element already drawn on the canvas when its module is replaced.
 *
 * Replacing a registry entry reaches the next render. What is already drawn
 * came from the module being replaced and keeps showing it — a publish that
 * changes nothing on the canvas somebody is looking at is the failure this is
 * about.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { registerElementType, replacePluginElementType, getElementTypeBySymbol } from './element-registry';
import { redrawPlacedElements } from './canvas/canvas-workspace-builder';
import { uiState } from '../../state/ui';
import { storeCleanup } from '@teranos/elements';
import type { Element } from '@teranos/elements';

const SYMBOL = '\u{1F52C}'; // 🔬, held by no built-in

function entry(saying: string, onTearDown?: () => void) {
    return {
        symbol: SYMBOL,
        className: 'canvas-plugin-element plugin-scope',
        title: 'Scope',
        label: 'scope',
        pluginName: 'scope',
        render: ((item: Element) => {
            const el = document.createElement('div');
            el.dataset.elementId = item.id;
            el.className = 'canvas-plugin-element plugin-scope';
            el.textContent = saying;
            if (onTearDown) storeCleanup(el, onTearDown);
            return el;
        }) as unknown as (item: Element) => HTMLElement,
    };
}

/**
 * Put this module behind the symbol. The registry is one map for the page, so
 * the first test to run registers and every one after it replaces.
 */
function standing(saying: string, onTearDown?: () => void): void {
    const now = entry(saying, onTearDown);
    if (getElementTypeBySymbol(SYMBOL)) {
        expect(replacePluginElementType(now)).toBe(true);
        return;
    }
    registerElementType(now);
}

/** A canvas with one element of this type drawn on it, from the entry registered. */
async function canvasHolding(id: string): Promise<HTMLElement> {
    const canvas = document.createElement('div');
    canvas.className = 'canvas-workspace';
    document.body.replaceChildren(canvas);

    uiState.addCanvasElement({ id, symbol: SYMBOL, x: 10, y: 20 });

    const el = document.createElement('div');
    el.dataset.elementId = id;
    el.className = 'canvas-plugin-element plugin-scope';
    el.textContent = 'first';
    canvas.appendChild(el);
    return canvas;
}

describe('redrawPlacedElements', () => {
    beforeEach(() => {
        document.body.replaceChildren();
    });

    test('an element already on the canvas is drawn again from the module now registered', async () => {
        standing('first');
        const canvas = await canvasHolding('scope-1');

        standing('second');
        const redrawn = await redrawPlacedElements(SYMBOL);

        expect(redrawn).toBe(1);
        const el = canvas.querySelector('[data-element-id="scope-1"]');
        expect(el?.textContent).toBe('second');
    });

    test('what the replaced module registered is torn down before its element goes', async () => {
        let tornDown = 0;
        standing('first', () => { tornDown++; });
        await canvasHolding('scope-2');

        // The element the canvas starts with is the test's own, so it is drawn
        // through the entry once to put the module's cleanup on it.
        await redrawPlacedElements(SYMBOL);
        standing('second');
        await redrawPlacedElements(SYMBOL);

        expect(tornDown).toBe(1);
    });

    test('a symbol nothing registered redraws nothing rather than throwing', async () => {
        expect(await redrawPlacedElements('\u{1F6F8}')).toBe(0);
    });
});
