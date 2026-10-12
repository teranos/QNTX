/**
 * HTML Utilities
 *
 * Shared utilities for HTML escaping, formatting, and display.
 * Extracted from duplicated implementations across panel components.
 */

import { readout, relative, shortAll } from './span';

/**
 * Escape HTML special characters to prevent XSS
 *
 * Uses browser's built-in text content escaping via DOM API.
 * This is the standard secure approach for escaping HTML.
 *
 * @param text - Text to escape
 * @returns HTML-safe string
 *
 * @example
 * escapeHtml('<script>alert("xss")</script>')
 * // Returns: '&lt;script&gt;alert("xss")&lt;/script&gt;'
 */
export function escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

/**
 * Format ISO/RFC3339 timestamp as relative time ("5m ago", "in 2h"), in the
 * span grammar the ax language reads back (span.ts).
 */
export function formatRelativeTime(timestamp: string): string {
    return relative(new Date(timestamp).getTime(), Date.now());
}

/**
 * Format Unix epoch seconds as relative time ("5m ago").
 * Returns "never" for 0/falsy input.
 */
export function formatRelativeTimeUnix(unixSeconds: number): string {
    if (!unixSeconds) return 'never';
    return relative(unixSeconds * 1000, Date.now());
}

/**
 * Format a build timestamp as "Xm ago (locale date string)".
 * Accepts RFC3339 strings, Unix epoch seconds (number), or Unix epoch as string.
 * Returns null for invalid/missing input.
 */
export function formatBuildTime(timestamp?: string | number): string | null {
    if (!timestamp || timestamp === 'dev' || timestamp === 'unknown') return null;

    let date: Date;
    if (typeof timestamp === 'number') {
        date = new Date(timestamp * 1000);
    } else {
        date = new Date(timestamp);
        if (isNaN(date.getTime())) {
            const epochSeconds = parseInt(timestamp, 10);
            if (!isNaN(epochSeconds)) {
                date = new Date(epochSeconds * 1000);
            }
        }
    }
    if (isNaN(date.getTime())) return null;

    const diffMs = Date.now() - date.getTime();
    if (diffMs < 0) return date.toLocaleString();

    const said = diffMs < 60000 ? 'just now' : relative(date.getTime(), Date.now());
    return `${said} (${date.toLocaleString()})`;
}

/**
 * Format value for display in configuration/settings panels
 *
 * Handles various types with appropriate formatting and styling:
 * - null/undefined: styled as "null"
 * - booleans: styled with bool class
 * - numbers: styled as numeric values
 * - objects: JSON stringified
 * - strings: escaped HTML with potential secret masking
 *
 * @param value - Value to format
 * @param maskSecrets - If true, mask strings that look like secrets (default: false)
 * @returns HTML string with appropriate styling classes
 *
 * @example
 * formatValue(null) // '<span class="config-value-null">null</span>'
 * formatValue(true) // '<span class="config-value-bool">true</span>'
 * formatValue(42) // '<span class="config-value-number">42</span>'
 * formatValue('hello') // '<span class="config-value-string">hello</span>'
 * formatValue('my_api_key', true) // '<span class="config-value-secret">********</span>'
 */
export function formatValue(value: unknown, maskSecrets: boolean = false): string {
    if (value === null || value === undefined) {
        return '<span class="config-value-null">null</span>';
    }

    if (typeof value === 'boolean') {
        return `<span class="config-value-bool">${value}</span>`;
    }

    if (typeof value === 'number') {
        return `<span class="config-value-number">${value}</span>`;
    }

    if (typeof value === 'object') {
        return `<span class="config-value-object">${JSON.stringify(value)}</span>`;
    }

    const str = String(value);

    // Optionally mask secrets (API keys, tokens, passwords)
    if (maskSecrets && looksLikeSecret(str)) {
        return '<span class="config-value-secret">********</span>';
    }

    return `<span class="config-value-string">${escapeHtml(str)}</span>`;
}

