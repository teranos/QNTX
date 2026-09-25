import { test, expect } from 'bun:test';
import { addressesOf, renderUser } from './user-element';
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

test("nobody is offered to add to another User's record", () => {
    const container = document.createElement('div');
    renderUser(container, root(), false);
    expect(container.querySelector('.user-add-email')).toBeNull();
    expect(container.textContent).toContain('Only this User adds to their own record');
});
