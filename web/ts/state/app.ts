/**
 * Application State - Runtime Graph State
 *
 * Manages transient application state (not persisted to localStorage).
 * For persisted state, see state/ui.ts.
 *
 * State managed here:
 * - Current query and verbosity
 * - Graph data and transform (zoom/pan)
 * - Graph visibility settings (hidden node types, isolated nodes)
 * - Log and progress buffers
 */

import type { AppState } from '../../types/core';

// Buffer limits
export const MAX_LOGS: number = 1000;
export const MAX_PROGRESS: number = 100;

/**
 * Shared runtime state (in-memory, not persisted)
 *
 * Virtue #10: State Locality - Single source of truth for runtime graph state
 */
export const appState: AppState = {
    currentVerbosity: 2,  // Default: Debug (-vv)
    logBuffer: [],
    progressBuffer: [],
    currentQuery: '',
    currentGraphData: null,
    currentTransform: null,
    graphVisibility: {
        hiddenNodeTypes: new Set<string>(),
        hideIsolated: false,
        revealRelatedActive: new Set<string>()
    }
};
