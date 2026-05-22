/**
 * HTTP utilities — status extraction and URL manipulation using string methods (no regex).
 */

/**
 * Extract an HTTP status code from an error message string.
 * Looks for patterns like "HTTP 404", "HTTP404", "http 500".
 * Returns the status code as a number, or null if not found.
 */
export function extractHttpStatus(message: string): number | null {
    const upper = message.toUpperCase();
    let pos = upper.indexOf('HTTP');
    while (pos !== -1) {
        // Skip past "HTTP" and any whitespace
        let i = pos + 4;
        while (i < message.length && message[i] === ' ') i++;
        // Check for exactly 3 digits
        if (i + 3 <= message.length &&
            message[i] >= '0' && message[i] <= '9' &&
            message[i + 1] >= '0' && message[i + 1] <= '9' &&
            message[i + 2] >= '0' && message[i + 2] <= '9' &&
            (i + 3 >= message.length || message[i + 3] < '0' || message[i + 3] > '9')) {
            return parseInt(message.slice(i, i + 3), 10);
        }
        pos = upper.indexOf('HTTP', pos + 1);
    }
    return null;
}

/**
 * Strip the protocol (http:// or https://) from a URL, returning the host and path.
 */
export function stripProtocol(url: string): string {
    if (url.startsWith('https://')) return url.slice(8);
    if (url.startsWith('http://')) return url.slice(7);
    return url;
}

/**
 * Throw if response is not OK. Includes status code and response body in the error.
 */
export async function assertOk(response: Response, context: string): Promise<void> {
    if (response.ok) return;
    const body = await response.text().catch(() => '');
    throw new Error(`${context}: HTTP ${response.status} ${body || response.statusText}`);
}

/**
 * Build RequestInit for JSON body requests (POST, PUT, PATCH).
 */
export function jsonBody(method: string, data: unknown): RequestInit {
    return {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    };
}
