/**
 * System Capabilities Handler
 *
 * Handles system_capabilities WebSocket message to inform UI about available optimizations.
 * Currently tracks fuzzy matching backend (Rust vs Go) and updates ax button visual state.
 */

import type { SystemCapabilitiesMessage } from '../../types/websocket';
import { log, SEG } from '../logger';

/**
 * Handle system capabilities message from backend
 * Updates ax button to show degraded state if using Go fallback
 * Updates vidstream button to show degraded state if ONNX unavailable
 */
export function handleSystemCapabilities(data: SystemCapabilitiesMessage): void {
    log.debug(SEG.PULSE, 'System capabilities received:', {
        fuzzy_backend: data.fuzzy_backend,
        fuzzy_optimized: data.fuzzy_optimized,
        vidstream_backend: data.vidstream_backend,
        vidstream_optimized: data.vidstream_optimized,
    });

    // Handle ax button (fuzzy matching)
    const axButton = document.querySelector('.palette-cell[data-cmd="ax"]') as HTMLElement;
    if (!axButton) {
        console.warn('[System Capabilities] ax button not found');
    } else {
        if (!data.fuzzy_optimized) {
            // Using Go fallback - show degraded state
            axButton.classList.add('degraded');
            axButton.setAttribute('data-fuzzy-backend', 'go');
            axButton.setAttribute('title', '⋈ ax - expand (Go fallback)\nClick for details');
            log.debug(SEG.PULSE, 'Using Go fallback - showing degraded state');
        } else {
            // Using Rust optimization - normal state
            axButton.classList.remove('degraded');
            axButton.setAttribute('data-fuzzy-backend', 'rust');
            axButton.setAttribute('title', '⋈ ax - expand (Rust optimized)');
            log.debug(SEG.PULSE, 'Using Rust optimization');
        }
    }

    // Handle vidstream button (ONNX video inference)
    const vidButton = document.querySelector('.palette-cell[data-cmd="vidstream"]') as HTMLElement;
    if (!vidButton) {
        console.warn('[System Capabilities] vidstream button not found');
    } else {
        if (!data.vidstream_optimized) {
            // ONNX unavailable - show degraded state
            vidButton.classList.add('degraded');
            vidButton.setAttribute('data-vidstream-backend', 'unavailable');
            vidButton.setAttribute('title', '⮀ vidstream - video inference (unavailable - requires CGO build)\nClick for details');
            log.debug(SEG.PULSE, 'ONNX unavailable - showing degraded state');
        } else {
            // ONNX available - normal state
            vidButton.classList.remove('degraded');
            vidButton.setAttribute('data-vidstream-backend', 'onnx');
            vidButton.setAttribute('title', '⮀ vidstream - real-time video inference (ONNX Runtime)');
            log.debug(SEG.PULSE, 'ONNX available');
        }
    }
}
