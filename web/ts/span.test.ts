/**
 * The spans the UI shows are the spans Rust writes: every vector in
 * crates/ats/testdata/spans.json, which crates/ats checks it writes too.
 */

import { describe, test, expect } from 'bun:test';
import vectors from '../../crates/ats/testdata/spans.json';
import { long, relative, short, spanBetween, spanOfMs } from './span';

describe('span', () => {
    test('between two moments, as Rust counts it', () => {
        for (const v of vectors.between) {
            const span = spanBetween(Date.parse(v.from), Date.parse(v.to));
            expect(`${v.from} → ${v.to}: ${short(span)} / ${long(span)}`)
                .toBe(`${v.from} → ${v.to}: ${v.short} / ${v.long}`);
        }
    });

    test('a length with no start, as Rust counts it', () => {
        for (const v of vectors.of_ms) {
            const span = spanOfMs(v.ms);
            expect(`${v.ms}: ${short(span)} / ${long(span)}`).toBe(`${v.ms}: ${v.short} / ${v.long}`);
        }
    });

    test('a moment past is ago, one to come is in: both read back', () => {
        const now = Date.parse('2026-03-31T10:00:00Z');
        expect(relative(Date.parse('2026-03-28T10:00:00Z'), now)).toBe('3d ago');
        expect(relative(Date.parse('2026-04-03T10:00:00Z'), now)).toBe('in 3d');
        expect(relative(now, now)).toBe('0s ago');
    });
});
