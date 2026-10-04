import { describe, test, expect } from 'bun:test';
import { renderSparkline } from '@teranos/elements';
import { unitFor, seriesOf, byLastSeen, formatIn, windowOf, timeOfBucket, seenOver, labelsOf } from './sparkline';

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const at = (y: number, m: number, d: number, h = 0, min = 0): number => new Date(y, m - 1, d, h, min).getTime();

describe("Umami's unit for a range (getMinimumUnit, date range)", () => {
    const t0 = at(2026, 3, 10, 12);
    test('an hour or less is minutes', () => {
        expect(unitFor(t0, t0 + 60 * MIN)).toBe('minute');
    });
    test('past an hour, up to 48 hours, is hours', () => {
        expect(unitFor(t0, t0 + 61 * MIN)).toBe('hour');
        expect(unitFor(t0, t0 + 48 * HOUR)).toBe('hour');
    });
    test('past 48 hours, up to seven calendar months, is days', () => {
        expect(unitFor(t0, t0 + 49 * HOUR)).toBe('day');
        expect(unitFor(at(2026, 1, 1), at(2026, 8, 31))).toBe('day');
    });
    test('up to 24 calendar months is months, beyond that years', () => {
        expect(unitFor(at(2026, 1, 1), at(2026, 9, 1))).toBe('month');
        expect(unitFor(at(2024, 1, 1), at(2026, 1, 1))).toBe('month');
        expect(unitFor(at(2024, 1, 1), at(2026, 2, 1))).toBe('year');
    });
});

describe('Buckets', () => {
    test('every bucket of the window is there, the empty ones at zero', () => {
        const w = { start: at(2026, 3, 1), end: at(2026, 3, 4, 18), unit: 'day' as const };
        expect(seriesOf([at(2026, 3, 1, 9), at(2026, 3, 1, 23), at(2026, 3, 3, 1)], w)).toEqual([2, 0, 1, 0]);
    });
    test('the window runs from the earliest moment to now, in its unit', () => {
        const now = at(2026, 3, 10);
        expect(windowOf([now - 3 * DAY, now - DAY], now)).toEqual({ start: now - 3 * DAY, end: now, unit: 'day' });
    });
    test('one moment in a window of many buckets still draws, as one spike', () => {
        const w = { start: at(2026, 3, 1), end: at(2026, 3, 30), unit: 'day' as const };
        expect(renderSparkline(seriesOf([at(2026, 3, 15, 8)], w))).toContain('<polyline');
    });
});

describe('Rows', () => {
    test('most recently seen first, however often something else was seen', () => {
        const rows = byLastSeen([
            { name: 'often', times: [1, 2, 3, 4] },
            { name: 'lately', times: [9] },
        ]);
        expect(rows.map((r) => r.name)).toEqual(['lately', 'often']);
    });
    test('the last-seen moment is written in its unit, the way Umami writes it', () => {
        const t = at(2026, 3, 7, 9, 5);
        expect(formatIn(t, 'minute')).toBe('2026-03-07 09:05');
        expect(formatIn(t, 'hour')).toBe('2026-03-07 09');
        expect(formatIn(t, 'day')).toBe('2026-03-07');
        expect(formatIn(t, 'month')).toBe('2026-03');
        expect(formatIn(t, 'year')).toBe('2026');
    });
});

describe('Buckets the node sends', () => {
    test('each key shape the node writes is the start of its bucket, in UTC', () => {
        expect(timeOfBucket('2026-09-11T14:10')).toBe(Date.UTC(2026, 8, 11, 14, 10));
        expect(timeOfBucket('2026-09-11T14')).toBe(Date.UTC(2026, 8, 11, 14));
        expect(timeOfBucket('2026-09-11')).toBe(Date.UTC(2026, 8, 11));
        // ISO week 37 of 2026 starts on Monday 7 September.
        expect(timeOfBucket('2026-W37')).toBe(Date.UTC(2026, 8, 7));
        expect(timeOfBucket('soon')).toBeNull();
    });

    test('a bucket counts as many as the node says fell in it', () => {
        const seen = seenOver('page_view', { '2026-09-11T14': 3, '2026-09-12T09': 1 }, '2026-09-12T09:41:00Z');
        const w = windowOf(seen.times, Date.UTC(2026, 8, 14, 12));
        expect(w.unit).toBe('day');
        const series = seriesOf(seen.times, w, seen.weights);
        expect(series.reduce((a, b) => a + b, 0)).toBe(4);
        expect(Math.max(...series)).toBe(3);
    });

    test('when last seen is the moment the node sends, not the start of its bucket', () => {
        const rows = byLastSeen([
            seenOver('earlier', { '2026-09-12T09': 1 }, '2026-09-12T09:05:00Z'),
            seenOver('later', { '2026-09-12T09': 1 }, '2026-09-12T09:41:00Z'),
        ]);
        expect(rows.map((r) => r.name)).toEqual(['later', 'earlier']);
    });
});

describe('A line answers pointing', () => {
    test('each step carries when it is, the window\'s buckets in their unit', () => {
        const w = { start: at(2026, 3, 10, 12), end: at(2026, 3, 10, 14, 30), unit: 'hour' as const };
        expect(labelsOf(w)).toEqual(['2026-03-10 12', '2026-03-10 13', '2026-03-10 14']);
        expect(labelsOf(w).length).toBe(seriesOf([], w).length);
    });
});
