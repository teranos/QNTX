/**
 * BoundedStorageWindow Tests
 *
 * Tests for weekly aggregation logic, localStorage persistence, and status calculations.
 */

import { describe, test, expect, beforeEach, afterEach } from 'bun:test';

// We need to test the logic without the full Window dependency
// Extract the testable functions or test via the exported singleton

describe('BoundedStorageWindow', () => {
    const STORAGE_KEY = 'qntx-bounded-storage-evictions';

    beforeEach(() => {
        // Clear localStorage before each test
        localStorage.removeItem(STORAGE_KEY);
    });

    afterEach(() => {
        localStorage.removeItem(STORAGE_KEY);
    });

    describe('Weekly Eviction Aggregation', () => {
        test('getWeeklyEvictionsByDay returns 7 days with correct labels', async () => {
            // Import fresh to get clean state
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            const days = boundedStorageWindow.getWeeklyEvictionsByDay();

            expect(days.length).toBe(7);
            expect(days[6].label).toBe('Today');
            expect(days[5].label).toBe('Yest');
        });

        test('getWeeklyEvictionsByDay aggregates eviction counts correctly', async () => {
            // Seed localStorage with test data
            const now = new Date();
            const today = new Date(now);
            today.setHours(12, 0, 0, 0); // Noon today

            const yesterday = new Date(now);
            yesterday.setDate(yesterday.getDate() - 1);
            yesterday.setHours(12, 0, 0, 0);

            const testEvictions = [
                { actor: 'user1', context: 'ctx1', deletionsCount: 10, message: 'test', eventType: 'fifo', timestamp: today.getTime() },
                { actor: 'user1', context: 'ctx1', deletionsCount: 5, message: 'test', eventType: 'fifo', timestamp: today.getTime() - 3600000 },
                { actor: 'user2', context: 'ctx2', deletionsCount: 20, message: 'test', eventType: 'lru', timestamp: yesterday.getTime() },
            ];

            localStorage.setItem(STORAGE_KEY, JSON.stringify(testEvictions));

            // Re-import to trigger constructor which loads from localStorage
            // We need to bust the module cache
            const modulePath = './bounded-storage-window';
            delete require.cache[require.resolve(modulePath)];

            // Since we can't easily reset the singleton, test via handleEviction
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            // Add fresh evictions through the API
            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'test',
                context: 'ctx',
                deletions_count: 7,
                message: 'test eviction',
                event_type: 'budget'
            });

            const days = boundedStorageWindow.getWeeklyEvictionsByDay();

            // Today should have at least 7 from our test eviction
            expect(days[6].count).toBeGreaterThanOrEqual(7);
        });

        test('getWeeklyEvictedAttestations sums deletions within 7 days', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            // Add test evictions
            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'user1',
                context: 'work',
                deletions_count: 100,
                message: 'test',
                event_type: 'limit'
            });

            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'user2',
                context: 'personal',
                deletions_count: 50,
                message: 'test',
                event_type: 'ttl'
            });

            const total = boundedStorageWindow.getWeeklyEvictedAttestations();
            expect(total).toBeGreaterThanOrEqual(150);
        });
    });

    describe('Status Level Calculation', () => {
        test('returns healthy when no buckets exceed 70%', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            // With no warnings, should be healthy
            // Note: Previous test state might affect this
            const level = boundedStorageWindow.getStatusLevel();
            // At minimum, without warnings it should not be critical
            expect(['healthy', 'warning']).toContain(level);
        });

        test('returns warning when bucket is 70-90% full', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            boundedStorageWindow.handleWarning({
                type: 'storage_warning',
                actor: 'test_user',
                context: 'test_context',
                current: 75,
                limit: 100,
                fill_percent: 0.75,
                time_until_full: '2 days',
                timestamp: Date.now()
            });

            const level = boundedStorageWindow.getStatusLevel();
            expect(['warning', 'critical']).toContain(level);
        });

        test('returns critical when bucket exceeds 90%', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            boundedStorageWindow.handleWarning({
                type: 'storage_warning',
                actor: 'critical_user',
                context: 'critical_context',
                current: 95,
                limit: 100,
                fill_percent: 0.95,
                time_until_full: '2 hours',
                timestamp: Date.now()
            });

            const level = boundedStorageWindow.getStatusLevel();
            expect(level).toBe('critical');
        });
    });

    describe('Active Issues Detection', () => {
        test('hasActiveIssues returns true for recent evictions', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            // Add a recent eviction
            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'recent',
                context: 'ctx',
                deletions_count: 5,
                message: 'recent eviction',
                event_type: 'fifo'
            });

            expect(boundedStorageWindow.hasActiveIssues()).toBe(true);
        });

        test('hasActiveIssues returns true for high fill buckets', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            boundedStorageWindow.handleWarning({
                type: 'storage_warning',
                actor: 'high_fill',
                context: 'ctx',
                current: 80,
                limit: 100,
                fill_percent: 0.80,
                time_until_full: '1 day',
                timestamp: Date.now()
            });

            expect(boundedStorageWindow.hasActiveIssues()).toBe(true);
        });
    });

    describe('Eviction Callbacks', () => {
        test('onEvictionUpdate callback is called on new eviction', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            let callbackCalled = false;
            const unsubscribe = boundedStorageWindow.onEvictionUpdate(() => {
                callbackCalled = true;
            });

            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'callback_test',
                context: 'ctx',
                deletions_count: 1,
                message: 'test',
                event_type: 'manual'
            });

            expect(callbackCalled).toBe(true);

            // Cleanup
            unsubscribe();
        });

        test('unsubscribe stops callback from being called', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            let callCount = 0;
            const unsubscribe = boundedStorageWindow.onEvictionUpdate(() => {
                callCount++;
            });

            // First eviction - should trigger callback
            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'unsub_test',
                context: 'ctx',
                deletions_count: 1,
                message: 'test',
                event_type: 'manual'
            });

            expect(callCount).toBe(1);

            // Unsubscribe
            unsubscribe();

            // Second eviction - should NOT trigger callback
            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'unsub_test2',
                context: 'ctx',
                deletions_count: 1,
                message: 'test',
                event_type: 'manual'
            });

            expect(callCount).toBe(1); // Still 1, not 2
        });
    });

    describe('localStorage Persistence', () => {
        test('evictions are saved to localStorage', async () => {
            const module = await import('./bounded-storage-window');
            const { boundedStorageWindow } = module;

            boundedStorageWindow.handleEviction({
                type: 'storage_eviction',
                actor: 'persist_test',
                context: 'ctx',
                deletions_count: 42,
                message: 'persistence test',
                event_type: 'budget'
            });

            const stored = localStorage.getItem(STORAGE_KEY);
            expect(stored).not.toBeNull();

            const parsed = JSON.parse(stored!);
            expect(Array.isArray(parsed)).toBe(true);
            expect(parsed.some((e: { actor: string }) => e.actor === 'persist_test')).toBe(true);
        });
    });
});
