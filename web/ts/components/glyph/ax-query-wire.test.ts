/**
 * Tests for the pure mappings between a parsed AX query and the stores that
 * answer it: the local filter (resolved instants) and the REST query string
 * (raw expressions).
 */

import { describe, it, expect } from 'bun:test';
import { temporalBounds, resolvedToFilter, axQueryParams, fromWireAs } from './ax-query-wire';
import type { AxQuery, ResolvedAxQuery } from '../../ats-wasm';

const NOW = 1_700_000_000_000;
const DAY = 86_400_000;

describe('temporalBounds', () => {
    it('Since bounds the start, Until the end', () => {
        expect(temporalBounds({ Since: NOW - DAY })).toEqual({ time_start: NOW - DAY });
        expect(temporalBounds({ Until: NOW })).toEqual({ time_end: NOW });
    });

    it('On and Between bound both ends', () => {
        expect(temporalBounds({ On: { start_ms: NOW, end_ms: NOW + DAY } }))
            .toEqual({ time_start: NOW, time_end: NOW + DAY });
        expect(temporalBounds({ Between: { start_ms: NOW - DAY, end_ms: NOW } }))
            .toEqual({ time_start: NOW - DAY, time_end: NOW });
    });

    it('Over is a duration, not a range — no bounds', () => {
        expect(temporalBounds({ Over: { raw: '5y', value: 5, unit: 'years' } })).toEqual({});
        expect(temporalBounds(undefined)).toEqual({});
    });
});

describe('resolvedToFilter', () => {
    it('carries the segments and the resolved bounds', () => {
        const query: ResolvedAxQuery = {
            subjects: ['ALICE'],
            predicates: ['author'],
            contexts: ['github'],
            actors: [],
            temporal: { Since: NOW - DAY },
            actions: [],
        };
        expect(resolvedToFilter(query)).toEqual({
            subjects: ['ALICE'],
            predicates: ['author'],
            contexts: ['github'],
            actors: [],
            time_start: NOW - DAY,
        });
    });
});

describe('axQueryParams', () => {
    const base: AxQuery = { subjects: [], predicates: [], contexts: [], actors: [] };

    it('maps segments to the REST parameter names, comma-joined', () => {
        const params = axQueryParams({
            ...base,
            subjects: ['ALICE', 'BOB'],
            predicates: ['author'],
        });
        expect(params).toBe('subject=ALICE%2CBOB&predicate=author');
    });

    it('temporal rides as the words the user typed', () => {
        expect(axQueryParams({ ...base, temporal: { Since: 'yesterday' } }))
            .toBe('since=yesterday');
        expect(axQueryParams({ ...base, temporal: { On: '2025-01-15' } }))
            .toBe('on=2025-01-15');
    });

    it('Between becomes since and until', () => {
        const params = new URLSearchParams(
            axQueryParams({ ...base, temporal: { Between: ['2025-01-01', '2025-02-01'] } }));
        expect(params.get('since')).toBe('2025-01-01');
        expect(params.get('until')).toBe('2025-02-01');
    });

    it('expressions with spaces survive the query string', () => {
        const params = new URLSearchParams(
            axQueryParams({ ...base, temporal: { Since: '3 days ago' } }));
        expect(params.get('since')).toBe('3 days ago');
    });

    it('Over has no REST word and stays out', () => {
        expect(axQueryParams({ ...base, temporal: { Over: { raw: '5y', value: 5, unit: 'years' } } }))
            .toBe('');
    });
});

describe('fromWireAs', () => {
    it('converts RFC3339 timestamps to the epoch milliseconds the glyph renders', () => {
        const att = fromWireAs({
            id: 'AS-1',
            subjects: ['ALICE'],
            predicates: ['author'],
            contexts: ['github'],
            actors: ['human:a'],
            timestamp: '2025-01-15T00:00:00Z',
            source: 'cli',
            created_at: '2025-01-15T00:00:00Z',
        });
        expect(att.id).toBe('AS-1');
        expect(att.timestamp).toBe(Date.parse('2025-01-15T00:00:00Z'));
        expect(att.created_at).toBe(Date.parse('2025-01-15T00:00:00Z'));
        expect(att.signer_did).toBe('');
    });
});
