/**
 * UIState - Centralized UI State Management
 *
 * Consolidates scattered panel visibility and UI state into a single source of truth.
 * Designed to be extensible for future full StateManager implementation.
 *
 * Architecture:
 * - Singleton instance for global access
 * - Simple pub/sub for reactive updates
 * - localStorage persistence for user preferences
 * - Type-safe state access
 *
 * Future: This can be extended to a full StateManager by:
 * - Adding middleware support
 * - Implementing state slices
 * - Adding devtools integration
 */

import type { PanelState } from '../types/core';

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
        lastUpdated: Date.now(),
    };
}

// ============================================================================
// UIState Class
// ============================================================================

const STORAGE_KEY = 'qntx-ui-state';
const STORAGE_VERSION = 1;

/**
 * Centralized UI state manager
 */
class UIState {
    private state: UIStateData;
    private subscribers: Map<keyof UIStateData, Set<StateSubscriber<any>>> = new Map();
    private globalSubscribers: Set<GlobalSubscriber> = new Set();

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
        this.subscribers.get(key)?.forEach(callback => {
            try {
                callback(value, key);
            } catch (e) {
                console.error(`[UIState] Subscriber error for ${key}:`, e);
            }
        });

        // Notify global subscribers
        this.globalSubscribers.forEach(callback => {
            try {
                callback(this.state, key);
            } catch (e) {
                console.error('[UIState] Global subscriber error:', e);
            }
        });

        // Persist to localStorage
        this.saveToStorage();
    }

    // ========================================================================
    // Persistence
    // ========================================================================

    /**
     * Save state to localStorage
     */
    private saveToStorage(): void {
        try {
            const data = {
                version: STORAGE_VERSION,
                state: {
                    // Only persist user preferences, not transient state
                    activeModality: this.state.activeModality,
                    usageView: this.state.usageView,
                    // Don't persist: panels (should start closed), budgetWarnings (session-only)
                },
            };
            localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
        } catch (e) {
            console.error('[UIState] Failed to save state:', e);
        }
    }

    /**
     * Load state from localStorage
     */
    private loadFromStorage(): UIStateData | null {
        try {
            const raw = localStorage.getItem(STORAGE_KEY);
            if (!raw) return null;

            const data = JSON.parse(raw);
            if (data.version !== STORAGE_VERSION) {
                console.warn('[UIState] State version mismatch, using defaults');
                return null;
            }

            // Merge persisted preferences with default state
            const defaultState = createDefaultState();
            return {
                ...defaultState,
                activeModality: data.state.activeModality ?? defaultState.activeModality,
                usageView: data.state.usageView ?? defaultState.usageView,
            };
        } catch (e) {
            console.error('[UIState] Failed to load state:', e);
            return null;
        }
    }

    /**
     * Clear all persisted state
     */
    clearStorage(): void {
        try {
            localStorage.removeItem(STORAGE_KEY);
        } catch (e) {
            console.error('[UIState] Failed to clear storage:', e);
        }
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
