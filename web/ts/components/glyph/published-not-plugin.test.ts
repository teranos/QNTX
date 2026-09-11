/**
 * A published glyph is not a plugin.
 *
 * It is an attestation this node serves from /g/. There is no process, no
 * am.toml line and nothing to enable. Registering one as a plugin put
 * plugin_name in its canvas record, and on the next page load — before
 * discovery had run — the canvas read that field and drew a placeholder saying
 * the plugin was not enabled, with an am.toml snippet to paste.
 *
 * That is worse than saying nothing: it sends whoever reads it to edit a file
 * that has nothing to do with the glyph in front of them.
 */

import { describe, test, expect } from 'bun:test';
import { registerGlyphType, replacePluginGlyphType, getGlyphTypeBySymbol } from './glyph-registry';
import type { Glyph } from '@qntx/glyphs';

const said = (what: string) => () => {
    const el = document.createElement('div');
    el.textContent = what;
    return el;
};

function published(symbol: string, name: string, what: string) {
    return {
        symbol,
        className: `canvas-published-glyph glyph-${name}`,
        title: 'CRIER',
        label: name,
        publishedName: name,
        render: said(what) as unknown as (glyph: Glyph) => HTMLElement,
    };
}

function plugin(symbol: string, name: string, what: string) {
    return {
        symbol,
        className: `canvas-plugin-glyph plugin-${name}`,
        title: 'Capy',
        label: name,
        pluginName: name,
        render: said(what) as unknown as (glyph: Glyph) => HTMLElement,
    };
}

describe('a published glyph', () => {
    test('is registered without claiming to be a plugin', () => {
        const symbol = '\u{1F4EF}'; // 📯
        registerGlyphType(published(symbol, 'crier', 'first'));

        const entry = getGlyphTypeBySymbol(symbol);
        expect(entry?.publishedName).toBe('crier');
        // What the canvas persists as plugin_name, and reads back to decide
        // whether to talk about plugins at all.
        expect(entry?.pluginName).toBeUndefined();
    });

    test('is replaced by a later publish of the same glyph', () => {
        const symbol = '\u{1F4E1}'; // 📡
        registerGlyphType(published(symbol, 'radar', 'first'));

        expect(replacePluginGlyphType(published(symbol, 'radar', 'second'))).toBe(true);
        expect(getGlyphTypeBySymbol(symbol)?.render({} as Glyph)).toBeDefined();
    });

    test('cannot be taken over by a plugin of the same name', () => {
        const symbol = '\u{1F50E}'; // 🔎
        registerGlyphType(published(symbol, 'finder', 'published'));

        expect(replacePluginGlyphType(plugin(symbol, 'finder', 'plugin'))).toBe(false);
    });

    test('cannot take over a plugin glyph of the same name', () => {
        const symbol = '\u{1F9EA}'; // 🧪
        registerGlyphType(plugin(symbol, 'lab', 'plugin'));

        expect(replacePluginGlyphType(published(symbol, 'lab', 'published'))).toBe(false);
    });

    test('cannot take over a built-in', () => {
        // AX is a built-in and has no provider.
        expect(replacePluginGlyphType(published('⋈', 'ax', 'mine'))).toBe(false);
    });
});
