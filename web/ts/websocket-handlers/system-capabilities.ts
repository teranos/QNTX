/**
 * System Capabilities Handler
 *
 * Handles system_capabilities WebSocket message to inform UI about available optimizations.
 * Currently tracks fuzzy matching backend (Rust vs Go) and updates ax button visual state.
 */

import type { SystemCapabilitiesMessage } from '../../types/websocket';
import { debugLog } from '../debug.ts';

/**
 * Handle system capabilities message from backend
 * Updates ax button to show degraded state if using Go fallback
 */
export function handleSystemCapabilities(data: SystemCapabilitiesMessage): void {
    debugLog('[System Capabilities] Received:', {
        fuzzy_backend: data.fuzzy_backend,
        fuzzy_optimized: data.fuzzy_optimized,
    });

    // Get the ax symbol button
    const axButton = document.querySelector('.palette-cell[data-cmd="ax"]') as HTMLElement;
    if (!axButton) {
        console.warn('[System Capabilities] ax button not found');
        return;
    }

    // Add or remove degraded class based on optimization status
    if (!data.fuzzy_optimized) {
        // Using Go fallback - show degraded state
        axButton.classList.add('degraded');
        axButton.setAttribute('data-fuzzy-backend', 'go');
        axButton.setAttribute('title', '⋈ ax - expand (Go fallback)\nClick for details');
        debugLog('[System Capabilities] ax using Go fallback - showing degraded state');
    } else {
        // Using Rust optimization - normal state
        axButton.classList.remove('degraded');
        axButton.setAttribute('data-fuzzy-backend', 'rust');
        axButton.setAttribute('title', '⋈ ax - expand (Rust optimized)');
        debugLog('[System Capabilities] ax using Rust optimization');
    }
}
