/**
 * Morph class lifecycle (MRPCL).
 *
 * The morph class belongs to the morph, not the glyph: it arrives with
 * prepareMorphTo, leaves at commit, and never wipes what the glyph wears.
 *
 * Personas:
 * - Tim: happy path — manifest, commit, the morph class is gone
 * - Spike: rollback — the glyph keeps the classes it had (Morph Axioma)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { prepareMorphTo } from './morphology';
import { setGlyphId } from '../dataset';
import type { Glyph } from '../glyph';

const noVerify = () => {};

function trayDot(ownClasses: string[] = []): { element: HTMLElement; glyph: Glyph } {
    const element = document.createElement('div');
    element.className = ['glyph-run-glyph', ...ownClasses].join(' ');
    setGlyphId(element, 'morph-test-1');
    document.body.appendChild(element);
    const glyph: Glyph = {
        id: 'morph-test-1',
        title: 'Morph Test',
        renderContent: () => document.createElement('div'),
    };
    return { element, glyph };
}

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: manifest and commit', () => {
    test('the morph class is added, the dot class leaves with the dot state', () => {
        const { element, glyph } = trayDot();
        prepareMorphTo(element, glyph, noVerify, 'glyph-morphing-to-window', '1000');

        expect(element.classList.contains('glyph-morphing-to-window')).toBe(true);
        expect(element.classList.contains('glyph-run-glyph')).toBe(false);
    });

    // "a settled window keeps a name saying it is still animating" — not any more.
    test('commit swaps the morph class for the settled class', () => {
        const { element, glyph } = trayDot();
        const morph = prepareMorphTo(element, glyph, noVerify, 'glyph-morphing-to-window', '1000');

        morph.commitClass('glyph-window');

        expect(element.classList.contains('glyph-morphing-to-window')).toBe(false);
        expect(element.classList.contains('glyph-window')).toBe(true);
    });

    test('commit can settle several classes at once (panel)', () => {
        const { element, glyph } = trayDot();
        const morph = prepareMorphTo(element, glyph, noVerify, 'glyph-morphing-to-panel', '10003');

        morph.commitClass('glyph-panel glyph-panel--fullscreen glyph-panel--from-top');

        expect(element.classList.contains('glyph-morphing-to-panel')).toBe(false);
        expect(element.classList.contains('glyph-panel')).toBe(true);
        expect(element.classList.contains('glyph-panel--fullscreen')).toBe(true);
        expect(element.classList.contains('glyph-panel--from-top')).toBe(true);
    });

    // "a glyph loses its own classes on manifest" — not any more.
    test('the glyph keeps its own classes through manifest and commit', () => {
        const { element, glyph } = trayDot(['glyph-error']);
        const morph = prepareMorphTo(element, glyph, noVerify, 'glyph-morphing-to-window', '1000');
        expect(element.classList.contains('glyph-error')).toBe(true);

        morph.commitClass('glyph-window');
        expect(element.classList.contains('glyph-error')).toBe(true);
    });
});

describe('Spike: rollback', () => {
    // Morph Axioma: the attempt is abandoned and the glyph keeps the state
    // it had.
    test('an abandoned morph restores exactly the classes the glyph had', () => {
        const { element, glyph } = trayDot(['glyph-error']);
        const before = element.className;
        const morph = prepareMorphTo(element, glyph, noVerify, 'glyph-morphing-to-window', '1000');

        morph.rollbackClass();

        expect(element.className).toBe(before);
    });
});
