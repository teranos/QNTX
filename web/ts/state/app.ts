/**
 * Application State - Runtime State
 *
 * Manages transient application state (not persisted).
 * For persisted state, see state/ui.ts.
 */

import type { AppState } from '../../types/core';

// Buffer limits
export const MAX_LOGS: number = 1000;
export const MAX_PROGRESS: number = 100;

export const appState: AppState = {
    currentVerbosity: 2,  // Default: Debug (-vv)
    logBuffer: [],
    progressBuffer: [],
    currentQuery: '',
};
