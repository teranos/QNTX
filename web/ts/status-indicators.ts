/**
 * Status Indicators Module
 *
 * Manages status indicators in the system drawer header.
 * Provides a clean interface for adding various status indicators
 * (WebSocket connection, Pulse daemon, future services, etc.)
 */

import { sendMessage } from './websocket.ts';
import { toast } from './toast.ts';
import type { DaemonStatusMessage } from '../types/websocket';
import { DB } from '@generated/sym.js';

interface StatusIndicator {
    id: string;
    label: string;
    clickable: boolean;
    onClick?: () => void;
    initialState?: 'active' | 'inactive' | 'connecting';
}

class StatusIndicatorManager {
    private container: HTMLElement | null = null;
    private indicators: Map<string, HTMLElement> = new Map();

    /**
     * Initialize the status indicator system
     */
    init(): void {
        // Find or create container in the system drawer header
        const logHeader = document.getElementById('system-drawer-header');
        if (!logHeader) return;

        // Remove old hardcoded status indicators from HTML
        const oldConnectionStatus = document.getElementById('connection-status');
        const oldPulseStatus = document.getElementById('pulse-status');
        if (oldConnectionStatus) oldConnectionStatus.remove();
        if (oldPulseStatus) oldPulseStatus.remove();

        // Create new container for status indicators
        this.container = document.createElement('div');
        this.container.id = 'status-indicators';
        this.container.className = 'status-indicators';

        // Insert before controls div (system-version is now inside controls)
        const controls = logHeader.querySelector('.controls');
        if (controls) {
            logHeader.insertBefore(this.container, controls);
        } else {
            logHeader.appendChild(this.container);
        }

        // Add default indicators
        this.addConnectionIndicator();
        this.addPulseIndicator();
        this.addDatabaseIndicator();
    }

