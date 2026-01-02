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
