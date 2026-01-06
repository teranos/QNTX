/**
 * HTML Utilities
 *
 * Shared utilities for HTML escaping, formatting, and display.
 * Extracted from duplicated implementations across panel components.
 */

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
 * Format timestamp as relative time string
 *
 * Converts RFC3339/ISO 8601 timestamps to human-readable relative time.
 * Shows past times as "5m ago", "2h ago", "3d ago".
 * Shows future times as "5m from now", "2h from now".
 *
 * @param timestamp - RFC3339 or ISO 8601 timestamp string
 * @returns Relative time string (e.g., "5m ago", "2h ago", "3d ago")
 *
 * @example
 * formatRelativeTime('2024-01-01T12:00:00Z') // "5m ago" (if current time is 12:05:00)
 * formatRelativeTime('2024-01-01T14:00:00Z') // "2h from now" (if current time is 12:00:00)
 */
export function formatRelativeTime(timestamp: string): string {
    const date = new Date(timestamp);
    const now = Date.now();
    const diffMs = now - date.getTime();
    const absDiff = Math.abs(diffMs);
    const isPast = diffMs > 0;

    const seconds = Math.floor(absDiff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    let timeStr: string;
    if (days > 0) {
        timeStr = `${days}d`;
    } else if (hours > 0) {
        timeStr = `${hours}h`;
    } else if (minutes > 0) {
        timeStr = `${minutes}m`;
    } else {
        timeStr = `${seconds}s`;
    }

    return isPast ? `${timeStr} ago` : `${timeStr} from now`;
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
 * Simple keyword-based detection matching config-panel.ts implementation.
 * Checks if value contains common secret-related keywords.
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
