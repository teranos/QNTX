/**
 * UIState - Centralized UI State Management (Implementation)
 *
 * THE state manager for QNTX web UI. Consolidates all persistent UI state
 * into a single source of truth with pub/sub reactivity.
 *
 * Architecture:
 * - Simple pub/sub for reactive updates
 * - localStorage persistence via storage.ts utility
 * - Type-safe state access
 *
 * The singleton instance lives in ui.ts (which re-exports everything from here).
 * Tests that need the real UIState class import from this file directly,
 * bypassing the process-global mock.module that replaces ui.ts.
 *
 * State domains:
 * - Panel visibility (transient, not persisted)
 * - User preferences (persisted)
 * - Session (query, verbosity — persisted with expiry)
 * - Budget warnings (transient)
 */

import type { PanelState } from '../../types/core';
import type { CompositionState } from '@teranos/elements';
import type { CanvasElement } from '../generated/proto/element/proto/canvas';
import { getItem, setItem, removeItem } from './storage';
import { keyFor } from '../standing';
import { log, SEG } from '../logger';
import { upsertCanvasElement as apiUpsertElement, deleteCanvasElement as apiDeleteElement, addMinimizedWindow as apiAddMinimized, deleteMinimizedWindow as apiDeleteMinimized } from '../api/canvas';

// ============================================================================
// State Types
// ============================================================================

/**
 * Panel identifiers - all toggleable panels in the UI
 */
export type PanelId =
    | 'config'      // ≡ am - Configuration panel
    | 'aiProvider'  // ⌬ by - AI provider selection
    | 'pulse'       // ꩜ - Scheduled jobs dashboard
    | 'prose'       // ▣ - Documentation viewer
    | 'code'        // Go code editor
    | 'hixtory'     // ⨳ ix - Job history panel
    | 'commandExplorer' // Command explorer overlay
    | 'log';        // Log panel

/**
 * Budget warning thresholds that have been crossed
 */
export interface BudgetWarningState {
    daily: boolean;
    weekly: boolean;
    monthly: boolean;
}

/**
 * Graph session state (persisted with 7-day expiry)
 */
export interface GraphSessionState {
    query?: string;
    verbosity?: number;
}

/**
 * Composition types — canonical, owned by @teranos/elements.
 * Re-exported here for backward compatibility with web/ consumers.
 */
export type { CompositionEdge, CompositionState } from '@teranos/elements';

// ============================================================================
// Embeddings State Types
// ============================================================================

export type EmbeddingModelInfo = { name: string; dimensions: number; count: number };

export interface EmbeddingsInfo {
    available: boolean;
    model_name: string;
    dimensions: number;
    models: EmbeddingModelInfo[];
    embedding_count: number;
    attestation_count: number;
    unembedded_count: number;
    lag?: { oldest_embedding: string; newest_attestation: string };
    cluster_info?: { n_clusters: number; n_noise: number; n_total: number; clusters: Record<string, number>; clusters_by_model?: Record<string, Array<{ model: string; count: number }>> };
    hdbscan_config?: { min_cluster_size: number; cluster_threshold: number; cluster_match_threshold: number };
}

export type ProjectionPoint = { id: string; source_id: string; method: string; model: string; x: number; y: number; z?: number; cluster_id: number };

export type TimelinePoint = { run_id: string; run_time: string; n_points: number; n_noise: number; cluster_id: number; label: string | null; n_members: number; event_type: string };

export interface EmbeddingsState {
    info: EmbeddingsInfo | null;
    projections: Record<string, ProjectionPoint[]>;
    clusterLabels: Record<string, string | null>;
    timeline: TimelinePoint[];
    reembedding: boolean;
    clustering: boolean;
    projecting: boolean;
    resultMsg: string;
    clusterResultMsg: string;
    view3d: boolean;
}

/**
 * Canvas element state (for persistence)
 * Derived from proto CanvasElement — id/symbol/x/y always present,
 * everything else optional (proto3 defaults 0/"" mean "unset").
 */
export type CanvasElementState =
    Pick<CanvasElement, 'id' | 'symbol' | 'x' | 'y'>
    & Partial<Omit<CanvasElement, 'id' | 'symbol' | 'x' | 'y'>>;

/**
 * Consolidated UI state
 */
export interface UIStateData {
    // Panel visibility
    panels: Record<PanelId, PanelState>;

    // Budget warning tracking (prevents duplicate toasts)
    budgetWarnings: BudgetWarningState;

    // Usage badge view mode
    usageView: 'week' | 'month';

    // Graph session (query, verbosity, transform)
    graphSession: GraphSessionState;

