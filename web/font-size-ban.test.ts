// "There's no doubt in my mind that Elements owns it's own font sizes."
// "I WANT QNTX WEB TO FAIL"
// A stylesheet picks a size by the package's token; any other size is named, file and line.

// Personas:
// - Tim: Happy path — no size, or a size picked by its token, passes; a size of its own is named
// - Spike: Edge cases — the shorthand, a token off the package's number, a token the package has not

import { describe, test, expect } from 'bun:test';
import { TEXT_SIZES, FIELD_TEXT_SIZE } from '@teranos/elements';
import { sizesIn, sizesUnder } from './font-size-ban';

describe('Tim: a stylesheet', () => {
    test('with no size, nothing is named', () => {
        expect(sizesIn('.row {\n    color: red;\n}\n')).toEqual([]);
    });

    test('with a size of its own, its line is named', () => {
        expect(sizesIn('.row {\n    color: red;\n    font-size: 30px;\n}\n')).toEqual([3]);
    });

    test('a size picked by its token is the package\'s', () => {
        expect(sizesIn('.row {\n    font-size: var(--font-size-sm);\n}')).toEqual([]);
        expect(sizesIn('.row { color: red; font-size: var(--font-size-field); }')).toEqual([]);
    });

    test('a token saying the package\'s number is the package\'s', () => {
        expect(sizesIn(`:root {\n    --font-size-md: ${TEXT_SIZES.md}px;\n    --font-size-field: ${FIELD_TEXT_SIZE}px;\n}`)).toEqual([]);
    });
});

describe('Spike: what is a size of its own', () => {
    test('the font shorthand carries a size', () => {
        expect(sizesIn('.row { font: 700 11px/1 var(--font-mono); }')).toEqual([1]);
    });

    test('inherit is no size of its own: the text stands at the size around it', () => {
        expect(sizesIn('.row {\n    font: inherit;\n    font-size: inherit;\n}')).toEqual([]);
    });

    test('a transition naming font-size sizes nothing', () => {
        expect(sizesIn('.row { transition: color 0.3s ease, font-size 0.3s ease; }')).toEqual([]);
    });

    test('in markup, a style attribute is read the same', () => {
        expect(sizesIn('<div style="font-size: var(--font-size-lg); margin: 0;">Loading</div>')).toEqual([]);
        expect(sizesIn('<span style="font-size: var(--font-size-xs)"></span>')).toEqual([]);
        expect(sizesIn('<div style="color: red; font-size: 12px">x</div>')).toEqual([1]);
    });

    test('a token off the package\'s number is named', () => {
        expect(sizesIn(`:root {\n    --font-size-sm: ${TEXT_SIZES.sm + 1}px;\n}`)).toEqual([2]);
    });

    test('a token the package has not is named, declared or picked', () => {
        expect(sizesIn(':root {\n    --font-size-xl: 18px;\n}')).toEqual([2]);
        expect(sizesIn('.row { font-size: var(--font-size-xl); }')).toEqual([1]);
    });

    test('a token picked with a size of its own behind it is named', () => {
        expect(sizesIn('.row { font-size: var(--font-size-xs, 10px); }')).toEqual([1]);
    });

    test('a comment that says font-size is not one', () => {
        expect(sizesIn('/* font-size is the package\'s */\n.row { color: red; }\n *  font-size: 3px\n')).toEqual([]);
    });

    test('font-family and font-weight are not sizes', () => {
        expect(sizesIn('.row { font-family: var(--font-mono); font-weight: 500; }')).toEqual([]);
    });
});

describe('Jenny: the whole of web/', () => {
    test('no stylesheet and not index.html sizes text of its own', () => {
        expect(sizesUnder(import.meta.dir)).toEqual([]);
    });
});
