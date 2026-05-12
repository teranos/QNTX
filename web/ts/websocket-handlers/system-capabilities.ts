/**
 * System Capabilities Handler
 *
 * Handles system_capabilities WebSocket message to inform UI about available optimizations.
 * Tracks storage and parser backends. Search will be provided by MeiliSearch
 * via the qntx-meili plugin (ADR-015).
 */

import type { SystemCapabilitiesMessage } from '../../types/websocket';
import { log, SEG } from '../logger';

/**
 * Handle system capabilities message from backend
 * Updates Self diagnostic glyph with system capabilities
 */
export function handleSystemCapabilities(data: SystemCapabilitiesMessage): void {
    log.debug(SEG.PULSE, 'System capabilities received:', {
        storage_backend: data.storage_backend,
        storage_optimized: data.storage_optimized,
        storage_version: data.storage_version,
    });

    // Update Self diagnostic glyph
    import('../default-glyphs.js').then(({ updateSelfCapabilities }) => {
        updateSelfCapabilities(data);
    });
}
