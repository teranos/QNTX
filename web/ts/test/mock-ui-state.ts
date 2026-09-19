/**
 * Shared uiState mock factory for tests.
 *
 * mock.module is process-global in Bun — every mock must be superset-complete
 * because it leaks across test files in the same run. This factory ensures
 * all mocks have identical, complete stubs.
 *
 * Usage:
 *   import { createMockUiState } from '../../test/mock-ui-state';
 *   const { uiState, elements, compositions, pan, minimizedWindows } = createMockUiState();
 *   mock.module('../../state/ui', () => ({ uiState }));
 */
export function createMockUiState() {
    const items: any[] = [];
    const compositions: any[] = [];
    const pan: Record<string, any> = {};
    const minimizedWindows: string[] = [];

    const uiState = {
        getCanvasElements: () => items,
        getCanvasElement: (id: string) => items.find((g: any) => g.id === id),
        setCanvasElements: (g: any[]) => { items.length = 0; items.push(...g); },
        addCanvasElement: (g: any) => {
            const i = items.findIndex((x: any) => x.id === g.id);
            if (i >= 0) items[i] = g; else items.push(g);
        },
        upsertCanvasElement: (g: any) => {
            const i = items.findIndex((x: any) => x.id === g.id);
            if (i >= 0) items[i] = g; else items.push(g);
        },
        removeCanvasElement: (id: string) => {
            const i = items.findIndex((g: any) => g.id === id);
            if (i >= 0) items.splice(i, 1);
        },
        clearCanvasElements: () => { items.length = 0; },
        getCanvasCompositions: () => compositions,
        setCanvasCompositions: (c: any[]) => { compositions.length = 0; compositions.push(...c); },
        clearCanvasCompositions: () => { compositions.length = 0; },
        getCanvasSpines: () => [] as any[],
        addCanvasSpine: () => {},
        removeCanvasSpine: () => {},
        getCanvasPan: (id: string) => pan[id] ?? null,
        setCanvasPan: (id: string, p: any) => { pan[id] = p; },
        loadPersistedState: () => {},
        getMinimizedWindows: () => minimizedWindows,
        addMinimizedWindow: (id: string) => {
            if (!minimizedWindows.includes(id)) minimizedWindows.push(id);
        },
        removeMinimizedWindow: (id: string) => {
            const idx = minimizedWindows.indexOf(id);
            if (idx >= 0) minimizedWindows.splice(idx, 1);
        },
        setMinimizedWindows: (ids: string[]) => { minimizedWindows.length = 0; minimizedWindows.push(...ids); },
        isWindowMinimized: (id: string) => minimizedWindows.includes(id),
        clearMinimizedWindows: () => { minimizedWindows.length = 0; },
        isPanelVisible: () => false,
        setPanelVisible: () => {},
        togglePanel: () => false,
        closeAllPanels: () => {},
        getBudgetWarnings: () => ({ daily: false, weekly: false, monthly: false }),
        setBudgetWarning: () => {},
        resetBudgetWarnings: () => {},
        getUsageView: () => 'week',
        setUsageView: () => {},
        getGraphSession: () => ({}),
        setGraphSession: () => {},
        setGraphQuery: () => {},
        setGraphVerbosity: () => {},
        clearGraphSession: () => {},
        subscribe: () => () => {},
        subscribeAll: () => () => {},
        getState: () => ({}),
        get: () => undefined,
        clearStorage: () => {},
        reset: () => {},
    };

    return { uiState, elements: items, compositions, pan, minimizedWindows };
}
