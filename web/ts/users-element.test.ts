import { test, expect } from 'bun:test';
import { fmt, renderList, type UserRecord } from './users-element';

function root(): UserRecord {
    return {
        id: 'US-USER-Y3BNGXYR',
        level: 'ROOT',
        namespace: '',
        accounts: [
            { provider: 'google', canonical_id: 'google:110106507016968762213', handle: 'me@abcd.nl' },
            { provider: 'apple', canonical_id: 'apple:001750', handle: '' },
        ],
        keys: [
            { did: 'did:key:z6MkecHPQvijTYesU9jSNGAfb27g6CUY9S1JfzmyeGmoRUQ1', origin: 'BROWSER' },
            { did: 'did:key:z6MkqqinWpV8H4CVdncY1d8y37Q4kGeezrtL7WzrZv8gaGJS', origin: 'DEVICE' },
        ],
        created_at: 0,
    } as UserRecord;
}

// created_at is milliseconds, the way the node writes it.
test('created reads as the day it was, in UTC', () => {
    expect(fmt(1789342756348)).toBe('2026-09-13 23:39:16');
    expect(fmt(0)).toBe('—');
});

// "the infinite row of switch of is pissing me off as well, should be in the User themselves"
test('the list switches nobody: the switch is in the User', () => {
    const container = document.createElement('div');
    renderList(container, [root()]);
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelectorAll('th').length).toBe(4);
});

// "Not interested in Phone in Users glyph"
// "i can find it in the user itself,"
test('the list has no Phone and no Reached by', () => {
    const container = document.createElement('div');
    renderList(container, [root()]);
    const heads = Array.from(container.querySelectorAll('th')).map(th => th.textContent);
    // "TABLES BEING MAX 4 col": the mail is under the name, the door under the level.
    expect(heads).toEqual(['Name', 'Level', 'Created', 'Status']);
    expect(container.querySelector('tbody tr')!.children.length).toBe(4);
});

// "but why dont i see my outgoing invitations in the same list, and a way for me to open the would-be-user"
test('an open invitation is a row of its own, invited, among the Users', () => {
    const container = document.createElement('div');
    const open = {
        id: 'inv1', email: 'ada@gmail.com', display_name: 'Ada',
        accounts: [{ provider: 'google', account: 'ada@gmail.com' }],
        invited_by: 'US-USER-Y3BNGXYR', created_at: 1789342756348, state: 'open' as const,
    };
    renderList(container, [root()], [
        open,
        { ...open, id: 'inv2', display_name: 'Bob', cancelled_at: 0, state: 'cancelled' as const },
        { ...open, id: 'inv3', display_name: 'Cy', accepted_by: 'US-CY', state: 'accepted' as const },
    ]);
    const rows = Array.from(container.querySelectorAll('tbody tr')).map(r => r.textContent ?? '');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toContain('Ada');
    expect(rows[0]).toContain('invited');
    expect(rows[0]).toContain('ada@gmail.com');
    expect(rows.join()).not.toContain('Bob');
    expect(rows.join()).not.toContain('Cy');
});
