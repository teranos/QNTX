/**
 * ATS Parse Client — semantic highlighting via parse_response over main WebSocket.
 *
 * The Rust classifier (qntx-core::semantic) now powers token classification
 * on both server (wazero) and browser (wasm-bindgen). This client sends
 * parse_request, receives classified tokens, and applies CodeMirror decorations.
 */

import { sendMessage } from './websocket.ts';
import { applySyntaxHighlighting, updateDiagnosticsDisplay } from './codemirror-editor.ts';
import type { ParseRequest } from '../types/lsp';
import type { ParseResponseMessage } from '../types/websocket';

// Debounce timings
// TODO(issue #14): Tune these values based on actual latency metrics
export const PARSE_DEBOUNCE_MS: number = 150;      // Fast feedback for highlighting

// State
let parseTimeout: ReturnType<typeof setTimeout> | null = null;
let lastParseResponse: ParseResponseMessage | null = null;

/**
 * Request parse with semantic tokens (debounced)
 * @param query - The ATS query to parse
 * @param line - Current cursor line (1-based)
 * @param cursor - Current cursor column (0-based)
 */
export function requestParse(query: string, line?: number, cursor?: number): void {
    if (parseTimeout) {
        clearTimeout(parseTimeout);
    }

    parseTimeout = setTimeout(() => {
        const request: ParseRequest = {
            type: 'parse_request',
            query: query,
            line: line || 1,
            cursor: cursor || 0,
            timestamp: Date.now()
        };
        sendMessage(request);
    }, PARSE_DEBOUNCE_MS);
}

/**
 * Handle parse response from server - update syntax highlighting and diagnostics
 * @param response - Parse response containing tokens and diagnostics
 */
export function handleParseResponse(response: ParseResponseMessage): void {
    lastParseResponse = response;

    // Update syntax highlighting with semantic tokens (CodeMirror decorations)
    if (response.tokens && response.tokens.length > 0) {
        applySyntaxHighlighting(response.tokens);
    }

    // Update diagnostics with inline error squiggles (CodeMirror linter)
    updateDiagnosticsDisplay(response.diagnostics || []);

    // Store parse state for future use
    // Type declaration for global window property
    if (response.parse_state) {
        (window as any).atsParseState = response.parse_state;
    }
}

/**
 * Update highlight layer with plain text (fallback before parse response)
 * @param text - Plain text to display in highlight layer
 */
export function updatePlainText(text: string): void {
    const highlightLayer = document.getElementById('syntax-highlight-layer') as HTMLElement | null;
    if (!highlightLayer) return;

    // Show plain text immediately (will be replaced by semantic tokens)
    highlightLayer.textContent = text;
}

/**
 * Get last parse response (for debugging)
 * @returns The last parse response received, or null if none
 */
export function getLastParseResponse(): ParseResponseMessage | null {
    return lastParseResponse;
}

// Type augmentation for window object
declare global {
    interface Window {
        atsParseState?: unknown;
    }
}