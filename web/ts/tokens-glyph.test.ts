import { test, expect } from 'bun:test';
import { renderList } from './tokens-glyph';

// A token lists and reads and revokes nothing, so a row does not offer it
// Revoke or Enable.
test('a token is not offered revoke or enable', () => {
    const container = document.createElement('div');
    renderList(container, [{
        id: 'AT_1',
        label: 'pond-sensor',
        did: 'did:key:zDatapunt',
        minted_by: 'apple:001750',
        namespaces: ['clean'],
        created_at: '2026-09-14T00:43:56Z',
    }], false);
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelectorAll('th').length).toBe(7);
});
