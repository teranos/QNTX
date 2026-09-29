/**
 * @jest-environment jsdom
 *
 * Triplet result line — when the triple was attested, not how many times.
 */

import { describe, test, expect } from 'bun:test';
import { renderTripletResultLine, fitTimeAxes } from './triplet-element.ts';
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

const DAY = 24 * HOUR;

const lineOf = (group: Attestation[]): HTMLElement => {
    const line = renderTripletResultLine(group, now);
    line.dataset.tripletAttestations = JSON.stringify(group);
    return line;
};

const pointsOf = (line: HTMLElement): string[] =>
    (line.querySelector('.sparkline polyline')?.getAttribute('points') ?? '').split(' ');

describe('fitTimeAxes', () => {
    test('every line in a list is drawn in one window, from its earliest attestation to now', () => {
        const list = document.createElement('div');
        const early = lineOf([at(now - 20 * DAY), at(now - 19 * DAY)]);
        const late = lineOf([at(now - 3 * DAY), at(now - 2 * DAY)]);
        list.append(early, late);

        fitTimeAxes(list, now);

        const e = list.children[0] as HTMLElement;
        const l = list.children[1] as HTMLElement;
        expect(pointsOf(e).length).toBe(21);
        expect(pointsOf(l).length).toBe(21);
        // The late line is flat until its first day: the same x is the same day on both.
        expect(pointsOf(l)[0].split(',')[1]).toBe('15');
        expect(e.querySelector('.triplet-time')?.getAttribute('data-window'))
            .toBe(l.querySelector('.triplet-time')?.getAttribute('data-window'));
    });

    test('an older attestation arriving moves every line to the new start', () => {
        const list = document.createElement('div');
        list.append(lineOf([at(now - 3 * DAY), at(now - 2 * DAY)]));
        fitTimeAxes(list, now);
        expect(pointsOf(list.children[0] as HTMLElement).length).toBe(4);

        list.append(lineOf([at(now - 10 * DAY), at(now - 9 * DAY)]));
        fitTimeAxes(list, now);

        expect(pointsOf(list.children[0] as HTMLElement).length).toBe(11);
        expect(pointsOf(list.children[1] as HTMLElement).length).toBe(11);
    });

    test('a line on its own keeps its own window', () => {
        const line = renderTripletResultLine([at(now - 3 * DAY), at(now - 2 * DAY)], now);
        expect(pointsOf(line).length).toBe(4);
    });
});
