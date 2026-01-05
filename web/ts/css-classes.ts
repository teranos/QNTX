/**
 * CSS Class Constants
 *
 * Centralized CSS class names for type safety and maintainability.
 * Import and use these constants instead of magic strings.
 *
 * @example
 * import { CSS } from './css-classes.ts';
 * element.classList.add(CSS.STATE.HIDDEN);
 */

export const CSS = {
    /** Component state classes */
    STATE: {
        HIDDEN: 'hidden',
        VISIBLE: 'visible',
        ACTIVE: 'active',
        COLLAPSED: 'collapsed',
        SELECTED: 'selected',
        DRAGGING: 'dragging',
        EXPANDED: 'expanded',
        FADING: 'fading',
        DISCONNECTED: 'disconnected',
    },

    /** Log panel classes */
    LOG: {
        LINE: 'log-line',
        ERROR: 'log-error',
        WARN: 'log-warn',
        INFO: 'log-info',
        DEBUG: 'log-debug',
        TIMESTAMP: 'log-timestamp',
        LOGGER: 'log-logger',
        MESSAGE: 'log-message',
        FIELDS: 'log-fields',
    },

    /** Toast notification classes */
    TOAST: {
        CONTAINER: 'toast-container',
        BASE: 'toast',
        TITLE: 'toast-title',
        VISIBLE: 'toast-visible',
        DISMISSING: 'toast-dismissing',
    },

    /** Panel classes */
    PANEL: {
        SLIDE_LEFT: 'panel-slide-left',
        OVERLAY: 'panel-overlay',
        HEADER: 'panel-header',
        TITLE: 'panel-title',
        CLOSE: 'panel-close',
        CONTENT: 'panel-content',
        ERROR: 'panel-error',
        EMPTY: 'panel-empty',
        LOADING: 'panel-loading',
    },

    /** Stream classes */
    STREAM: {
        ACTIVE: 'stream-active',
        ERROR: 'stream-error',
        COMPLETE: 'stream-complete',
    },

    /** Filter classes */
    FILTER: {
        ITEM: 'filter-item',
        SELECTED: 'selected',
    },

    /** Job/Pulse classes */
    JOB: {
        STATE_ACTIVE: 'job-state-active',
        STATE_INACTIVE: 'job-state-inactive',
    },
} as const;

/** TypeScript types for compile-time checking */
export type StateClass = typeof CSS.STATE[keyof typeof CSS.STATE];
export type LogClass = typeof CSS.LOG[keyof typeof CSS.LOG];
export type ToastClass = typeof CSS.TOAST[keyof typeof CSS.TOAST];
export type PanelClass = typeof CSS.PANEL[keyof typeof CSS.PANEL];
export type StreamClass = typeof CSS.STREAM[keyof typeof CSS.STREAM];

// ============================================================================
// Data Attribute State Management
// ============================================================================

/**
 * Data attribute values for component state
 * Use data-state attribute instead of multiple classes for clearer state management
 *
 * Benefits (per issue #114):
 * - Single attribute vs many classes
 * - Easier debugging (inspect shows data-state="hidden")
 * - Prevents conflicting states
 * - Better DevTools filtering
 *
 * @example
 * import { DATA, setDataState } from './css-classes.ts';
 * setDataState(element, 'visibility', DATA.VISIBILITY.HIDDEN);
 * // Results in: <div data-visibility="hidden">
 *
 * CSS:
 * [data-visibility="hidden"] { display: none; }
 * [data-visibility="visible"] { display: block; }
 */
export const DATA = {
    /** Visibility states */
    VISIBILITY: {
        HIDDEN: 'hidden',
        VISIBLE: 'visible',
        FADING: 'fading',
    },

    /** Expansion states */
    EXPANSION: {
        COLLAPSED: 'collapsed',
        EXPANDED: 'expanded',
    },

    /** Active/selection states */
    ACTIVE: {
        INACTIVE: 'inactive',
        ACTIVE: 'active',
        SELECTED: 'selected',
    },

    /** Loading states */
    LOADING: {
        IDLE: 'idle',
        LOADING: 'loading',
        ERROR: 'error',
        SUCCESS: 'success',
    },
} as const;

export type VisibilityState = typeof DATA.VISIBILITY[keyof typeof DATA.VISIBILITY];
export type ExpansionState = typeof DATA.EXPANSION[keyof typeof DATA.EXPANSION];
export type ActiveState = typeof DATA.ACTIVE[keyof typeof DATA.ACTIVE];
export type LoadingState = typeof DATA.LOADING[keyof typeof DATA.LOADING];

/**
 * Set a data-* attribute state on an element
 */
export function setDataState(
    element: HTMLElement | null | undefined,
    attribute: string,
    value: string
): void {
    if (element) {
        element.dataset[attribute] = value;
    }
}

/**
 * Get a data-* attribute state from an element
 */
export function getDataState(
    element: HTMLElement | null | undefined,
    attribute: string
): string | undefined {
    return element?.dataset[attribute];
}

/**
 * Clear a data-* attribute from an element
 */
export function clearDataState(
    element: HTMLElement | null | undefined,
    attribute: string
): void {
    if (element) {
        delete element.dataset[attribute];
    }
}
