import { describe, test, expect } from 'bun:test';
import { unitFor, seriesOf, byLastSeen, formatIn, windowOf, renderSparkline } from './sparkline';

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
