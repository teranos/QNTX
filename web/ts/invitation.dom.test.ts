/**
 * @jest-environment jsdom
 *
 * The invitation a friend arrived with (ADR-031). JSDOM, because the page has
 * an address there and the link is read off it.
 */

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

import { describe, test, expect, beforeEach } from 'bun:test';
import { heldInvitation, letGoOfInvitation, onlyInvited } from './invitation.ts';
import type { ProviderDescription } from './ceremony.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

function provider(id: string): ProviderDescription {
    return {
        id, label: id, kind: 'redirect',
        host_prompt: '', host_placeholder: '', host_default: '',
        identifier_prompt: '', secret_prompt: '',
    };
}

describe('Ada arrives with the link ROOT mailed her', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        window.sessionStorage.clear();
        window.history.replaceState(null, '', '/');
    });

    test('she sees only the provider ROOT selected', () => {
        const offered = [provider('google'), provider('apple'), provider('mastodon')];
        const seen = onlyInvited(offered, { token: 't', provider: 'mastodon', account: '@ada@mastodon.example' });
        expect(seen.map(p => p.id)).toEqual(['mastodon']);
    });

    test('the link is taken off the address and kept across the provider\'s round trip', () => {
        window.history.replaceState(null, '', '/?invitation=abc123&x=1');
        expect(heldInvitation()).toBe('abc123');
        expect(window.location.search).toBe('?x=1');

        // Back from the provider, the address carries a ceremony and no invitation.
        window.history.replaceState(null, '', '/?ceremony=t');
        expect(heldInvitation()).toBe('abc123');

        letGoOfInvitation();
        expect(heldInvitation()).toBe('');
    });
});
