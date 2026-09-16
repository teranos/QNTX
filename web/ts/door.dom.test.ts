/**
 * @jest-environment jsdom
 *
 * The door names who sent the person (ADR-025). An app that sent somebody
 * here is not the origin they are looking at, so nothing else in front of
 * them says who will hold the token.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { doorHost, sentBy } from './door.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('The door names who sent you', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '<div id="system-drawer"></div>';
    });

    test('a client is named on the face, and stays while the door stands', () => {
        doorHost();
        sentBy('app');

        const from = document.querySelector('.door-from');
        expect(from?.textContent).toBe('app sent you here');

        // The door is emptied and redrawn as it changes face; the line stays.
        doorHost();
        expect(document.querySelector('.door-from')?.textContent).toBe('app sent you here');
    });

    test('nobody is named until somebody sent you', () => {
        doorHost();
        expect(document.querySelector('.door-from')).toBeNull();

        sentBy('');
        expect(document.querySelector('.door-from')?.textContent).toBe('');
    });
});
