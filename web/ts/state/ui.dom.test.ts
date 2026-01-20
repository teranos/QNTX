/**
 * Tests for UIState - centralized UI state management
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';

// We need to test the class, not the singleton, so we'll import the module fresh
// For now, test the singleton behavior
import { uiState, type PanelId, type GraphSessionState } from './ui';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost' // Required for localStorage
    });
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;
}

describe('UIState', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        // Reset state before each test
        uiState.reset();
        localStorage.clear();
    });

    describe('Panel Management', () => {
        test('panels start hidden by default', () => {
            expect(uiState.isPanelVisible('config')).toBe(false);
            expect(uiState.isPanelVisible('pulse')).toBe(false);
            expect(uiState.isPanelVisible('prose')).toBe(false);
        });

        test('setPanelVisible changes panel visibility', () => {
            uiState.setPanelVisible('config', true);
            expect(uiState.isPanelVisible('config')).toBe(true);

            uiState.setPanelVisible('config', false);
            expect(uiState.isPanelVisible('config')).toBe(false);
        });

        test('togglePanel toggles visibility and returns new state', () => {
            expect(uiState.isPanelVisible('pulse')).toBe(false);

            const result1 = uiState.togglePanel('pulse');
            expect(result1).toBe(true);
            expect(uiState.isPanelVisible('pulse')).toBe(true);

            const result2 = uiState.togglePanel('pulse');
            expect(result2).toBe(false);
            expect(uiState.isPanelVisible('pulse')).toBe(false);
        });

        test('closeAllPanels closes all panels', () => {
            uiState.setPanelVisible('config', true);
            uiState.setPanelVisible('pulse', true);
            uiState.setPanelVisible('prose', true);

            uiState.closeAllPanels();

            expect(uiState.isPanelVisible('config')).toBe(false);
            expect(uiState.isPanelVisible('pulse')).toBe(false);
            expect(uiState.isPanelVisible('prose')).toBe(false);
        });
    });

    describe('Modality Management', () => {
        test('default modality is ax', () => {
            expect(uiState.getActiveModality()).toBe('ax');
        });

        test('setActiveModality changes modality', () => {
            uiState.setActiveModality('ix');
            expect(uiState.getActiveModality()).toBe('ix');

            uiState.setActiveModality('db');
            expect(uiState.getActiveModality()).toBe('db');
        });
    });

    describe('Budget Warning Management', () => {
        test('budget warnings start false', () => {
            const warnings = uiState.getBudgetWarnings();
            expect(warnings.daily).toBe(false);
            expect(warnings.weekly).toBe(false);
            expect(warnings.monthly).toBe(false);
        });

        test('setBudgetWarning sets individual warnings', () => {
            uiState.setBudgetWarning('daily', true);
            expect(uiState.getBudgetWarnings().daily).toBe(true);
            expect(uiState.getBudgetWarnings().weekly).toBe(false);

            uiState.setBudgetWarning('weekly', true);
            expect(uiState.getBudgetWarnings().weekly).toBe(true);
        });

        test('resetBudgetWarnings clears all warnings', () => {
            uiState.setBudgetWarning('daily', true);
            uiState.setBudgetWarning('weekly', true);
            uiState.setBudgetWarning('monthly', true);

            uiState.resetBudgetWarnings();

            const warnings = uiState.getBudgetWarnings();
            expect(warnings.daily).toBe(false);
            expect(warnings.weekly).toBe(false);
            expect(warnings.monthly).toBe(false);
        });
    });

    describe('Usage View Management', () => {
        test('default usage view is week', () => {
            expect(uiState.getUsageView()).toBe('week');
        });

        test('setUsageView changes view mode', () => {
            uiState.setUsageView('month');
            expect(uiState.getUsageView()).toBe('month');

            uiState.setUsageView('week');
            expect(uiState.getUsageView()).toBe('week');
        });
    });

    describe('Graph Session Management', () => {
        test('graph session starts empty', () => {
            const session = uiState.getGraphSession();
            expect(session.query).toBeUndefined();
            expect(session.verbosity).toBeUndefined();
            expect(session.transform).toBeUndefined();
        });

        test('setGraphSession updates session partially', () => {
            uiState.setGraphSession({ query: 'test query' });
            expect(uiState.getGraphSession().query).toBe('test query');

            uiState.setGraphSession({ verbosity: 3 });
            expect(uiState.getGraphSession().query).toBe('test query');
            expect(uiState.getGraphSession().verbosity).toBe(3);
        });

        test('setGraphQuery sets query', () => {
            uiState.setGraphQuery('i:Function');
            expect(uiState.getGraphSession().query).toBe('i:Function');
        });

        test('setGraphVerbosity sets verbosity', () => {
            uiState.setGraphVerbosity(5);
            expect(uiState.getGraphSession().verbosity).toBe(5);
        });

        test('setGraphTransform sets transform', () => {
            const transform = { x: 100, y: 200, k: 1.5 };
            uiState.setGraphTransform(transform);
            expect(uiState.getGraphSession().transform).toEqual(transform);
        });

        test('clearGraphSession resets graph session', () => {
            uiState.setGraphSession({
                query: 'test',
                verbosity: 3,
                transform: { x: 1, y: 2, k: 1 },
            });

            uiState.clearGraphSession();

            const session = uiState.getGraphSession();
            expect(session.query).toBeUndefined();
            expect(session.verbosity).toBeUndefined();
            expect(session.transform).toBeUndefined();
        });
    });

    describe('Subscription (Pub/Sub)', () => {
        test('subscribe receives updates for specific key', () => {
            const callback = mock(() => {});

            uiState.subscribe('activeModality', callback);
            uiState.setActiveModality('ix');

            expect(callback).toHaveBeenCalledWith('ix', 'activeModality');
        });

        test('unsubscribe stops receiving updates', () => {
            const callback = mock(() => {});

            const unsubscribe = uiState.subscribe('activeModality', callback);
            uiState.setActiveModality('ix');
            expect(callback).toHaveBeenCalledTimes(1);

            unsubscribe();
            uiState.setActiveModality('db');
            expect(callback).toHaveBeenCalledTimes(1); // Still 1, not 2
        });

        test('subscribeAll receives all updates', () => {
            const callback = mock(() => {});

            uiState.subscribeAll(callback);
            uiState.setActiveModality('ix');
            uiState.setUsageView('month');

            expect(callback).toHaveBeenCalledTimes(2);
        });
    });

    describe('State Access', () => {
        test('getState returns readonly state snapshot', () => {
            uiState.setActiveModality('db');
            const state = uiState.getState();

            expect(state.activeModality).toBe('db');
            expect(state.panels).toBeDefined();
            expect(state.lastUpdated).toBeGreaterThan(0);
        });

        test('get returns specific state value', () => {
            uiState.setUsageView('month');
            expect(uiState.get('usageView')).toBe('month');
        });
    });

    describe('Persistence', () => {
        test('persisted state survives reset for preferences', () => {
            // Set some values
            uiState.setActiveModality('ix');
            uiState.setUsageView('month');
            uiState.setGraphSession({ query: 'test', verbosity: 2 });

            // Check localStorage has data
            const stored = localStorage.getItem('qntx-ui-state');
            expect(stored).not.toBeNull();
        });

        test('clearStorage removes persisted state', () => {
            uiState.setActiveModality('ix');
            uiState.clearStorage();

            expect(localStorage.getItem('qntx-ui-state')).toBeNull();
        });
    });

    describe('Reset', () => {
        test('reset restores default state', () => {
            uiState.setActiveModality('ix');
            uiState.setUsageView('month');
            uiState.setPanelVisible('config', true);

            uiState.reset();

            expect(uiState.getActiveModality()).toBe('ax');
            expect(uiState.getUsageView()).toBe('week');
            expect(uiState.isPanelVisible('config')).toBe(false);
        });

        test('reset notifies all subscribers', () => {
            const callback = mock(() => {});
            uiState.subscribe('activeModality', callback);

            uiState.reset();

            // Should be called during reset
            expect(callback).toHaveBeenCalled();
        });
    });
});
