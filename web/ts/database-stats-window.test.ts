/**
 * Tests for Database Stats Window
 */

import { describe, it, expect } from 'bun:test';
import { databaseStatsWindow } from './database-stats-window';

describe('Database Stats Window', () => {
    it('should update stats', () => {
        const testStats = {
            path: '/test/db.sqlite',
            total_attestations: 42,
            unique_actors: 3,
            unique_subjects: 10,
            unique_contexts: 5
        };

        databaseStatsWindow.updateStats(testStats);

        // Verify stats were stored
        expect(databaseStatsWindow['stats']).toEqual(testStats);
    });
});
