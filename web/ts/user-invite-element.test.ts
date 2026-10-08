// Invite User element — ROOT inviting a friend (ADR-031).

// "as root, i press the + and i can create a new user, like how i would create a new oauth token"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

// "if ROOT knows that the friend has gmail, in most cases they will just enter the friends gmail, and the userstory just shows ROOT the option to alo pick that mail address as the mail of the google identity"

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderInvite } from './user-invite-element.ts';
import { renderInviteLink } from './users-element.ts';
import type { ProviderDescription } from './ceremony.ts';

function provider(id: string, label: string): ProviderDescription {
    return {
        id, label, kind: 'redirect',
        host_prompt: '', host_placeholder: '', host_default: '',
        identifier_prompt: '', secret_prompt: '',
    };
}

const PROVIDERS = [provider('google', 'Google'), provider('apple', 'Apple'), provider('mastodon', 'Mastodon')];

describe('Tim, as ROOT, invites a friend', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    function rows(): HTMLButtonElement[] {
        return Array.from(container.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    }

    test('the Users element carries a +, the way Access Tokens does', () => {
        renderInviteLink(container);
        const plus = container.querySelector('button');
        expect(plus).not.toBeNull();
        expect(plus!.textContent).toBe('+');
        expect(plus!.getAttribute('aria-label')).toBe('Invite a user');
    });

    test('one row per provider the node lists, none pressed', () => {
        renderInvite(container, PROVIDERS);
        expect(rows().map(r => r.dataset.provider)).toEqual(['google', 'apple', 'mastodon']);
        for (const row of rows()) expect(row.getAttribute('aria-checked')).toBe('false');
    });

    test('ROOT enters the friend\'s e-mail address and their account at the provider', () => {
        renderInvite(container, PROVIDERS);
        expect(container.querySelector<HTMLInputElement>('input[name="email"]')!.type).toBe('email');
        expect(container.querySelector<HTMLInputElement>('input[name="account"]')).not.toBeNull();
        expect(container.textContent).toContain('Send invitation');
    });

    test('the friend\'s gmail is offered as their Google account', () => {
        renderInvite(container, PROVIDERS);
        rows().find(r => r.dataset.provider === 'google')!.click();
        const email = container.querySelector<HTMLInputElement>('input[name="email"]')!;
        email.value = 'ada@gmail.com';
        email.dispatchEvent(new Event('input'));

        const same = container.querySelector<HTMLInputElement>('input[name="same"]')!;
        expect(same.closest('label')!.textContent).toContain('Google');
        same.click();

        expect(container.querySelector<HTMLInputElement>('input[name="account"]')!.value).toBe('ada@gmail.com');
    });
});
