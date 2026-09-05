import { describe, expect, test } from 'bun:test';
import { APP_DOOR, ticketIn } from './app-door';

// Safari finishes the ceremony and hands the ticket back through qntx://.
// The node only ever sends somebody to a door am.toml named, and the app's
// door is one origin: what arrives at any other is not a ticket.

describe('Tim', () => {
    test('the ticket rides the deep link back into the app', () => {
        expect(ticketIn([`${APP_DOOR}?ceremony=abc123`])).toBe('abc123');
    });

    test('the door is the one am.toml names', () => {
        expect(APP_DOOR).toBe('qntx://door');
    });
});

describe('Spike', () => {
    test('a deep link with no ticket is not one', () => {
        expect(ticketIn([`${APP_DOOR}`])).toBeNull();
        expect(ticketIn([])).toBeNull();
    });

    test('a ticket on some other address is not this door\'s', () => {
        expect(ticketIn(['qntx://elsewhere?ceremony=abc123'])).toBeNull();
        expect(ticketIn(['https://q.sbvh.nl/?ceremony=abc123'])).toBeNull();
    });

    test('the first ticket wins when several arrive at once', () => {
        expect(ticketIn([`${APP_DOOR}?other=1`, `${APP_DOOR}?ceremony=first`, `${APP_DOOR}?ceremony=second`])).toBe('first');
    });
});
