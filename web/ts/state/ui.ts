/**
 * UIState - Singleton Export
 *
 * Re-exports everything from ui-impl.ts (types, class) and creates the
 * global singleton instance. All production code imports from this file.
 *
 * Test files that need the real UIState class (not the mock) import
 * directly from ui-impl.ts — see ui-impl.ts header for details.
 */

export * from './ui-impl';
import { UIState } from './ui-impl';

/**
 * Global UI state instance
 */
export const uiState = new UIState();

/**
 * Type-safe state access (convenience export)
 */
export default uiState;