    /**
     * Add a new status indicator
     */
    addIndicator(config: StatusIndicator): HTMLElement {
        if (!this.container) {
            throw new Error('Status indicator manager not initialized');
        }

        // Create indicator element
        const indicator = document.createElement('div');
        indicator.id = `${config.id}-status`;
        indicator.className = 'status-indicator';

        if (config.clickable) {
            indicator.setAttribute('role', 'button');
            indicator.setAttribute('tabindex', '0');
            indicator.classList.add('clickable');

            if (config.onClick) {
                indicator.addEventListener('click', config.onClick);

                // Keyboard support
                indicator.addEventListener('keydown', (e: KeyboardEvent) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        config.onClick!();
                    }
                });
            }
        } else {
            indicator.setAttribute('role', 'status');
        }

        indicator.setAttribute('aria-live', 'polite');

        // Add dot and text
        const dot = document.createElement('span');
        dot.className = 'status-dot';
        dot.id = `${config.id}-dot`;

        const text = document.createElement('span');
        text.className = 'status-text';
        text.id = `${config.id}-text`;
        text.textContent = config.label;

        indicator.appendChild(dot);
        indicator.appendChild(text);

        // Add to container
        this.container.appendChild(indicator);
        this.indicators.set(config.id, indicator);

        // Set initial state if provided
        if (config.initialState) {
            this.updateIndicator(config.id, config.initialState, config.label);
        }

        return indicator;
    }

    /**
     * Add WebSocket connection indicator
     */
    private addConnectionIndicator(): void {
        this.addIndicator({
            id: 'connection',
            label: 'Connecting...',
            clickable: false,
            initialState: 'connecting'
        });
    }

    /**
     * Add Pulse daemon indicator
     */
    private addPulseIndicator(): void {
        // Disable touch interactions on mobile (max-width: 768px)
        const isMobile = window.matchMedia('(max-width: 768px)').matches;

        this.addIndicator({
            id: 'pulse',
            label: 'Pulse: OFF',
            clickable: !isMobile,
            onClick: isMobile ? undefined : () => this.togglePulseDaemon(),
            initialState: 'inactive'
        });
    }

    /**
     * Add Database indicator
     */
    private addDatabaseIndicator(): void {
        this.addIndicator({
            id: 'database',
            label: `${DB} Loading...`,
            clickable: true,
            onClick: () => this.showDatabaseInfo(),
            initialState: 'active'
        });
    }

    /**
     * Update an indicator's state
     */
    updateIndicator(id: string, state: string, label?: string): void {
        const indicator = this.indicators.get(id);
        if (!indicator) return;

        const text = indicator.querySelector('.status-text') as HTMLElement;

        // Preserve clickable class if it was set
        const wasClickable = indicator.classList.contains('clickable');

        // Remove all state classes
        indicator.className = 'status-indicator';
        if (wasClickable) {
            indicator.classList.add('clickable');
        }

        // Add new state class
        indicator.classList.add(`${id}-${state}`);

        // Update label if provided
        if (label && text) {
            text.textContent = label;
        }

        // Update title/tooltip for clickable indicators
        if (id === 'pulse' && indicator.hasAttribute('role') && indicator.getAttribute('role') === 'button') {
            switch (state) {
                case 'active':
                    indicator.title = 'Click to stop Pulse daemon';
                    break;
                case 'inactive':
                    indicator.title = 'Click to start Pulse daemon';
                    break;
                case 'starting':
                    indicator.title = 'Starting Pulse daemon...';
                    break;
                case 'stopping':
                    indicator.title = 'Stopping Pulse daemon...';
                    break;
            }
        }
    }

    /**
     * Toggle Pulse daemon
     */
    private async togglePulseDaemon(): Promise<void> {
        const indicator = this.indicators.get('pulse');
        if (!indicator) return;

        // Get current state from classes
        const isActive = indicator.classList.contains('pulse-active');
        const isTransitioning = indicator.classList.contains('pulse-starting') ||
                                indicator.classList.contains('pulse-stopping');

        if (isTransitioning) {
            toast.info('Please wait for current operation to complete');
            return;
        }

        const action = isActive ? 'stop' : 'start';

        // Update UI to show transitioning state
        this.updateIndicator(
            'pulse',
            action === 'start' ? 'starting' : 'stopping',
            `Pulse: ${action === 'start' ? 'Starting...' : 'Stopping...'}`
        );

        // Send command to backend
        sendMessage({
            type: 'pulse_daemon_control',
            action: action
        });

        toast.info(`${action === 'start' ? 'Starting' : 'Stopping'} Pulse daemon...`);
    }

    /**
     * Show database information modal
     */
    private async showDatabaseInfo(): Promise<void> {
        const module = await import('./database-stats-window.js');
        module.databaseStatsWindow.toggle();
    }

    /**
     * Handle connection status updates
     */
    handleConnectionStatus(connected: boolean): void {
        this.updateIndicator(
            'connection',
            connected ? 'connected' : 'disconnected',
            connected ? 'Connected' : 'Disconnected'
        );

        // Also update body class for global styling
        if (connected) {
            document.body.classList.remove('disconnected', 'connecting');
        } else {
            document.body.classList.add('disconnected');
        }
    }

    /**
     * Handle Pulse daemon status updates
     */
    handlePulseDaemonStatus(data: DaemonStatusMessage): void {
        // Determine state from daemon status
        const state: 'active' | 'inactive' = data.running ? 'active' : 'inactive';

        this.updateIndicator(
            'pulse',
            state,
            `Pulse: ${state === 'active' ? 'ON' : 'OFF'}`
        );
    }

    /**
     * Handle database stats update
     */
    handleDatabaseStats(count: number): void {
        const formatted = count >= 1000 ? `${(count / 1000).toFixed(1)}k` : count.toString();
        this.updateIndicator('database', 'active', `${DB} ${formatted}`);
    }
}

// Export singleton instance
export const statusIndicators = new StatusIndicatorManager();