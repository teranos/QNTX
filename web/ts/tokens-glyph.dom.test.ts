/**
 * @jest-environment jsdom
 *
 * Access tokens glyph — which control a row offers (ADR-025).
 *
 * Revocation is a switch: kill the token, watch whether anything is still
 * presenting it, turn it back on if that was you. A revoked row with no way
 * back means the only recovery is minting a new token and redistributing it.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderList } from './tokens-glyph.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Access tokens glyph rows', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('a revoked token offers Enable and not Revoke', () => {
        renderList(container, [
            {
                id: 'AT_1',
                label: 'laptop-cron',
                created_at: '2026-07-27T10:00:00Z',
                revoked_at: '2026-07-27T11:00:00Z',
            },
        ]);

        expect(container.textContent).toContain('Enable');
        expect(container.textContent).not.toContain('Revoke');
    });

    test('a live token offers Revoke and not Enable', () => {
        renderList(container, [
            { id: 'AT_1', label: 'laptop-cron', created_at: '2026-07-27T10:00:00Z' },
        ]);

        expect(container.textContent).toContain('Revoke');
        expect(container.textContent).not.toContain('Enable');
    });

    test('a revoked token stays listed, carrying when it stopped working', () => {
        renderList(container, [
            {
                id: 'AT_1',
                label: 'laptop-cron',
                created_at: '2026-07-27T10:00:00Z',
                revoked_at: '2026-07-27T11:00:00Z',
            },
        ]);

        expect(container.textContent).toContain('laptop-cron');
        expect(container.textContent).toContain('revoked');
        expect(container.textContent).toContain('2026-07-27 11:00:00');
    });

    test('each row is independent — one revoked, one live', () => {
        renderList(container, [
            { id: 'AT_1', label: 'live-one', created_at: '2026-07-27T10:00:00Z' },
            {
                id: 'AT_2',
                label: 'dead-one',
                created_at: '2026-07-27T10:00:00Z',
                revoked_at: '2026-07-27T11:00:00Z',
            },
        ]);

        expect(container.textContent).toContain('Revoke');
        expect(container.textContent).toContain('Enable');
        expect(container.querySelectorAll('tbody tr').length).toBe(2);
    });
});
