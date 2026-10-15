/**
 * Text size is @teranos/elements', and QNTX web fails to build when it sizes text itself.
 *
 * "There's no doubt in my mind that Elements owns it's own font sizes."
 * "I WANT QNTX WEB TO FAIL"
 *
 * ESLint stops a size written in TypeScript (eslint.config.js, NO_FONT_SIZE). It cannot read a
 * stylesheet; font-size-ban.ts reads every one, and index.html.
 *
 * Personas:
 * - Tim: Happy path — a stylesheet with no size passes; one with font-size is named, file and line
 * - Spike: Edge cases — the font shorthand is a size; a comment saying font-size is not
 * - Jenny: Complex scenarios — the whole of web/ is read, and today it fails
 */

import { describe, test, expect } from 'bun:test';
import { sizesIn, sizesUnder } from './font-size-ban';

describe('Tim: a stylesheet', () => {
    test('with no size, nothing is named', () => {
        expect(sizesIn('.row {\n    color: red;\n}\n')).toEqual([]);
    });

    test('with font-size, its line is named', () => {
        expect(sizesIn('.row {\n    color: red;\n    font-size: 30px;\n}\n')).toEqual([3]);
    });
});

describe('Spike: what is a size', () => {
    test('the font shorthand carries a size', () => {
        expect(sizesIn('.row { font: 700 11px/1 var(--font-mono); }')).toEqual([1]);
        expect(sizesIn('.row {\n    font: inherit;\n}')).toEqual([2]);
    });

    test('a font-size token is a size too', () => {
        expect(sizesIn(':root {\n    --font-size-sm: 11px;\n}')).toEqual([2]);
    });

    test('a comment that says font-size is not one', () => {
        expect(sizesIn('/* font-size is the package\'s */\n.row { color: red; }\n *  font-size: 3px\n')).toEqual([]);
    });

    test('font-family and font-weight are not sizes', () => {
        expect(sizesIn('.row { font-family: var(--font-mono); font-weight: 500; }')).toEqual([]);
    });
});

describe('Jenny: the whole of web/', () => {
    test('every stylesheet and index.html is read, and today a size stands in them', () => {
        const found = sizesUnder(import.meta.dir);
        expect(found.some((at) => at.startsWith('css/tokens.css:'))).toBe(true);
        expect(found.some((at) => at.startsWith('index.html:'))).toBe(true);
        expect(found.some((at) => at.includes('node_modules'))).toBe(false);
    });
});
