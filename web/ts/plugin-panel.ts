/**
 * Plugin Panel - Shows installed domain plugins and their status
 *
 * Displays plugin information when triggered from the symbol palette:
 * - Lists all installed plugins with metadata
 * - Shows health status for each plugin
 * - Color-coded status indicators
 *
 * Uses /api/plugins endpoint from server/handlers.go
 */

import { BasePanel } from './base-panel.ts';
import { apiFetch } from './api.ts';
import { toast } from './toast';

interface PluginInfo {
    name: string;
    version: string;
    qntx_version?: string;
    description: string;
    author?: string;
    license?: string;
    healthy: boolean;
    message?: string;
    details?: Record<string, unknown>;
    state: 'running' | 'paused' | 'stopped';
    pausable: boolean;
}

interface PluginsResponse {
    plugins: PluginInfo[];
}

class PluginPanel extends BasePanel {
    private plugins: PluginInfo[] = [];

    constructor() {
        super({
            id: 'plugin-panel',
            classes: ['panel-slide-left', 'plugin-panel'],
            useOverlay: true,
            closeOnEscape: true
        });
    }

    protected getTemplate(): string {
        return `
            <div class="panel-header plugin-panel-header">
                <h3 class="panel-title plugin-panel-title">Domain Plugins</h3>
                <button class="panel-close plugin-panel-close" aria-label="Close">&#10005;</button>
            </div>

            <div class="plugin-panel-search">
                <input type="text" placeholder="Filter plugins..." class="plugin-search-input">
            </div>
            <div class="panel-content plugin-panel-content" id="plugin-panel-content">
                <div class="panel-loading plugin-loading">
                    <p>Loading plugins...</p>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Search input
        const searchInput = this.$<HTMLInputElement>('.plugin-search-input');
        searchInput?.addEventListener('input', (e: Event) => {
            const target = e.target as HTMLInputElement;
            this.filterPlugins(target.value);
        });

        // Button click handlers (event delegation)
        const content = this.$('.plugin-panel-content');
        content?.addEventListener('click', async (e: Event) => {
            const target = e.target as HTMLElement;

            // Refresh button
            if (target.closest('.plugin-refresh-btn')) {
                await this.fetchPlugins();
                this.render();
                return;
            }

            // Pause button
            const pauseBtn = target.closest('.plugin-pause-btn') as HTMLElement | null;
            if (pauseBtn) {
                const pluginName = pauseBtn.dataset.plugin;
                if (pluginName) {
                    await this.pausePlugin(pluginName);
                }
                return;
            }

            // Resume button
            const resumeBtn = target.closest('.plugin-resume-btn') as HTMLElement | null;
            if (resumeBtn) {
                const pluginName = resumeBtn.dataset.plugin;
                if (pluginName) {
                    await this.resumePlugin(pluginName);
                }
                return;
            }
        });
    }

    protected async onShow(): Promise<void> {
        await this.fetchPlugins();
        this.render();

        // Focus search input
        const searchInput = this.$<HTMLInputElement>('.plugin-search-input');
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }
    }

    private async fetchPlugins(): Promise<void> {
        try {
            console.log('[Plugin Panel] Fetching plugins from /api/plugins...');
            const response = await apiFetch('/api/plugins');

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP ${response.status}: ${response.statusText}\n${errorText}`);
            }

            const data: PluginsResponse = await response.json();

            if (!data || !Array.isArray(data.plugins)) {
                throw new Error('Invalid plugins response: missing plugins array');
            }

            this.plugins = data.plugins;
            console.log('[Plugin Panel] Successfully loaded', this.plugins.length, 'plugins');
        } catch (error) {
            console.error('[Plugin Panel] Failed to fetch plugins:', error);
            this.plugins = [];
        }
    }

    private render(): void {
        const content = this.$('#plugin-panel-content');
        if (!content) return;

        if (this.plugins.length === 0) {
            content.innerHTML = `
                <div class="panel-empty plugin-empty">
                    <div class="panel-empty-icon">&#128268;</div>
                    <p>No plugins installed</p>
                    <p class="panel-text-muted">Domain plugins extend QNTX with specialized functionality</p>
                </div>
            `;
            return;
        }

        content.innerHTML = `
            <div class="plugin-summary panel-card">
                <div class="plugin-summary-stats">
                    <span class="plugin-count">${this.plugins.length} plugin${this.plugins.length !== 1 ? 's' : ''} installed</span>
                    <span class="plugin-health-summary">${this.getHealthSummary()}</span>
                </div>
                <button class="panel-btn panel-btn-sm plugin-refresh-btn" title="Refresh">&#8635; Refresh</button>
            </div>
            <div class="panel-list plugin-list">
                ${this.plugins.map(plugin => this.renderPlugin(plugin)).join('')}
            </div>
        `;
    }

    private getHealthSummary(): string {
        const healthy = this.plugins.filter(p => p.healthy).length;
        const unhealthy = this.plugins.length - healthy;

        if (unhealthy === 0) {
            return '<span class="plugin-health-good">All healthy</span>';
        }
        return `<span class="plugin-health-warning">${unhealthy} unhealthy</span>`;
    }

    private renderPlugin(plugin: PluginInfo): string {
        const statusClass = plugin.healthy ? 'plugin-status-healthy' : 'plugin-status-unhealthy';
        const statusIcon = plugin.healthy ? '&#10003;' : '&#10007;';
        const statusText = plugin.healthy ? 'Healthy' : 'Unhealthy';

        // State badge
        const stateClass = this.getStateClass(plugin.state);
        const stateIcon = this.getStateIcon(plugin.state);

        // Control buttons (only for pausable plugins)
        let controls = '';
        if (plugin.pausable) {
            if (plugin.state === 'running') {
                controls = `<button class="panel-btn panel-btn-sm plugin-pause-btn" data-plugin="${plugin.name}" title="Pause plugin">&#10074;&#10074; Pause</button>`;
            } else if (plugin.state === 'paused') {
                controls = `<button class="panel-btn panel-btn-sm plugin-resume-btn" data-plugin="${plugin.name}" title="Resume plugin">&#9654; Resume</button>`;
            }
        }

        return `
            <div class="panel-card plugin-card" data-plugin="${plugin.name}">
                <div class="plugin-card-header">
                    <div class="plugin-name-row">
                        <span class="plugin-name">${this.escapeHtml(plugin.name)}</span>
                        <span class="plugin-version panel-code">${this.escapeHtml(plugin.version)}</span>
                    </div>
                    <div class="plugin-badges">
                        <div class="plugin-state ${stateClass}">
                            <span class="plugin-state-icon">${stateIcon}</span>
                            <span class="plugin-state-text">${plugin.state}</span>
                        </div>
                        <div class="plugin-status ${statusClass}">
                            <span class="plugin-status-icon">${statusIcon}</span>
                            <span class="plugin-status-text">${statusText}</span>
                        </div>
                    </div>
                </div>
                <div class="plugin-description">
                    ${this.escapeHtml(plugin.description || 'No description available')}
                </div>
                <div class="plugin-meta">
                    ${plugin.author ? `<span class="plugin-author" title="Author">&#128100; ${this.escapeHtml(plugin.author)}</span>` : ''}
                    ${plugin.license ? `<span class="plugin-license" title="License">&#128196; ${this.escapeHtml(plugin.license)}</span>` : ''}
                    ${plugin.qntx_version ? `<span class="plugin-qntx-version" title="QNTX Version Requirement">&#8805; ${this.escapeHtml(plugin.qntx_version)}</span>` : ''}
                </div>
                ${controls ? `<div class="plugin-controls">${controls}</div>` : ''}
                ${plugin.message ? `<div class="plugin-message ${plugin.healthy ? '' : 'plugin-message-error'}">${this.escapeHtml(plugin.message)}</div>` : ''}
                ${this.renderDetails(plugin.details)}
            </div>
        `;
    }

    private getStateClass(state: string): string {
        switch (state) {
            case 'running': return 'plugin-state-running';
            case 'paused': return 'plugin-state-paused';
            case 'stopped': return 'plugin-state-stopped';
            default: return '';
        }
    }

    private getStateIcon(state: string): string {
        switch (state) {
            case 'running': return '&#9654;'; // Play icon
            case 'paused': return '&#10074;&#10074;'; // Pause icon
            case 'stopped': return '&#9632;'; // Stop icon
            default: return '';
        }
    }

    private async pausePlugin(name: string): Promise<void> {
        try {
            console.log('[Plugin Panel] Pausing plugin:', name);
            const response = await apiFetch(`/api/plugins/${name}/pause`, {
                method: 'POST'
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Failed to pause: ${errorText}`);
            }

            // Refresh the list to show updated state
            await this.fetchPlugins();
            this.render();
            console.log('[Plugin Panel] Plugin paused:', name);
        } catch (error) {
            console.error('[Plugin Panel] Failed to pause plugin:', error);
            toast.error(`Failed to pause plugin: ${error}`);
        }
    }

    private async resumePlugin(name: string): Promise<void> {
        try {
            console.log('[Plugin Panel] Resuming plugin:', name);
            const response = await apiFetch(`/api/plugins/${name}/resume`, {
                method: 'POST'
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Failed to resume: ${errorText}`);
            }

            // Refresh the list to show updated state
            await this.fetchPlugins();
            this.render();
            console.log('[Plugin Panel] Plugin resumed:', name);
        } catch (error) {
            console.error('[Plugin Panel] Failed to resume plugin:', error);
            toast.error(`Failed to resume plugin: ${error}`);
        }
    }

    private renderDetails(details?: Record<string, unknown>): string {
        if (!details || Object.keys(details).length === 0) {
            return '';
        }

        const items = Object.entries(details).map(([key, value]) => {
            const displayValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
            return `<div class="plugin-detail-item">
                <span class="plugin-detail-key">${this.escapeHtml(key)}:</span>
                <span class="plugin-detail-value">${this.escapeHtml(displayValue)}</span>
            </div>`;
        }).join('');

        return `<div class="plugin-details">${items}</div>`;
    }

    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    private filterPlugins(searchText: string): void {
        const cards = this.$$('.plugin-card');
        const search = searchText.toLowerCase();

        cards.forEach(card => {
            const htmlCard = card as HTMLElement;
            const name = card.querySelector('.plugin-name')?.textContent || '';
            const desc = card.querySelector('.plugin-description')?.textContent || '';
            const matches = name.toLowerCase().includes(search) || desc.toLowerCase().includes(search);
            if (matches) {
                htmlCard.classList.remove('u-hidden');
                htmlCard.classList.add('u-block');
            } else {
                htmlCard.classList.remove('u-block');
                htmlCard.classList.add('u-hidden');
            }
        });
    }
}

// Initialize and export
const pluginPanel = new PluginPanel();

export function showPluginPanel(): void {
    pluginPanel.show();
}

export function hidePluginPanel(): void {
    pluginPanel.hide();
}

export function togglePluginPanel(): void {
    pluginPanel.toggle();
}

export {};