/**
 * Check if string looks like a secret (API key, token, password)
 *
 * Simple keyword-based detection: whether the value contains common
 * secret-related keywords.
 *
 * @param value - String to check
 * @returns True if string contains secret-related keywords
 */
function looksLikeSecret(value: string): boolean {
    const str = String(value).toLowerCase();
    return (
        str.includes('token') ||
        str.includes('key') ||
        str.includes('secret') ||
        str.includes('password') ||
        str.includes('bearer')
    );
}

/**
 * Format timestamp as locale time string (HH:MM:SS)
 *
 * Provides consistent time formatting across the application with
 * 24-hour format and milliseconds.
 *
 * @param timestamp - ISO timestamp string or Date object
 * @returns Formatted time string (e.g., "14:30:45.123")
 *
 * @example
 * formatTimestamp("2024-01-15T14:30:45.123Z") // "14:30:45.123"
 */
export function formatTimestamp(timestamp: string | Date): string {
    const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp;
    return date.toLocaleTimeString('en-US', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        fractionalSecondDigits: 3
    } as Intl.DateTimeFormatOptions);
}

/**
 * Format date and time for display (e.g., "Jan 15, 2024 14:30")
 *
 * Combines date and time formatting for consistent timestamp display.
 * Uses short month, numeric day/year, and 24-hour time format.
 *
 * @param timestamp - ISO timestamp string or Date object
 * @returns Formatted datetime string
 *
 * @example
 * formatDateTime("2024-01-15T14:30:00Z") // "Jan 15, 2024 14:30"
 */
export function formatDateTime(timestamp: string | Date): string {
    const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp;
    const dateStr = date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric'
    });
    const timeStr = date.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
    });
    return `${dateStr} ${timeStr}`;
}

/**
 * Strip HTML tags from a string and return plain text
 *
 * @param html - HTML string to strip
 * @returns Plain text without HTML tags
 *
 * @example
 * stripHtml("<b>Hello</b> world") // "Hello world"
 * stripHtml("<div>Test</div>") // "Test"
 */
export function stripHtml(html: string): string {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    return doc.body.textContent || '';
}

/**
 * Format duration in milliseconds as a two-unit readout in the span grammar
 * the ax language reads back (span.ts): "150ms", "5s", "2m30s".
 *
 * @param durationMs - Duration in milliseconds
 */
export function formatDuration(durationMs: number): string {
    return shortAll(readout(durationMs, 'millisecond'));
}

/**
 * Options for {@link el}.
 */
export interface ElOptions {
    /** Inline styles as a camelCase property object (e.g. `{ marginBottom: '8px' }`). */
    style?: Partial<CSSStyleDeclaration>;
    /** Sets `textContent`. Applied before any children are appended. */
    text?: string;
    /** Sets `className`. */
    class?: string;
}

/**
 * Create an HTML element with style, text, class, and children in one call.
 *
 * Collapses the createElement-plus-assignments boilerplate:
 *
 *     const row = document.createElement('div');
 *     row.className = 'time-row';
 *     row.style.display = 'flex';
 *     row.style.gap = '6px';
 *     row.textContent = label;
 *
 * into:
 *
 *     const row = el('div', { class: 'time-row', style: { display: 'flex', gap: '6px' }, text: label });
 *
 * @param tag - HTML tag name
 * @param options - style (camelCase keys), text (textContent), class (className)
 * @param children - nodes or strings appended after text is set
 * @returns The created element, typed to the tag
 */
export function el<K extends keyof HTMLElementTagNameMap>(
    tag: K,
    options?: ElOptions,
    children?: ReadonlyArray<Node | string>,
): HTMLElementTagNameMap[K] {
    const element = document.createElement(tag);
    if (options?.style) {
        Object.assign(element.style, options.style);
    }
    if (options?.text !== undefined) {
        element.textContent = options.text;
    }
    if (options?.class !== undefined) {
        element.className = options.class;
    }
    if (children) {
        element.append(...children);
    }
    return element;
}
