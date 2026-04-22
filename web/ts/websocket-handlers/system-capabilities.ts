/**
 * System Capabilities Handler
 *
 * Handles system_capabilities WebSocket message to inform UI about available
 * optimizations (Rust storage backend, WASM parser).
 */

import type { SystemCapabilitiesMessage } from '../../types/websocket';
import { log, SEG } from '../logger';

/**
 * Handle system capabilities message from backend.
 * Updates Self diagnostic glyph.
 */
export function handleSystemCapabilities(data: SystemCapabilitiesMessage): void {
    log.debug(SEG.PULSE, 'System capabilities received:', {
        storage_backend: data.storage_backend,
        storage_optimized: data.storage_optimized,
        storage_version: data.storage_version,
        parser_backend: data.parser_backend,
        parser_optimized: data.parser_optimized,
        parser_version: data.parser_version,
    });

    // Update Self diagnostic glyph
    import('../default-glyphs.js').then(({ updateSelfCapabilities }) => {
        updateSelfCapabilities(data);
    });
}
