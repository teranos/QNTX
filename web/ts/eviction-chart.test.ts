import { describe, test, expect } from 'bun:test';
import { seedEvictions, getPredicateBreakdown } from './eviction-chart';

const ev = (at: string, predicates: string[], deletions = 1) => ({
    event_type: 'actor_context_limit', actor: 'a', context: 'c', entity: 'e',
    deletions_count: deletions, message: '', timestamp: at, predicates,
});

describe('Evicted predicates', () => {
    test('most recently evicted first, however much more another lost, with when each was', () => {
        seedEvictions([
            ev('2026-09-01T10:00:00Z', ['often'], 500),
            ev('2026-09-02T10:00:00Z', ['often'], 500),
            ev('2026-09-20T10:00:00Z', ['lately'], 1),
        ]);
        const breakdown = getPredicateBreakdown();
        expect(breakdown.map((b) => b.predicate)).toEqual(['lately', 'often']);
        expect(breakdown[1].evictedAt).toEqual([
            Date.parse('2026-09-01T10:00:00Z'),
            Date.parse('2026-09-02T10:00:00Z'),
        ]);
        expect(breakdown[0].lastEviction).toBe(Date.parse('2026-09-20T10:00:00Z'));
    });
});
