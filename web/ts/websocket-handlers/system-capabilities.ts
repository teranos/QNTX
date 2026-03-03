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
 * Updates Self diagnostic window with system capabilities
 */
export function handleSystemCapabilities(data: SystemCapabilitiesMessage): void {
    log.debug(SEG.PULSE, 'System capabilities received:', {
        fuzzy_backend: data.fuzzy_backend,
        fuzzy_optimized: data.fuzzy_optimized,
        fuzzy_version: data.fuzzy_version,
        vidstream_backend: data.vidstream_backend,
        vidstream_optimized: data.vidstream_optimized,
        vidstream_version: data.vidstream_version,
        storage_backend: data.storage_backend,
        storage_optimized: data.storage_optimized,
        storage_version: data.storage_version,
    });

    // Update Self diagnostic window
    import('../self-window.js').then(({ selfWindow }) => {
        selfWindow.updateCapabilities(data);
    });

    // Update Self diagnostic glyph
    import('../default-glyphs.js').then(({ updateSelfCapabilities }) => {
        updateSelfCapabilities(data);
    });

    // Handle ax button (fuzzy matching)
    const axButton = document.querySelector('.palette-cell[data-cmd="ax"]') as HTMLElement;
    if (!axButton) {
        log.warn(SEG.UI, '[System Capabilities] ax button not found');
    } else {
        if (!data.fuzzy_optimized) {
            // Using Go fallback - show degraded state
            axButton.classList.add('degraded');
            axButton.setAttribute('data-fuzzy-backend', 'go');
            axButton.setAttribute('data-tooltip', `⋈ Expand - contextual query\n${data.fuzzy_backend} fallback (Go)\nClick for details`);
            log.debug(SEG.PULSE, 'Using Go fallback - showing degraded state');
        } else {
            // Using Rust optimization - normal state
            axButton.classList.remove('degraded');
            axButton.setAttribute('data-fuzzy-backend', 'rust');
            axButton.setAttribute('data-tooltip', `⋈ Expand - contextual query\nfuzzy-ax v${data.fuzzy_version} (${data.fuzzy_backend})`);
            log.debug(SEG.PULSE, 'Using Rust optimization');
        }
    }

    // VidStream capabilities logged for diagnostics (UI moved to vidstream plugin glyph)
    if (data.vidstream_optimized) {
        log.debug(SEG.PULSE, `VidStream ONNX available: v${data.vidstream_version}`);
    }
}
