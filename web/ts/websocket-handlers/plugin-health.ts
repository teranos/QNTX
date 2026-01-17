/**
 * Plugin Health Handler
 *
 * Handles plugin_health WebSocket messages and:
 * 1. Displays toast notifications on plugin state changes
 * 2. Updates the plugins button indicator for unhealthy states
 */

import { toast } from '../toast';
import { log, SEG } from '../logger';
import type { PluginHealthMessage } from '../../types/websocket';
import { getButton } from '../components/button';

// Track unhealthy plugins for indicator
const unhealthyPlugins = new Set<string>();

/**
 * Handle plugin health message - display toast, update indicator, and update button state
 */
export function handlePluginHealth(data: PluginHealthMessage): void {
    log.debug(SEG.WS, 'Plugin health update:', data.name, data.state, data.healthy ? 'healthy' : 'unhealthy');

    // Update unhealthy set
    if (!data.healthy) {
        unhealthyPlugins.add(data.name);
    } else {
        unhealthyPlugins.delete(data.name);
    }

    // Update button indicator
    updatePluginButtonIndicator();

    // Update plugin control buttons via registry
    // When state changes, clear loading on the button that triggered the action
    if (data.state === 'paused') {
        // Pause completed - clear loading on the pause button
        const pauseBtn = getButton(`plugin-pause-${data.name}`);
        if (pauseBtn) {
            pauseBtn.setLoading(false);
        }
    } else if (data.state === 'running') {
        // Resume completed - clear loading on the resume button
        const resumeBtn = getButton(`plugin-resume-${data.name}`);
        if (resumeBtn) {
            resumeBtn.setLoading(false);
        }
    }

    // Show appropriate toast based on state
    if (data.state === 'paused') {
        toast.warning(`Plugin "${data.name}" paused`);
    } else if (!data.healthy) {
        toast.error(`Plugin "${data.name}" is unhealthy: ${data.message}`);
    } else if (data.state === 'running' && data.message === 'Plugin resumed') {
        toast.success(`Plugin "${data.name}" resumed`);
    }
}

/**
 * Update the plugins button indicator (red dot for unhealthy)
 */
function updatePluginButtonIndicator(): void {
    const pluginsButton = document.querySelector('[data-cmd="plugins"]') as HTMLElement | null;
    if (!pluginsButton) {
        return;
    }

    const hasUnhealthy = unhealthyPlugins.size > 0;

    // Add or remove indicator
    let indicator = pluginsButton.querySelector('.plugin-health-indicator') as HTMLElement | null;

    if (hasUnhealthy && !indicator) {
        // Add indicator
        indicator = document.createElement('span');
        indicator.className = 'plugin-health-indicator';
        indicator.title = `${unhealthyPlugins.size} plugin(s) unhealthy`;
        pluginsButton.style.position = 'relative';
        pluginsButton.appendChild(indicator);
    } else if (!hasUnhealthy && indicator) {
        // Remove indicator
        indicator.remove();
    } else if (hasUnhealthy && indicator) {
        // Update tooltip
        indicator.title = `${unhealthyPlugins.size} plugin(s) unhealthy`;
    }
}

/**
 * Check if any plugins are unhealthy
 */
export function hasUnhealthyPlugins(): boolean {
    return unhealthyPlugins.size > 0;
}

/**
 * Get count of unhealthy plugins
 */
export function getUnhealthyCount(): number {
    return unhealthyPlugins.size;
}