    // Minimized window IDs (for window tray)
    minimizedWindows: string[];

    // Canvas workspace elements (for canvas element)
    canvasElements: CanvasElementState[];

    // Canvas melded compositions (for composition persistence)
    canvasCompositions: CompositionState[];

    // Canvas navigational threads (spines)
    canvasSpines: { id: string; color: string; nodes: string[] }[];

    // Canvas pan offset and zoom scale (for canvas navigation)
    canvasPan: Record<string, { panX: number; panY: number; scale?: number }>;

    // Embeddings state (transient, not persisted)
    embeddings: EmbeddingsState;

    // Timestamp for state versioning
    lastUpdated: number;
}

/**
 * Subscriber callback type
 */
export type StateSubscriber<K extends keyof UIStateData> = (
    value: UIStateData[K],
    key: K
) => void;

/**
 * Generic subscriber for any state change
 */
export type GlobalSubscriber = (state: UIStateData, changedKey: keyof UIStateData) => void;

/**
 * Subset of UIStateData that gets persisted to localStorage
 */
interface PersistedUIState {
    usageView: 'week' | 'month';
    graphSession: GraphSessionState;
    minimizedWindows: string[];
    canvasElements: CanvasElementState[];
    canvasCompositions: CompositionState[];
    canvasSpines: { id: string; color: string; nodes: string[] }[];
    canvasPan: Record<string, { panX: number; panY: number; scale?: number }>;
}

// ============================================================================
// Default State
// ============================================================================

const DEFAULT_PANEL_STATE: PanelState = {
    visible: false,
    expanded: false,
};

function createDefaultState(): UIStateData {
    return {
        panels: {
            config: { ...DEFAULT_PANEL_STATE },
            aiProvider: { ...DEFAULT_PANEL_STATE },
            pulse: { ...DEFAULT_PANEL_STATE },
            prose: { ...DEFAULT_PANEL_STATE },
            code: { ...DEFAULT_PANEL_STATE },
            hixtory: { ...DEFAULT_PANEL_STATE },
            commandExplorer: { ...DEFAULT_PANEL_STATE },
            log: { ...DEFAULT_PANEL_STATE },
        },
        budgetWarnings: {
            daily: false,
            weekly: false,
            monthly: false,
        },
        usageView: 'week',
        graphSession: {},
        minimizedWindows: [],
        canvasElements: [],
        canvasCompositions: [],
        canvasSpines: [],
        canvasPan: {},
        embeddings: {
            info: null,
            projections: {},
            clusterLabels: {},
            timeline: [],
            reembedding: false,
            clustering: false,
            projecting: false,
            resultMsg: '',
            clusterResultMsg: '',
            view3d: false,
        },
        lastUpdated: Date.now(),
    };
}

// ============================================================================
// UIState Class
// ============================================================================

// Under keyFor (standing.ts): default keeps this one, every other namespace
// keeps its own. One canvas per namespace, in the browser as on the node
// (ADR-026). Resolved per call, since standing is set after this module loads.
const STORAGE_KEY = 'qntx-ui-state';
const STORAGE_VERSION = 2; // Bumped for graph session addition
const MAX_SUBSCRIBER_FAILURES = 3;
const GRAPH_SESSION_MAX_AGE = 7 * 24 * 60 * 60 * 1000; // 7 days

/**
 * Centralized UI state manager
 * Virtue #10: State Locality - Single source of truth, scoped access, predictable mutations
 */
export class UIState {
    private state: UIStateData;
    private subscribers: Map<keyof UIStateData, Set<StateSubscriber<any>>> = new Map();
    private globalSubscribers: Set<GlobalSubscriber> = new Set();
    // Track consecutive failures per subscriber for auto-unsubscribe
    private subscriberFailures: WeakMap<Function, number> = new WeakMap();

    constructor() {
        this.state = createDefaultState();
    }

    /**
     * Load persisted state from storage (call after initStorage())
     * Merges persisted values with current state
     */
    loadPersistedState(): void {
        const loaded = this.loadFromStorage();
        if (loaded) {
            this.state = loaded;
            log.debug(SEG.UI, '[UIState] Loaded persisted state from IndexedDB');
        }
    }

    // ========================================================================
    // State Access
    // ========================================================================

    /**
     * Get current state (read-only snapshot)
     */
    getState(): Readonly<UIStateData> {
        return this.state;
    }

    /**
     * Get a specific state value
     */
    get<K extends keyof UIStateData>(key: K): UIStateData[K] {
        return this.state[key];
    }

    // ========================================================================
    // Panel Management
    // ========================================================================

