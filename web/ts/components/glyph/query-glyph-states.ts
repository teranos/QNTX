/**
 * Shared state management for query glyphs (AX, Semantic)
 *
 * Query glyphs share: color tinting for idle/pending/result/error,
 * empty state display, and error display with severity levels.
 */

import { log, SEG } from '../../logger';

/** Color palette for query glyph states */
export const QUERY_COLOR_STATES = {
    idle:    { container: 'rgba(30, 30, 35, 0.92)',  titleBar: 'var(--bg-tertiary)' },
    pending: { container: 'rgba(42, 43, 61, 0.92)',  titleBar: 'rgba(42, 43, 61, 0.92)' },
    orange:  { container: 'rgba(61, 45, 20, 0.92)',  titleBar: '#5c3d1a' },
    teal:    { container: 'rgba(31, 61, 61, 0.92)',  titleBar: '#1f3d3d' },
} as const;

export type QueryColorState = keyof typeof QUERY_COLOR_STATES;

/**
 * Create a color state setter for a query glyph.
 * Applies matching background colors to both the container and title bar.
 */
export function createColorStateSetter(
    element: HTMLElement,
    titleBar: HTMLElement,
): (state: QueryColorState) => void {
    return (state) => {
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
 * Show an error in a query glyph's results container.
 * Removes any existing empty state and previous error first.
 * Tints the glyph background to indicate error severity.
 */
export function showQueryError(
    glyphElement: HTMLElement,
    resultsContainer: HTMLElement,
    emptyStateClass: string,
    errorClass: string,
    severity: string,
    errorMsg: string,
    label: string,
    glyphId: string,
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
    errorDisplay.style.fontFamily = 'monospace';
    errorDisplay.style.backgroundColor = severity === 'error' ? 'var(--glyph-status-error-section-bg)' : '#2b2b1a';
    errorDisplay.style.color = severity === 'error' ? '#ff9999' : '#ffcc66';
    errorDisplay.style.whiteSpace = 'pre-wrap';
    errorDisplay.style.wordBreak = 'break-word';
    errorDisplay.style.overflowWrap = 'anywhere';
    errorDisplay.style.maxWidth = '100%';

    errorDisplay.textContent = `${severity.toUpperCase()}: ${errorMsg}`;

    if (details && details.length > 0) {
        errorDisplay.textContent += '\n\n' + details.map(d => `  ${d}`).join('\n');
    }

    resultsContainer.insertBefore(errorDisplay, resultsContainer.firstChild);

    // Tint glyph + title bar to indicate error
    const errorBg = severity === 'error' ? 'rgba(61, 31, 31, 0.92)' : 'rgba(61, 61, 31, 0.92)';
    const errorTitleBg = severity === 'error' ? '#3d1f1f' : '#3d3d1f';
    glyphElement.style.backgroundColor = errorBg;
    const titleBar = glyphElement.querySelector('.glyph-title-bar') as HTMLElement;
    if (titleBar) titleBar.style.backgroundColor = errorTitleBg;

    log.debug(SEG.GLYPH, `[${label}] Displayed ${severity} for ${glyphId}:`, errorMsg);
}
