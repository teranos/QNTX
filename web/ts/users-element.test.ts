import { test, expect } from 'bun:test';
import { fmt, reachedBy, renderList, type UserRecord } from './users-element';

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

// The row carries the count and the hover carries every route, so a User
// with many keys stays one row wide and nothing is hidden.
test('the row counts the routes and the hover names them', () => {
    const reached = reachedBy(root());
    expect(reached.shown).toBe('2 accounts, 2 keys');
    expect(reached.whole).toBe('me@abcd.nl\napple:001750\nbrowser …eGmoRUQ1\ndevice …Zv8gaGJS');
});

// created_at is milliseconds, the way the node writes it.
test('created reads as the day it was, in UTC', () => {
    expect(fmt(1789342756348)).toBe('2026-09-13 23:39:16');
    expect(fmt(0)).toBe('—');
});

// A token lists and reads and switches nobody, so a row does not offer it
// the switch.
test('a token is not offered the switch', () => {
    const container = document.createElement('div');
    renderList(container, [root()], false);
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelectorAll('th').length).toBe(8);
});

test('one of a thing is not plural, and none is a dash', () => {
    const one = { ...root(), accounts: (root().accounts ?? []).slice(0, 1), keys: (root().keys ?? []).slice(0, 1) };
    expect(reachedBy(one).shown).toBe('1 account, 1 key');
    expect(reachedBy({ ...root(), accounts: [], keys: [] })).toEqual({ shown: '—', whole: '' });
});
