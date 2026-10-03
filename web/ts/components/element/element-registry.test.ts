/**
 * Tests for replacing a plugin element type whose module was replaced on disk.
 *
 * The entry registered from a module closes over that module, so an element whose
 * file changed keeps rendering the old one until its entry is replaced.
 */

import { describe, test, expect } from 'bun:test';
import {
    registerElementType,
    replacePluginElementType,
    getElementTypeBySymbol,
    getAllElementTypes,
    holderOf,
} from './element-registry';
import type { Element } from '@teranos/elements';

const said = (what: string) => () => {
    const el = document.createElement('div');
    el.textContent = what;
    return el;
};

function entry(symbol: string, plugin: string | undefined, what: string) {
    return {
        symbol,
        className: `canvas-plugin-element plugin-${plugin ?? 'none'}`,
        title: 'Chart',
        label: 'chart',
        pluginName: plugin,
        render: said(what) as unknown as (item: Element) => HTMLElement,
    };
}

describe('replacePluginElementType', () => {
    test('a plugin element is replaced by the same plugin', () => {
        const symbol = '\u{1F5FA}';
        registerElementType(entry(symbol, 'chart', 'first'));

        const replaced = replacePluginElementType(entry(symbol, 'chart', 'second'));

        expect(replaced).toBe(true);
        const now = getElementTypeBySymbol(symbol);
        expect(now).toBeDefined();
        expect((now!.render({} as Element) as HTMLElement).textContent).toBe('second');
    });

    test('replacing does not leave a second entry behind', () => {
        const symbol = '\u{1F4D0}';
        registerElementType(entry(symbol, 'ruler', 'first'));
        const before = getAllElementTypes().filter(e => e.symbol === symbol).length;

        replacePluginElementType(entry(symbol, 'ruler', 'second'));

        expect(before).toBe(1);
        expect(getAllElementTypes().filter(e => e.symbol === symbol).length).toBe(1);
    });

    test('a built-in element is not replaceable by a plugin route', () => {
        // 'py' is built in, so it carries no pluginName.
        const replaced = replacePluginElementType(entry('py', 'chart', 'hijacked'));

        expect(replaced).toBe(false);
        expect((getElementTypeBySymbol('py') as { pluginName?: string }).pluginName).toBeUndefined();
    });

    test('one plugin cannot take over another plugin symbol', () => {
        const symbol = '\u{1F9ED}';
        registerElementType(entry(symbol, 'compass', 'first'));

        const replaced = replacePluginElementType(entry(symbol, 'other', 'second'));

        expect(replaced).toBe(false);
        expect((getElementTypeBySymbol(symbol) as HTMLElement & { pluginName?: string }).pluginName).toBe('compass');
    });

    test('a symbol nobody registered is not replaced into existence', () => {
        expect(replacePluginElementType(entry('\u{1F6F8}', 'ufo', 'first'))).toBe(false);
        expect(getElementTypeBySymbol('\u{1F6F8}')).toBeUndefined();
    });
});

// "Make sure ui logs can't lie or become stale": a refusal names what holds the
// symbol, read from the registry when it is said.
describe('holderOf', () => {
    test('a built-in is named as the built-in it is', () => {
        expect(holderOf('⋈')).toBe('the built-in AX Query');
    });

    test('a plugin is named with the element it registered', () => {
        const symbol = '\u{1F52D}';
        registerElementType(entry(symbol, 'telescope', 'first'));
        expect(holderOf(symbol)).toBe('plugin telescope, as Chart');
    });

    test('a published element is named as published', () => {
        const symbol = '\u{1F9F6}';
        registerElementType({ ...entry(symbol, undefined, 'first'), className: 'canvas-published-element element-yarn', publishedName: 'yarn' });
        expect(holderOf(symbol)).toBe('published element yarn');
    });

    test('a replaced holder is named as it is now, not as it was', () => {
        const symbol = '\u{1F9EE}';
        registerElementType({ ...entry(symbol, 'abacus', 'first'), title: 'Old' });
        replacePluginElementType({ ...entry(symbol, 'abacus', 'second'), title: 'New' });
        expect(holderOf(symbol)).toBe('plugin abacus, as New');
    });

    test('a symbol nobody holds says so', () => {
        expect(holderOf('\u{1F6F8}')).toBe('nothing');
    });
});
