/**
 * Shared state management for query elements (AX, Semantic)
 *
 * Query elements share: color tinting for idle/pending/result/error,
 * empty state display, and error display with severity levels.
 */

import { log, SEG } from '../../logger';
import { setResponseState } from './response-state';

/** Color palette for query element states — references tokens.css */
export const QUERY_COLOR_STATES = {
    idle:    { container: 'var(--element-status-idle-bg)',    titleBar: 'var(--element-status-idle-section-bg)' },
    pending: { container: 'var(--element-status-pending-bg)', titleBar: 'var(--element-status-pending-section-bg)' },
    orange:  { container: 'rgba(61, 45, 20, 0.92)',         titleBar: '#5c3d1a' },
    teal:    { container: 'rgba(31, 61, 61, 0.92)',         titleBar: '#1f3d3d' },
} as const;

export type QueryColorState = keyof typeof QUERY_COLOR_STATES;

/**
 * Create a color state setter for a query element.
 * Applies matching background colors to both the container and title bar.
 */
export function createColorStateSetter(
    element: HTMLElement,
    titleBar: HTMLElement,
): (state: QueryColorState) => void {
    return (state) => {
        // A new query state means the last answer no longer stands.
        setResponseState(element, null);
        element.style.backgroundColor = QUERY_COLOR_STATES[state].container;
        titleBar.style.backgroundColor = QUERY_COLOR_STATES[state].titleBar;
    };
}

/**
 * Append an empty state placeholder to a results container.
 */
export function appendEmptyState(container: HTMLElement, className: string): void {
    const empty = document.createElement('div');
    empty.className = className;
    empty.textContent = 'No matches yet';
    empty.style.color = 'var(--text-secondary)';
    empty.style.textAlign = 'center';
    empty.style.padding = '20px';
    container.appendChild(empty);
}

/**
 * Show an error in a query element's results container.
 * Removes any existing empty state and previous error first.
 * Tints the element background to indicate error severity.
 */
export function showQueryError(
    elementElement: HTMLElement,
    resultsContainer: HTMLElement,
    emptyStateClass: string,
    errorClass: string,
    severity: string,
    errorMsg: string,
    label: string,
    elementId: string,
    details?: string[],
): void {
    // Remove empty state if present
    const emptyState = resultsContainer.querySelector(`.${emptyStateClass}`);
    if (emptyState) emptyState.remove();

    // Remove existing error display if present
    const existingError = resultsContainer.querySelector(`.${errorClass}`);
    if (existingError) existingError.remove();

    // Create error display
    const errorDisplay = document.createElement('div');
    errorDisplay.className = errorClass;
    errorDisplay.style.padding = '6px 8px';
    errorDisplay.style.fontSize = '11px';
    errorDisplay.style.fontFamily = 'var(--font-mono)';
    errorDisplay.style.backgroundColor = severity === 'error' ? 'var(--element-status-error-section-bg)' : 'var(--element-status-warning-section-bg)';
    errorDisplay.style.color = severity === 'error' ? 'var(--element-status-error-text)' : 'var(--element-status-warning-text)';
    errorDisplay.style.whiteSpace = 'pre-wrap';
    errorDisplay.style.wordBreak = 'break-word';
    errorDisplay.style.overflowWrap = 'anywhere';
    errorDisplay.style.maxWidth = '100%';

    errorDisplay.textContent = `${severity.toUpperCase()}: ${errorMsg}`;

    if (details && details.length > 0) {
        errorDisplay.textContent += '\n\n' + details.map(d => `  ${d}`).join('\n');
    }

    resultsContainer.insertBefore(errorDisplay, resultsContainer.firstChild);

    setResponseState(elementElement, severity === 'error' ? 'error' : 'warning');

    log.debug(SEG.ELEMENT, `[${label}] Displayed ${severity} for ${elementId}:`, errorMsg);
}
