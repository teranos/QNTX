/**
 * A published element is not a plugin.
 *
 * It is an attestation this node serves from /g/. There is no process, no
 * am.toml line and nothing to enable. Registering one as a plugin put
 * plugin_name in its canvas record, and on the next page load — before
 * discovery had run — the canvas read that field and drew a placeholder saying
 * the plugin was not enabled, with an am.toml snippet to paste.
 *
 * That is worse than saying nothing: it sends whoever reads it to edit a file
 * that has nothing to do with the element in front of them.
 */

import { describe, test, expect } from 'bun:test';
import { registerElementType, replacePluginElementType, getElementTypeBySymbol } from './element-registry';
import type { Element } from '@teranos/elements';

const said = (what: string) => () => {
    const el = document.createElement('div');
    el.textContent = what;
    return el;
};

function published(symbol: string, name: string, what: string) {
    return {
        symbol,
        className: `canvas-published-element element-${name}`,
        title: 'CRIER',
        label: name,
        publishedName: name,
        render: said(what) as unknown as (item: Element) => HTMLElement,
    };
}

function plugin(symbol: string, name: string, what: string) {
    return {
        symbol,
        className: `canvas-plugin-element plugin-${name}`,
        title: 'Capy',
        label: name,
        pluginName: name,
        render: said(what) as unknown as (item: Element) => HTMLElement,
    };
}

describe('a published element', () => {
    test('is registered without claiming to be a plugin', () => {
        const symbol = '\u{1F4EF}'; // 📯
        registerElementType(published(symbol, 'crier', 'first'));

        const entry = getElementTypeBySymbol(symbol);
        expect(entry?.publishedName).toBe('crier');
        // What the canvas persists as plugin_name, and reads back to decide
        // whether to talk about plugins at all.
        expect(entry?.pluginName).toBeUndefined();
    });

    test('is replaced by a later publish of the same element', () => {
        const symbol = '\u{1F4E1}'; // 📡
        registerElementType(published(symbol, 'radar', 'first'));

        expect(replacePluginElementType(published(symbol, 'radar', 'second'))).toBe(true);
        expect(getElementTypeBySymbol(symbol)?.render({} as Element)).toBeDefined();
    });

    test('cannot be taken over by a plugin of the same name', () => {
        const symbol = '\u{1F50E}'; // 🔎
        registerElementType(published(symbol, 'finder', 'published'));

        expect(replacePluginElementType(plugin(symbol, 'finder', 'plugin'))).toBe(false);
    });

    test('cannot take over a plugin element of the same name', () => {
        const symbol = '\u{1F9EA}'; // 🧪
        registerElementType(plugin(symbol, 'lab', 'plugin'));

        expect(replacePluginElementType(published(symbol, 'lab', 'published'))).toBe(false);
    });

    test('cannot take over a built-in', () => {
        // AX is a built-in and has no provider.
        expect(replacePluginElementType(published('⋈', 'ax', 'mine'))).toBe(false);
    });
});
