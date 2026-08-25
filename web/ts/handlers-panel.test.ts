/**
 * Handler fire rows — a failing handler is a fix-now kind of event.
 *
 * watcher_fires (migration 055) records errors with the fire; the panel used
 * to skip any fire without an attestation and never read `error`, so the one
 * thing that matters most was the one thing not drawn.
 *
 * Personas:
 * - Tim: an error fire renders as the error row the CSS anticipated
 * - Spike: a fire with no attestation at all is still a row
 */

import { describe, test, expect } from 'bun:test';
import { errorFireRow, type Fire } from './handlers-panel';

describe('Tim: error fires are drawn', () => {
    test('the error text takes the row, when sits on the right', () => {
        const f: Fire = {
            at_ms: Date.now() - 60_000,
            attestation_id: 'as-123',
            error: 'gave up after 5 attempts: plugin not loaded',
        };
        const row = errorFireRow(f);

        expect(row.classList.contains('handlers-result-error')).toBe(true);
        expect(row.textContent).toContain('gave up after 5 attempts');
        expect(row.title).toContain('as-123');
        expect(row.title).toContain('failed');
    });
});

describe('Spike: a failure with no attestation is still a row', () => {
    // A queue write that failed before an attestation existed records with
    // an empty id — the failure is the thing worth seeing.
    test('no attestation, no title, the error still renders', () => {
        const f: Fire = { at_ms: Date.now(), error: 'queue write failed' };
        const row = errorFireRow(f);

        expect(row.textContent).toContain('queue write failed');
        expect(row.title).toBe('');
    });
});