    /**
     * Check if a panel is visible
     */
    isPanelVisible(panelId: PanelId): boolean {
        return this.state.panels[panelId]?.visible ?? false;
    }

    /**
     * Set panel visibility
     */
    setPanelVisible(panelId: PanelId, visible: boolean): void {
        const panels = { ...this.state.panels };
        panels[panelId] = { ...panels[panelId], visible };
        this.update('panels', panels);
    }

    /**
     * Toggle panel visibility
     */
    togglePanel(panelId: PanelId): boolean {
        const newVisible = !this.isPanelVisible(panelId);
        this.setPanelVisible(panelId, newVisible);
        return newVisible;
    }

    /**
     * Close all panels
     */
    closeAllPanels(): void {
        const panels = { ...this.state.panels };
        for (const id of Object.keys(panels) as PanelId[]) {
            panels[id] = { ...panels[id], visible: false };
        }
        this.update('panels', panels);
    }

    // ========================================================================
    // Modality Management
    // ========================================================================

    // ========================================================================
    // Budget Warning Management
    // ========================================================================

    /**
     * Get budget warning state
     */
    getBudgetWarnings(): BudgetWarningState {
        return this.state.budgetWarnings;
    }

    /**
     * Set a budget warning flag
     */
    setBudgetWarning(period: keyof BudgetWarningState, warned: boolean): void {
        const warnings = { ...this.state.budgetWarnings, [period]: warned };
        this.update('budgetWarnings', warnings);
    }

    /**
     * Reset all budget warnings (e.g., on new day/week/month)
     */
    resetBudgetWarnings(): void {
        this.update('budgetWarnings', { daily: false, weekly: false, monthly: false });
    }

    // ========================================================================
    // Usage View Management
    // ========================================================================

    /**
     * Get usage view mode
     */
    getUsageView(): 'week' | 'month' {
        return this.state.usageView;
    }

    /**
     * Set usage view mode
     */
    setUsageView(view: 'week' | 'month'): void {
        this.update('usageView', view);
    }

    // ========================================================================
    // Graph Session Management
    // ========================================================================

    /**
     * Get current graph session
     */
    getGraphSession(): GraphSessionState {
        return this.state.graphSession;
    }

    /**
     * Update graph session (partial update)
     */
    setGraphSession(session: Partial<GraphSessionState>): void {
        const updated = { ...this.state.graphSession, ...session };
        this.update('graphSession', updated);
    }

    /**
     * Set graph query
     */
    setGraphQuery(query: string): void {
        this.setGraphSession({ query });
    }

    /**
     * Set graph verbosity level
     */
    setGraphVerbosity(verbosity: number): void {
        this.setGraphSession({ verbosity });
    }

    /**
     * Clear graph session
     */
    clearGraphSession(): void {
        this.update('graphSession', {});
    }

    // ========================================================================
    // Minimized Windows Management
    // ========================================================================

    /**
     * Get minimized window IDs
     */
    getMinimizedWindows(): string[] {
        return this.state.minimizedWindows;
    }

    /**
     * Add a window to the minimized list
     */
    addMinimizedWindow(id: string): void {
        if (this.state.minimizedWindows.includes(id)) return;
        const updated = [...this.state.minimizedWindows, id];
        this.update('minimizedWindows', updated);
        apiAddMinimized(id);
    }

    /**
     * Remove a window from the minimized list
     */
    removeMinimizedWindow(id: string): void {
        const updated = this.state.minimizedWindows.filter(wid => wid !== id);
        this.update('minimizedWindows', updated);
        apiDeleteMinimized(id);
    }

    /**
     * Set minimized windows (full replace, no sync — used for page-load merge)
     */
    setMinimizedWindows(ids: string[]): void {
        this.update('minimizedWindows', ids);
    }

    /**
     * Check if a window is minimized
     */
    isWindowMinimized(id: string): boolean {
        return this.state.minimizedWindows.includes(id);
    }

    /**
     * Clear all minimized windows
     */
    clearMinimizedWindows(): void {
        this.update('minimizedWindows', []);
    }

    // ========================================================================
    // Canvas Elements Management
    // ========================================================================

    /**
     * Get canvas elements, optionally filtered by canvas_id.
     * Without argument: returns all elements (backward compatible).
     * With canvasId: returns only elements belonging to that canvas.
     * 'canvas-workspace' maps to '' (root canvas).
     */
    getCanvasElements(canvasId?: string): CanvasElementState[] {
        if (canvasId === undefined) return this.state.canvasElements;
        const targetId = canvasId === 'canvas-workspace' ? '' : canvasId;
        return this.state.canvasElements.filter(g => (g.canvas_id ?? '') === targetId);
    }

