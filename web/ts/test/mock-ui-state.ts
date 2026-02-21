/**
 * Shared uiState mock factory for tests.
 *
 * mock.module is process-global in Bun — every mock must be superset-complete
 * because it leaks across test files in the same run. This factory ensures
 * all mocks have identical, complete stubs.
 *
 * Usage:
 *   import { createMockUiState } from '../../test/mock-ui-state';
 *   const { uiState, glyphs, compositions, pan, minimizedWindows } = createMockUiState();
 *   mock.module('../../state/ui', () => ({ uiState }));
 */
export function createMockUiState() {
    const glyphs: any[] = [];
    const compositions: any[] = [];
    const pan: Record<string, any> = {};
    const minimizedWindows: string[] = [];

    const uiState = {
        getCanvasGlyphs: () => glyphs,
        setCanvasGlyphs: (g: any[]) => { glyphs.length = 0; glyphs.push(...g); },
        addCanvasGlyph: (g: any) => {
            const i = glyphs.findIndex((x: any) => x.id === g.id);
            if (i >= 0) glyphs[i] = g; else glyphs.push(g);
        },
        upsertCanvasGlyph: (g: any) => {
            const i = glyphs.findIndex((x: any) => x.id === g.id);
            if (i >= 0) glyphs[i] = g; else glyphs.push(g);
        },
        removeCanvasGlyph: (id: string) => {
            const i = glyphs.findIndex((g: any) => g.id === id);
            if (i >= 0) glyphs.splice(i, 1);
        },
        clearCanvasGlyphs: () => { glyphs.length = 0; },
        getCanvasCompositions: () => compositions,
        setCanvasCompositions: (c: any[]) => { compositions.length = 0; compositions.push(...c); },
        clearCanvasCompositions: () => { compositions.length = 0; },
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
        getActiveModality: () => 'ax',
        setActiveModality: () => {},
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

    return { uiState, glyphs, compositions, pan, minimizedWindows };
}
