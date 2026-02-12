/**
 * Test for storage warning handler
 */

import { describe, test, expect, mock, spyOn } from 'bun:test';
import { handleStorageWarning } from './storage-warning';

describe('Storage Warning Handler', () => {
    test('storage warning logs correct fill percentage', () => {
        const warnSpy = spyOn(console, 'warn');

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

        const loggedArgs = warnSpy.mock.calls[0].join(' ');
        expect(loggedArgs).toContain('85%');
        expect(loggedArgs).toContain('85/100');

        warnSpy.mockRestore();
    });
});
