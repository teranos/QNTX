/**
 * @jest-environment jsdom
 *
 * Triplet result line — when the triple was attested, not how many times.
 */

import { describe, test, expect } from 'bun:test';
import { renderTripletResultLine } from './triplet-element.ts';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const HOUR = 3_600_000;
const now = new Date(2026, 8, 29, 12, 30).getTime();

const at = (timestamp: number): Attestation => ({
    id: `AS-${timestamp}`,
    subjects: ['boutique-7'],
    predicates: ['visited'],
    contexts: ['clean'],
    actors: ['did:key:z6Mkalice'],
    timestamp,
    source: 'staand',
} as Attestation);

describe('renderTripletResultLine', () => {
    test('a group shows a line over time and when it was last attested, and no count', () => {
        const group = [at(now - 20 * HOUR), at(now - 3 * HOUR), at(now - 2 * HOUR)];
        const line = renderTripletResultLine(group, now);

        expect(line.querySelector('.sparkline polyline')).not.toBeNull();
        expect(line.querySelector('.sparkline-last')?.textContent).toBe('2026-09-29 10');
        expect(line.textContent).not.toContain('(3)');
        expect(line.dataset.tooltip).not.toContain('attestation');
    });

    test('a watcher streams the timestamp as an ISO string, and it is read', () => {
        const streamed = [at(now - 3 * HOUR), at(now - 2 * HOUR)].map((a) =>
            ({ ...a, timestamp: new Date(a.timestamp).toISOString() }) as unknown as Attestation);
        const line = renderTripletResultLine(streamed, now);
        expect(line.querySelector('.sparkline-last')?.textContent).toBe('2026-09-29 10');
    });

    test('a group with no dated attestation shows no time', () => {
        const line = renderTripletResultLine([at(0), at(0)], now);
        expect(line.querySelector('.sparkline')).toBeNull();
        expect(line.textContent).not.toContain('(2)');
    });
});
