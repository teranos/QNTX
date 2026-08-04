/**
 * Text selection during a morph.
 *
 * Personas:
 * - Tim: pressing a tray dot selects nothing
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { suppressSelectionUntilRelease } from './morph-transaction';

const realm = () => globalThis.window as unknown as { Event: typeof Event };

beforeEach(() => {
    document.body.style.userSelect = '';
});

describe('Tim: selection during a morph', () => {
    // "when clicking on a glyph from the tray, on release i see that i have
    //  selected the text of the other glyophs while the tray was getting
    //  smaller"
    test('the press stops selection before the tray moves', () => {
        suppressSelectionUntilRelease();
        expect(document.body.style.userSelect).toBe('none');
    });

    // Same quote: "on release" is when it must be over, not before.
    test('release gives selection back', () => {
        suppressSelectionUntilRelease();
        const Ev = realm().Event;
        document.dispatchEvent(new Ev('mouseup', { bubbles: true }));
        expect(document.body.style.userSelect).not.toBe('none');
    });

});