    /**
     * Look up a single canvas element by id.
     */
    getCanvasElement(id: string): CanvasElementState | undefined {
        return this.state.canvasElements.find(g => g.id === id);
    }

    /**
     * Set canvas elements (full replace)
     */
    setCanvasElements(items: CanvasElementState[]): void {
        this.update('canvasElements', items);
    }

    /**
     * Add an element to canvas
     */
    addCanvasElement(item: CanvasElementState): void {
        // Round coordinates to integers at the boundary (backend expects int, not float)
        const normalizedElement = {
            ...item,
            x: Math.round(item.x),
            y: Math.round(item.y)
        };

        // Debug logging for result elements
        if (item.symbol === 'result') {
            log.debug(SEG.UI, `[UIState] Adding result element ${item.id}`, {
                hasContent: !!item.content,
                contentSize: item.content?.length ?? 0
            });
        }

        const existing = this.state.canvasElements.find(g => g.id === normalizedElement.id);
        if (existing) {
            // Update existing element
            const updated = this.state.canvasElements.map(g =>
                g.id === normalizedElement.id ? normalizedElement : g
            );
            this.update('canvasElements', updated);
        } else {
            // Add new element
            const updated = [...this.state.canvasElements, normalizedElement];
            this.update('canvasElements', updated);
        }

        // Enqueue for server sync (never throws)
        apiUpsertElement(normalizedElement);
    }

    /**
     * Remove an element from canvas
     */
    removeCanvasElement(id: string): void {
        const updated = this.state.canvasElements.filter(g => g.id !== id);
        this.update('canvasElements', updated);

        // Enqueue for server sync (never throws)
        apiDeleteElement(id);
    }

    /**
     * Clear all canvas elements
     */
    clearCanvasElements(): void {
        this.update('canvasElements', []);
    }

    // ========================================================================
    // Canvas Compositions Management
    // (Composition logic in state/compositions.ts - these are low-level accessors)
    // ========================================================================

    /**
     * Get canvas compositions
     */
    getCanvasCompositions(): CompositionState[] {
        return this.state.canvasCompositions;
    }

    /**
     * Set canvas compositions (full replace)
     */
    setCanvasCompositions(compositions: CompositionState[]): void {
        this.update('canvasCompositions', compositions);
    }

    // ========================================================================
    // Canvas Spines (Navigational Threads) Management
    // ========================================================================

    getCanvasSpines(): { id: string; color: string; nodes: string[] }[] {
        return this.state.canvasSpines;
    }

    addCanvasSpine(spine: { id: string; color: string; nodes: string[] }): void {
        const updated = [...this.state.canvasSpines, spine];
        this.update('canvasSpines', updated);
    }

    removeCanvasSpine(id: string): void {
        const updated = this.state.canvasSpines.filter(s => s.id !== id);
        this.update('canvasSpines', updated);
    }

    // ========================================================================
    // Canvas Pan Management
    // ========================================================================

    /**
     * Get canvas pan offset and zoom for a specific canvas
     */
    getCanvasPan(canvasId: string): { panX: number; panY: number; scale?: number } | null {
        return this.state.canvasPan[canvasId] ?? null;
    }

    /**
     * Set canvas pan offset and zoom for a specific canvas
     */
    setCanvasPan(canvasId: string, pan: { panX: number; panY: number; scale?: number }): void {
        const updated = { ...this.state.canvasPan, [canvasId]: pan };
        this.update('canvasPan', updated);
    }

    // ========================================================================
    // Embeddings State Management (transient, not persisted)
    // ========================================================================

    /**
     * Get current embeddings state
     */
    getEmbeddings(): EmbeddingsState {
        return this.state.embeddings;
    }

    /**
     * Partial update of embeddings state
     */
    updateEmbeddings(partial: Partial<EmbeddingsState>): void {
        const updated = { ...this.state.embeddings, ...partial };
        this.update('embeddings', updated);
    }

    // ========================================================================
    // Subscription (Pub/Sub)
    // ========================================================================

    /**
     * Subscribe to changes on a specific state key
     */
    subscribe<K extends keyof UIStateData>(
        key: K,
        callback: StateSubscriber<K>
    ): () => void {
        if (!this.subscribers.has(key)) {
            this.subscribers.set(key, new Set());
        }
        this.subscribers.get(key)!.add(callback);

        // Return unsubscribe function
        return () => {
            this.subscribers.get(key)?.delete(callback);
        };
    }

