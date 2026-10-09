// Invite User element — ROOT inviting a friend (ADR-031).

// "as root, i press the + and i can create a new user, like how i would create a new oauth token"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

// "and dont we actually have logo's for them"

// "and i want to set Name"

// "if both google and apple, then we set both, and  if set then we set the mail address of that provider"

// "or the username"

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderInvite, renderCancel, type InvitationBody } from './user-invite-element.ts';
import { renderInviteLink } from './users-element.ts';
import type { ProviderDescription } from './ceremony.ts';

function provider(id: string, label: string): ProviderDescription {
    return {
        id, label, kind: 'redirect',
        host_prompt: '', host_placeholder: '', host_default: '',
        identifier_prompt: '', secret_prompt: '',
    };
}

const PROVIDERS = [provider('google', 'Google'), provider('apple', 'Apple'), provider('github', 'GitHub')];

describe('Tim, as ROOT, invites a friend', () => {
    let container: HTMLElement;
    let sent: InvitationBody[];
    const send = async (body: InvitationBody) => { sent.push(body); };

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        sent = [];
    });

    function toggles(): HTMLButtonElement[] {
        return Array.from(container.querySelectorAll<HTMLButtonElement>('button[data-provider]'));
    }
    function press(id: string): void {
        toggles().find(t => t.dataset.provider === id)!.click();
    }
    function field(name: string): HTMLInputElement {
        return container.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
    }
    function type(name: string, value: string): void {
        field(name).value = value;
        field(name).dispatchEvent(new Event('input'));
    }
    async function sendIt(): Promise<void> {
        Array.from(container.querySelectorAll('button')).find(b => b.textContent?.includes('Send invitation'))!.click();
        await new Promise(resolve => setTimeout(resolve, 0));
    }

    test('the Users element carries a +, the way Access Tokens does', () => {
        renderInviteLink(container);
        const plus = container.querySelector('button');
        expect(plus!.textContent).toBe('+');
        expect(plus!.getAttribute('aria-label')).toBe('Invite a user');
    });

    test('Name, Invitation mail, and a toggle per provider, each wearing its mark, none pressed', () => {
        renderInvite(container, PROVIDERS, send);
        expect(field('display_name')).not.toBeNull();
        expect(field('email').type).toBe('email');
        expect(container.textContent).toContain('Invitation mail');
        expect(toggles().map(t => t.dataset.provider)).toEqual(['google', 'apple', 'github']);
        for (const t of toggles()) {
            expect(t.getAttribute('aria-pressed')).toBe('false');
            expect(t.querySelector('svg')).not.toBeNull();
        }
        expect(container.querySelector('input[name^="account-"]')).toBeNull();
    });

    test('Google and GitHub pressed: an account each, Google the invitation mail, GitHub the username', async () => {
        renderInvite(container, PROVIDERS, send);
        type('display_name', 'Ada');
        type('email', 'ada@gmail.com');
        press('google');
        press('github');

        container.querySelector<HTMLInputElement>('input[name="same-google"]')!.click();
        expect(field('account-google').value).toBe('ada@gmail.com');
        type('account-github', 'adalovelace');

        await sendIt();
        expect(sent).toHaveLength(1);
        expect(sent[0].display_name).toBe('Ada');
        expect(sent[0].email).toBe('ada@gmail.com');
        expect(sent[0].accounts).toEqual([
            { provider: 'google', account: 'ada@gmail.com' },
            { provider: 'github', account: 'adalovelace' },
        ]);
    });

    test('pressing a provider again takes it off the invitation', () => {
        renderInvite(container, PROVIDERS, send);
        press('apple');
        expect(field('account-apple')).not.toBeNull();
        press('apple');
        expect(field('account-apple')).toBeNull();
    });

    test('a pressed provider with no account is not sent', async () => {
        renderInvite(container, PROVIDERS, send);
        type('email', 'ada@gmail.com');
        press('github');
        await sendIt();
        expect(sent).toHaveLength(0);
        expect(container.textContent).toContain('no GitHub account');
    });
});

// "the MAIL ROOT received has a button for cancelling the invitation"

describe('Tim, as ROOT, opens the cancel in his copy of the mail', () => {
    let container: HTMLElement;
    const ada = {
        id: 'inv1', email: 'ada@gmail.com', display_name: 'Ada',
        accounts: [{ provider: 'google', account: 'ada@gmail.com' }, { provider: 'github', account: 'adalovelace' }],
        invited_by: 'US-TIM', created_at: 1,
    };

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('an open invitation says who it is for and offers to cancel it', () => {
        renderCancel(container, ada);
        expect(container.textContent).toContain('Ada');
        expect(container.textContent).toContain('ada@gmail.com');
        expect(container.textContent).toContain('github as adalovelace');
        expect(container.textContent).toContain('Cancel the invitation');
    });

    test('a used invitation offers nothing to cancel', () => {
        renderCancel(container, { ...ada, accepted_by: 'US-ADA' });
        expect(container.textContent).toContain('already accepted');
        expect(container.textContent).not.toContain('Cancel the invitation');
    });

    test('a cancelled one says so', () => {
        renderCancel(container, { ...ada, cancelled_at: 2 });
        expect(container.textContent).toContain('already cancelled');
        expect(container.textContent).not.toContain('Cancel the invitation');
    });
});
