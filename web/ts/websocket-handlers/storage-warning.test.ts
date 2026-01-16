/**
 * Test for storage warning handler
 */

import { describe, test, expect, mock, beforeEach } from 'bun:test';
import { handleStorageWarning } from './storage-warning';
import { boundedStorageWindow } from '../bounded-storage-window';

describe('Storage Warning Handler', () => {
    beforeEach(() => {
        // Reset the bounded storage window state
        // The window tracks warnings internally
    });

    test('storage warning updates bounded storage window', () => {
        // Create a spy for handleWarning
        const originalHandleWarning = boundedStorageWindow.handleWarning.bind(boundedStorageWindow);
        let capturedData: any = null;

        boundedStorageWindow.handleWarning = (data: any) => {
            capturedData = data;
            originalHandleWarning(data);
        };

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

        // Verify the bounded storage window received the data
        expect(capturedData).not.toBeNull();
        expect(capturedData.actor).toBe('user@test');
        expect(capturedData.context).toBe('work');
        expect(capturedData.fill_percent).toBe(0.85);

        // Restore original
        boundedStorageWindow.handleWarning = originalHandleWarning;
    });

    test('bounded storage window tracks bucket status', () => {
        handleStorageWarning({
            type: 'storage_warning',
            actor: 'plugin@github',
            context: 'repos',
            current: 950,
            limit: 1000,
            fill_percent: 0.95,
            time_until_full: '6 hours',
            timestamp: Date.now(),
        });

        // Check status level reflects critical state (>90%)
        const status = boundedStorageWindow.getStatusLevel();
        expect(status).toBe('critical');
    });

    test('storage at 70% triggers warning status', () => {
        handleStorageWarning({
            type: 'storage_warning',
            actor: 'user@example',
            context: 'default',
            current: 70,
            limit: 100,
            fill_percent: 0.70,
            time_until_full: '1 week',
            timestamp: Date.now(),
        });

        // Window should report active issues at 70%+
        expect(boundedStorageWindow.hasActiveIssues()).toBe(true);
    });
});
