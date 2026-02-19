/**
 * Application State - Runtime State
 *
 * Manages transient application state (not persisted).
 * For persisted state, see state/ui.ts.
 */

import type { AppState } from '../../types/core';

export const appState: AppState = {
    currentVerbosity: 2,  // Default: Debug (-vv)
    currentQuery: '',
};
