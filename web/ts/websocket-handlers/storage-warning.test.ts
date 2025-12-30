/**
 * Test for storage warning handler
 */

import { describe, test, expect, mock } from 'bun:test';
import { handleStorageWarning } from './storage-warning';

// Mock the toast module
mock.module('../toast', () => ({
    toast: {
        warning: mock(() => {}),
    },
}));

describe('Storage Warning Handler', () => {
    test('storage warning shows correct fill percentage', () => {
        const { toast } = require('../toast');

        handleStorageWarning({
            type: 'storage_warning',
            actor: 'user@test',
            context: 'work',
            current: 85,
            limit: 100,
            fill_percent: 0.85,
            time_until_full: '2 days',
            timestamp: Date.now(),
        });

        // Should display "Storage 85% full for user@test/work (85/100)"
        const message = toast.warning.mock.calls[0][0];
        expect(message).toContain('85%');
        expect(message).toContain('85/100');
    });
});