    /**
     * Subscribe to any state change
     */
    subscribeAll(callback: GlobalSubscriber): () => void {
        this.globalSubscribers.add(callback);
        return () => {
            this.globalSubscribers.delete(callback);
        };
    }

    // ========================================================================
    // Internal State Updates
    // ========================================================================

    /**
     * Update a state value and notify subscribers
     */
    private update<K extends keyof UIStateData>(key: K, value: UIStateData[K]): void {
        this.state = {
            ...this.state,
            [key]: value,
            lastUpdated: Date.now(),
        };

        // Notify key-specific subscribers
        const keySubscribers = this.subscribers.get(key);
        if (keySubscribers) {
            for (const callback of keySubscribers) {
                if (!this.safeNotify(callback, () => callback(value, key), String(key))) {
                    keySubscribers.delete(callback);
                }
            }
        }

        // Notify global subscribers
        for (const callback of this.globalSubscribers) {
            if (!this.safeNotify(callback, () => callback(this.state, key), 'global')) {
                this.globalSubscribers.delete(callback);
            }
        }

        // Persist to storage (IndexedDB)
        this.saveToStorage();
    }

    /**
     * Safely notify a subscriber with failure tracking
     * Returns false if subscriber should be removed (too many failures)
     */
    private safeNotify(
        callback: Function,
        invoke: () => void,
        context: string
    ): boolean {
        try {
            invoke();
            // Reset failure count on success
            this.subscriberFailures.delete(callback);
            return true;
        } catch (error: unknown) {
            const failures = (this.subscriberFailures.get(callback) ?? 0) + 1;
            this.subscriberFailures.set(callback, failures);

            if (failures >= MAX_SUBSCRIBER_FAILURES) {
                log.error(SEG.UI, `Subscriber for ${context} failed ${failures} times, auto-unsubscribing:`, error);
                return false;
            }

            log.error(SEG.UI, `Subscriber error for ${context} (${failures}/${MAX_SUBSCRIBER_FAILURES}):`, error);
            return true;
        }
    }

    // ========================================================================
    // Persistence
    // ========================================================================

    /**
     * Persisted state shape (subset of UIStateData)
     */
    private getPersistedState(): PersistedUIState {
        return {
            usageView: this.state.usageView,
            graphSession: this.state.graphSession,
            minimizedWindows: this.state.minimizedWindows,
            canvasElements: this.state.canvasElements,
            canvasCompositions: this.state.canvasCompositions,
            canvasSpines: this.state.canvasSpines,
            canvasPan: this.state.canvasPan,
            // Don't persist: panels (should start closed), budgetWarnings (session-only)
        };
    }

    /**
     * Save state to localStorage using storage.ts
     */
    private saveToStorage(): void {
        setItem(keyFor(STORAGE_KEY), this.getPersistedState(), { version: STORAGE_VERSION });
    }

    /**
     * Load state from localStorage using storage.ts
     */
    private loadFromStorage(): UIStateData | null {
        const persisted = getItem<PersistedUIState>(keyFor(STORAGE_KEY), {
            version: STORAGE_VERSION,
            maxAge: GRAPH_SESSION_MAX_AGE,
        });

        if (!persisted) return null;

        // Merge persisted preferences with default state
        const defaultState = createDefaultState();
        return {
            ...defaultState,
            usageView: persisted.usageView ?? defaultState.usageView,
            graphSession: persisted.graphSession ?? defaultState.graphSession,
            minimizedWindows: persisted.minimizedWindows ?? defaultState.minimizedWindows,
            canvasElements: persisted.canvasElements ?? defaultState.canvasElements,
            canvasCompositions: persisted.canvasCompositions ?? defaultState.canvasCompositions,
            canvasSpines: persisted.canvasSpines ?? defaultState.canvasSpines,
            canvasPan: persisted.canvasPan ?? defaultState.canvasPan,
        };
    }

    /**
     * Clear all persisted state
     */
    clearStorage(): void {
        removeItem(keyFor(STORAGE_KEY));
    }

    /**
     * Reset to default state
     */
    reset(): void {
        this.state = createDefaultState();
        this.clearStorage();

        // Notify all subscribers (use safeNotify to prevent a throwing subscriber from breaking the loop)
        for (const key of Object.keys(this.state) as (keyof UIStateData)[]) {
            const keySubscribers = this.subscribers.get(key);
            if (keySubscribers) {
                for (const callback of keySubscribers) {
                    if (!this.safeNotify(callback, () => callback(this.state[key], key), `reset:${String(key)}`)) {
                        keySubscribers.delete(callback);
                    }
                }
            }
        }
    }
}
