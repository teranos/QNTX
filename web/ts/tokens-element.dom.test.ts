/**
 * @jest-environment jsdom
 *
 * Access tokens element — what a row is (ADR-025).
 *
 * Revocation is a switch: kill the token, watch whether anything is still
 * presenting it, turn it back on if that was you. The switch is the token
 * element's; a row is the way in to it.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderList } from './tokens-element.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Access tokens element rows', () => {
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

    // "these revoke buttons row on the right, it feels like it can be removed
    // given that that button is already in the token element itself."
    test('no row offers Revoke or Enable', () => {
        renderList(container, [
            { id: 'AT_1', label: 'live-one', created_at: '2026-07-27T10:00:00Z' },
            {
                id: 'AT_2',
                label: 'dead-one',
                created_at: '2026-07-27T10:00:00Z',
                revoked_at: '2026-07-27T11:00:00Z',
            },
        ]);

        expect(container.textContent).not.toContain('Revoke');
        expect(container.textContent).not.toContain('Enable');
        expect(container.querySelectorAll('tbody tr').length).toBe(2);
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
        expect(container.textContent).toContain('revoked:');
        expect(container.textContent).toContain(' ago');
    });

    test('the label is the way in to the token', () => {
        renderList(container, [
            { id: 'AT_1', label: 'laptop-cron', created_at: '2026-07-27T10:00:00Z' },
        ]);

        const label = container.querySelector<HTMLElement>('tbody td');
        expect(label?.textContent).toBe('laptop-cron');
        expect(label?.title).toBe('press to open this token');
    });
});
