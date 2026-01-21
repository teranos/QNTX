/**
 * UIState - Centralized UI State Management
 *
 * THE state manager for QNTX web UI. Consolidates all persistent UI state
 * into a single source of truth with pub/sub reactivity.
 *
 * Architecture:
 * - Singleton instance for global access
 * - Simple pub/sub for reactive updates
 * - localStorage persistence via storage.ts utility
 * - Type-safe state access
 *
 * State domains:
 * - Panel visibility (transient, not persisted)
 * - User preferences (persisted)
 * - Graph session (persisted with expiry)
 * - Budget warnings (transient)
 */

import type { PanelState, Transform } from '../../types/core';
import { getItem, setItem, removeItem } from './storage';
import { log, SEG } from '../logger';

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
    transform?: Transform | null;
}

/**
 * Individual window state
 */
export interface WindowState {
    x: number;
    y: number;
    width: string;
    height?: string;  // Optional height (for dot-as-primitive windows)
    visible: boolean;
    minimized: boolean;
}

/**
 * Consolidated UI state
 */
export interface UIStateData {
    // Panel visibility
    panels: Record<PanelId, PanelState>;

    // Current active modality in symbol palette
    activeModality: string;

    // Budget warning tracking (prevents duplicate toasts)
    budgetWarnings: BudgetWarningState;

    // Usage badge view mode
    usageView: 'week' | 'month';

    // Graph session (query, verbosity, transform)
    graphSession: GraphSessionState;

    // Window states (position, visibility, minimized)
    windowStates: Record<string, WindowState>;

    // Minimized window IDs (for window tray) - DEPRECATED: use windowStates[id].minimized
    minimizedWindows: string[];

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
    activeModality: string;
    usageView: 'week' | 'month';
    graphSession: GraphSessionState;
    windowStates: Record<string, WindowState>;
    minimizedWindows: string[];  // DEPRECATED: kept for migration compatibility
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
        activeModality: 'ax',
        budgetWarnings: {
            daily: false,
            weekly: false,
            monthly: false,
        },
        usageView: 'week',
        graphSession: {},
        windowStates: {},
        minimizedWindows: [],
        lastUpdated: Date.now(),
    };
}

// ============================================================================
// UIState Class
// ============================================================================

const STORAGE_KEY = 'qntx-ui-state';
const STORAGE_VERSION = 3; // Bumped for windowStates addition
const MAX_SUBSCRIBER_FAILURES = 3;
const GRAPH_SESSION_MAX_AGE = 7 * 24 * 60 * 60 * 1000; // 7 days

/**
 * Centralized UI state manager
 * Virtue #10: State Locality - Single source of truth, scoped access, predictable mutations
 */
class UIState {
    private state: UIStateData;
    private subscribers: Map<keyof UIStateData, Set<StateSubscriber<any>>> = new Map();
    private globalSubscribers: Set<GlobalSubscriber> = new Set();
    // Track consecutive failures per subscriber for auto-unsubscribe
    private subscriberFailures: WeakMap<Function, number> = new WeakMap();

    constructor() {
        this.state = this.loadFromStorage() || createDefaultState();
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

    /**
     * Get current active modality
     */
    getActiveModality(): string {
        return this.state.activeModality;
    }

    /**
     * Set active modality
     */
    setActiveModality(modality: string): void {
        this.update('activeModality', modality);
    }

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
     * Set graph transform (zoom/pan state)
     */
    setGraphTransform(transform: Transform | null): void {
        this.setGraphSession({ transform });
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
    }

    /**
     * Remove a window from the minimized list
     */
    removeMinimizedWindow(id: string): void {
        const updated = this.state.minimizedWindows.filter(wid => wid !== id);
        this.update('minimizedWindows', updated);
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
    // Window State Management
    // ========================================================================

    /**
     * Get state for a specific window
     */
    getWindowState(id: string): WindowState | undefined {
        return this.state.windowStates[id];
    }

    /**
     * Set state for a specific window
     */
    setWindowState(id: string, state: WindowState): void {
        const updated = { ...this.state.windowStates, [id]: state };
        this.update('windowStates', updated);
    }

    /**
     * Update partial state for a specific window
     */
    updateWindowState(id: string, partial: Partial<WindowState>): void {
        const existing = this.state.windowStates[id];
        if (!existing) {
            log.warn(SEG.UI, `Attempted to update non-existent window state: ${id}`);
            return;
        }
        const updated = { ...this.state.windowStates, [id]: { ...existing, ...partial } };
        this.update('windowStates', updated);
    }

    /**
     * Remove a window from state
     */
    removeWindowState(id: string): void {
        const updated = { ...this.state.windowStates };
        delete updated[id];
        this.update('windowStates', updated);
    }

    /**
     * Get all window states
     */
    getAllWindowStates(): Record<string, WindowState> {
        return this.state.windowStates;
    }

    /**
     * Get list of expanded window IDs (dot-as-primitive)
     * Returns IDs of windows that are currently expanded (not minimized)
     */
    getExpandedWindows(): string[] {
        const windows = this.getAllWindowStates();
        return Object.entries(windows)
            .filter(([_, state]) => !state.minimized)
            .map(([id, _]) => id);
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

        // Persist to localStorage
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
            activeModality: this.state.activeModality,
            usageView: this.state.usageView,
            graphSession: this.state.graphSession,
            windowStates: this.state.windowStates,
            minimizedWindows: this.state.minimizedWindows,
            // Don't persist: panels (should start closed), budgetWarnings (session-only)
        };
    }

    /**
     * Save state to localStorage using storage.ts
     */
    private saveToStorage(): void {
        setItem(STORAGE_KEY, this.getPersistedState(), { version: STORAGE_VERSION });
    }

    /**
     * Load state from localStorage using storage.ts
     */
    private loadFromStorage(): UIStateData | null {
        const persisted = getItem<PersistedUIState>(STORAGE_KEY, {
            version: STORAGE_VERSION,
            maxAge: GRAPH_SESSION_MAX_AGE,
        });

        if (!persisted) return null;

        // Merge persisted preferences with default state
        const defaultState = createDefaultState();
        return {
            ...defaultState,
            activeModality: persisted.activeModality ?? defaultState.activeModality,
            usageView: persisted.usageView ?? defaultState.usageView,
            graphSession: persisted.graphSession ?? defaultState.graphSession,
            windowStates: persisted.windowStates ?? defaultState.windowStates,
            minimizedWindows: persisted.minimizedWindows ?? defaultState.minimizedWindows,
        };
    }

    /**
     * Clear all persisted state
     */
    clearStorage(): void {
        removeItem(STORAGE_KEY);
    }

    /**
     * Reset to default state
     */
    reset(): void {
        this.state = createDefaultState();
        this.clearStorage();

        // Notify all subscribers
        for (const key of Object.keys(this.state) as (keyof UIStateData)[]) {
            this.subscribers.get(key)?.forEach(callback => {
                callback(this.state[key], key);
            });
        }
    }
}

// ============================================================================
// Singleton Export
// ============================================================================

/**
 * Global UI state instance
 */
export const uiState = new UIState();

/**
 * Type-safe state access (convenience export)
 */
export default uiState;
