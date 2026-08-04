/**
 * Window corner radius.
 *
 * Personas:
 * - Tim: a host sets it and windows take it
 */

import { describe, test, expect, afterEach } from 'bun:test';
import { configureGlyphs, getWindowBorderRadius } from './config';

const HISTORICAL = '8px';

afterEach(() => {
    configureGlyphs({ windowBorderRadius: HISTORICAL });
});

describe('Tim: window corner radius', () => {
    // "and glyphs as windows still have rounded corners" — said after setting
    // --border-radius: 0 in the example's own stylesheet, which the package
    // overrides inline.
    test('a host can square them', () => {
        configureGlyphs({ windowBorderRadius: '0' });
        expect(getWindowBorderRadius()).toBe('0');
    });

    // "i guess i dont even want rounded corners for the highres black white
    //  example"
    test('any css length works, not only zero', () => {
        configureGlyphs({ windowBorderRadius: '2px' });
        expect(getWindowBorderRadius()).toBe('2px');
    });

    // A host that says nothing keeps what every window had before this was
    // configurable.
    test('unset leaves the historical radius', () => {
        configureGlyphs({});
        expect(getWindowBorderRadius()).toBe(HISTORICAL);
    });
});
