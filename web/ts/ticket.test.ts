import { describe, expect, test } from 'bun:test';
import { ticketInQuery, withoutTicket } from './ticket';

// The node sends the person back to the door with the ticket in the URL. The
// door reads it, spends it, and keeps an address without it.

describe('Tim', () => {
    test('the ticket is read off the URL the browser landed on', () => {
        expect(ticketInQuery('https://door.example/?ceremony=abc123')).toBe('abc123');
        expect(ticketInQuery('?ceremony=abc123')).toBe('abc123');
    });

    test('the address kept is the one without the ticket', () => {
        expect(withoutTicket('https://door.example/?ceremony=abc123')).toBe('https://door.example/');
        expect(withoutTicket('https://door.example/branch/x/?ceremony=abc123')).toBe('https://door.example/branch/x/');
    });
});

describe('Spike', () => {
    test('an address with no ticket has none', () => {
        expect(ticketInQuery('https://door.example/')).toBeNull();
        expect(ticketInQuery('https://door.example/?other=1')).toBeNull();
        expect(ticketInQuery('https://door.example/?ceremony=')).toBeNull();
    });

    test('other parameters and the fragment survive the ticket leaving', () => {
        expect(withoutTicket('https://door.example/?brow=1&ceremony=abc123#top')).toBe('https://door.example/?brow=1#top');
        expect(withoutTicket('https://door.example/?ceremony=abc123&brow=1')).toBe('https://door.example/?brow=1');
    });

    test('an address without a ticket is left as it is', () => {
        expect(withoutTicket('https://door.example/#top')).toBe('https://door.example/#top');
    });
});
