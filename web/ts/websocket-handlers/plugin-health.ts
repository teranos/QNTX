/**
 * Plugin Health Handler
 *
 * Handles plugin_health WebSocket messages.
 * Dispatches 'plugin-health-change' events so the plugin panel can live-update.
 */

import { log, SEG } from '../logger';
import type { PluginHealthMessage } from '../../types/websocket';

// Track unhealthy plugins
const unhealthyPlugins = new Set<string>();

/**
 * Handle plugin health message
 */
export function handlePluginHealth(data: PluginHealthMessage): void {
    log.debug(SEG.WS, 'Plugin health update:', data.name, data.state, data.healthy ? 'healthy' : 'unhealthy');

    if (!data.healthy) {
        unhealthyPlugins.add(data.name);
    } else {
        unhealthyPlugins.delete(data.name);
    }

    // Notify plugin panel (or anything else listening) to refresh
    document.dispatchEvent(new CustomEvent('plugin-health-change', { detail: data }));
}

export function hasUnhealthyPlugins(): boolean {
    return unhealthyPlugins.size > 0;
}

export function getUnhealthyCount(): number {
    return unhealthyPlugins.size;
}
