/**
 * A published glyph can hold state on the canvas.
 *
 * Every built-in that persists something reads
 * uiState.getCanvasGlyph(id)?.content and writes it back through the debounced
 * save. A published module reaches neither, so a glyph with anything to
 * remember — a note's text, an editor's code, a doc's file reference — could
 * not leave the shell. content() and saveContent() are that seam.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createGlyphUI } from './glyph-ui';
import { uiState } from '../../state/ui';
import type { Glyph } from '@qntx/glyphs';

const SYMBOL = '\u{1F4D4}'; // 📔

function placed(id: string): Glyph {
    return { id, title: 'Notebook', symbol: SYMBOL, renderContent: () => document.createElement('div') };
}

/** The debounce is 500ms; a test should not wait that long for real. */
const AFTER_DEBOUNCE = 600;

describe('a glyph reading its own content', () => {
    beforeEach(() => {
        document.body.replaceChildren();
    });

    test('reads what the canvas persisted for it', () => {
        uiState.addCanvasGlyph({ id: 'held-1', symbol: SYMBOL, x: 0, y: 0, content: 'written earlier' });

        const ui = createGlyphUI(placed('held-1'), 'notebook');
        expect(ui.content()).toBe('written earlier');
    });

    test('reads nothing when the canvas holds nothing for it', () => {
        const ui = createGlyphUI(placed('held-none'), 'notebook');
        expect(ui.content()).toBeUndefined();
    });
});

describe('a glyph saving its own content', () => {
    test('what it saves is what it reads back', async () => {
        uiState.addCanvasGlyph({ id: 'save-1', symbol: SYMBOL, x: 0, y: 0 });

        const ui = createGlyphUI(placed('save-1'), 'notebook');
        ui.saveContent('typed just now');

        await new Promise(done => setTimeout(done, AFTER_DEBOUNCE));

        expect(uiState.getCanvasGlyph('save-1')?.content).toBe('typed just now');
        expect(createGlyphUI(placed('save-1'), 'notebook').content()).toBe('typed just now');
    });

    test('typing lands once, as the last thing typed', async () => {
        uiState.addCanvasGlyph({ id: 'save-2', symbol: SYMBOL, x: 0, y: 0 });

        const ui = createGlyphUI(placed('save-2'), 'notebook');
        ui.saveContent('a');
        ui.saveContent('ab');
        ui.saveContent('abc');

        await new Promise(done => setTimeout(done, AFTER_DEBOUNCE));

        expect(uiState.getCanvasGlyph('save-2')?.content).toBe('abc');
    });

    test('the rest of what the canvas holds survives the save', async () => {
        uiState.addCanvasGlyph({ id: 'save-3', symbol: SYMBOL, x: 42, y: 99, width: 300, height: 200 });

        createGlyphUI(placed('save-3'), 'notebook').saveContent('body');
        await new Promise(done => setTimeout(done, AFTER_DEBOUNCE));

        const held = uiState.getCanvasGlyph('save-3');
        expect(held?.x).toBe(42);
        expect(held?.y).toBe(99);
        expect(held?.width).toBe(300);
        expect(held?.content).toBe('body');
    });

    test('a glyph that never saves leaves the canvas as it was', async () => {
        uiState.addCanvasGlyph({ id: 'save-4', symbol: SYMBOL, x: 0, y: 0, content: 'untouched' });

        createGlyphUI(placed('save-4'), 'notebook');
        await new Promise(done => setTimeout(done, AFTER_DEBOUNCE));

        expect(uiState.getCanvasGlyph('save-4')?.content).toBe('untouched');
    });
});
