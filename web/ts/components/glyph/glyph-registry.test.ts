/**
 * Tests for replacing a plugin glyph type whose module was replaced on disk.
 *
 * The entry registered from a module closes over that module, so a glyph whose
 * file changed keeps rendering the old one until its entry is replaced.
 */

import { describe, test, expect } from 'bun:test';
import {
    registerGlyphType,
    replacePluginGlyphType,
    getGlyphTypeBySymbol,
    getAllGlyphTypes,
} from './glyph-registry';
import type { Glyph } from '@qntx/glyphs';

const said = (what: string) => () => {
    const el = document.createElement('div');
    el.textContent = what;
    return el;
};

function entry(symbol: string, plugin: string | undefined, what: string) {
    return {
        symbol,
        className: `canvas-plugin-glyph plugin-${plugin ?? 'none'}`,
        title: 'Chart',
        label: 'chart',
        pluginName: plugin,
        render: said(what) as unknown as (glyph: Glyph) => HTMLElement,
    };
}

describe('replacePluginGlyphType', () => {
    test('a plugin glyph is replaced by the same plugin', () => {
        const symbol = '\u{1F5FA}';
        registerGlyphType(entry(symbol, 'chart', 'first'));

        const replaced = replacePluginGlyphType(entry(symbol, 'chart', 'second'));

        expect(replaced).toBe(true);
        const now = getGlyphTypeBySymbol(symbol);
        expect(now).toBeDefined();
        expect((now!.render({} as Glyph) as HTMLElement).textContent).toBe('second');
    });

    test('replacing does not leave a second entry behind', () => {
        const symbol = '\u{1F4D0}';
        registerGlyphType(entry(symbol, 'ruler', 'first'));
        const before = getAllGlyphTypes().filter(e => e.symbol === symbol).length;

        replacePluginGlyphType(entry(symbol, 'ruler', 'second'));

        expect(before).toBe(1);
        expect(getAllGlyphTypes().filter(e => e.symbol === symbol).length).toBe(1);
    });

    test('a built-in glyph is not replaceable by a plugin route', () => {
        // 'py' is built in, so it carries no pluginName.
        const replaced = replacePluginGlyphType(entry('py', 'chart', 'hijacked'));

        expect(replaced).toBe(false);
        expect((getGlyphTypeBySymbol('py') as { pluginName?: string }).pluginName).toBeUndefined();
    });

    test('one plugin cannot take over another plugin symbol', () => {
        const symbol = '\u{1F9ED}';
        registerGlyphType(entry(symbol, 'compass', 'first'));

        const replaced = replacePluginGlyphType(entry(symbol, 'other', 'second'));

        expect(replaced).toBe(false);
        expect((getGlyphTypeBySymbol(symbol) as HTMLElement & { pluginName?: string }).pluginName).toBe('compass');
    });

    test('a symbol nobody registered is not replaced into existence', () => {
        expect(replacePluginGlyphType(entry('\u{1F6F8}', 'ufo', 'first'))).toBe(false);
        expect(getGlyphTypeBySymbol('\u{1F6F8}')).toBeUndefined();
    });
});
