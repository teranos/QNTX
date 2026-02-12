/**
 * Visual Mode Manager
 *
 * Manages CSS-based visual mode switching in response to connectivity changes.
 * Updates the root element's data-connectivity-mode attribute, which triggers
 * CSS custom property changes for mode-specific styling.
 */

import { connectivityManager, type ConnectivityState } from './connectivity';
import { log, SEG } from './logger';

/**
 * Initialize visual mode system
 * Subscribes to connectivity changes and updates root element attribute
 */
export function initVisualMode(): void {
    connectivityManager.subscribe((state: ConnectivityState) => {
        log.debug(SEG.UI, `[VisualMode] Connectivity mode changed to: ${state}`);
        document.documentElement.setAttribute('data-connectivity-mode', state);
    });
}
