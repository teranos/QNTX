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

        // Refresh button click handler (event delegation)
        const content = this.$('.plugin-panel-content');
        content?.addEventListener('click', async (e: Event) => {
            const target = e.target as HTMLElement;
            if (target.closest('.plugin-refresh-btn')) {
                await this.fetchPlugins();
                this.render();
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

        return `
            <div class="panel-card plugin-card" data-plugin="${plugin.name}">
                <div class="plugin-card-header">
                    <div class="plugin-name-row">
                        <span class="plugin-name">${this.escapeHtml(plugin.name)}</span>
                        <span class="plugin-version panel-code">${this.escapeHtml(plugin.version)}</span>
                    </div>
                    <div class="plugin-status ${statusClass}">
                        <span class="plugin-status-icon">${statusIcon}</span>
                        <span class="plugin-status-text">${statusText}</span>
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
                ${plugin.message ? `<div class="plugin-message ${plugin.healthy ? '' : 'plugin-message-error'}">${this.escapeHtml(plugin.message)}</div>` : ''}
                ${this.renderDetails(plugin.details)}
            </div>
        `;
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
            htmlCard.style.display = matches ? 'block' : 'none';
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
