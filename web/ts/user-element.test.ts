import { test, expect } from 'bun:test';
import { addressesOf, becomable, renderUser } from './user-element';
import type { UserRecord } from './users-element';

function root(emails: string[] = []): UserRecord {
    return {
        id: 'US-USER-ROOT0001',
        level: 'ROOT',
        email_addresses: emails,
        accounts: [{ provider: 'google', canonical_id: 'google:1', handle: 'root@garden.test' }],
        keys: [{ did: 'did:key:z6Mkgarden', origin: 'BROWSER' }],
        created_at: 0,
    };
}

// "goes to their primary email address if there are multiple."
test('the first address is named primary, and none is said', () => {
    expect(addressesOf(root(['root@garden.test', 'root@orchard.test']))).toBe('root@garden.test (primary)\nroot@orchard.test');
    expect(addressesOf(root())).toBe('— (no mail reaches this User)');
});

// "can i set this in qntx ? how about a user element accesible from users element"
test('a person is offered to add to their own record', () => {
    const container = document.createElement('div');
    renderUser(container, root(), true);
    expect(container.querySelector('.user-add-email')).not.toBeNull();
    expect(container.textContent).toContain('no mail reaches this User');
});

// "the infinite row of switch of is pissing me off as well, should be in the User themselves"
test('the switch is in the User: off for a User who is on, on for one who is off', () => {
    const on = document.createElement('div');
    renderUser(on, root(), false, () => {}, true);
    expect(on.textContent).toContain('Switch off');
    expect(on.textContent).not.toContain('Switch on');

    const off = document.createElement('div');
    renderUser(off, { ...root(), disabled_by: 'US-USER-ROOT0001' }, false, () => {}, true);
    expect(off.textContent).toContain('Switch on');
    expect(off.textContent).not.toContain('Switch off');
});

test('a token reading a User is offered no switch', () => {
    const container = document.createElement('div');
    renderUser(container, root(), false, () => {}, false);
    expect(container.textContent).not.toContain('Switch');
});

test("nobody is offered to add to another User's record", () => {
    const container = document.createElement('div');
    renderUser(container, root(), false);
    expect(container.querySelector('.user-add-email')).toBeNull();
    expect(container.textContent).toContain('Only this User adds to their own record');
});

// "and to become or unbecome, is actually in the specific User in the Users Element, the i element is to get back to ROOT"
test('ROOT is offered to become a User that is not ROOT, and is on', () => {
    const other: UserRecord = { ...root(), id: 'US-OTHER-1', level: 'PUBLIC_REGISTRATION' };
    expect(becomable(other, true)).toBe(true);
    expect(becomable(other, false)).toBe(false);
    expect(becomable(root(), true)).toBe(false);
    expect(becomable({ ...other, disabled_by: 'US-USER-ROOT0001' }, true)).toBe(false);

    const container = document.createElement('div');
    renderUser(container, other, false, () => {}, false, true);
    expect(container.textContent).toContain('Become');
    renderUser(container, other, false, () => {}, false, false);
    expect(container.textContent).not.toContain('Become');
});
