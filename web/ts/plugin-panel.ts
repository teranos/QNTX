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
import { escapeHtml } from './html-utils.ts';

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

interface ConfigFieldSchema {
    type: 'string' | 'number' | 'boolean' | 'array';
    description: string;
    default_value: string;
    required: boolean;
    min_value?: string;
    max_value?: string;
    pattern?: string;
    element_type?: string;
}

interface PluginConfigResponse {
    plugin: string;
    config: Record<string, string>;
    schema: Record<string, ConfigFieldSchema> | null;
}

interface ConfigFormState {
    pluginName: string;
    currentConfig: Record<string, string>;
    newConfig: Record<string, string>;
    schema: Record<string, ConfigFieldSchema>;
    validationErrors: Record<string, string>;
}

class PluginPanel extends BasePanel {
    private plugins: PluginInfo[] = [];
    private expandedPlugin: string | null = null;
    private configState: ConfigFormState | null = null;

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
                e.stopPropagation();
                const pluginName = pauseBtn.dataset.plugin;
                if (pluginName) {
                    await this.pausePlugin(pluginName);
                }
                return;
            }

            // Resume button
            const resumeBtn = target.closest('.plugin-resume-btn') as HTMLElement | null;
            if (resumeBtn) {
                e.stopPropagation();
                const pluginName = resumeBtn.dataset.plugin;
                if (pluginName) {
                    await this.resumePlugin(pluginName);
                }
                return;
            }

            // Save config button
            if (target.closest('.plugin-config-save-btn')) {
                e.stopPropagation();
                await this.savePluginConfig();
                return;
            }

            // Cancel config button
            if (target.closest('.plugin-config-cancel-btn')) {
                e.stopPropagation();
                this.expandedPlugin = null;
                this.configState = null;
                this.render();
                return;
            }

            // Plugin card click - toggle config expansion
            const card = target.closest('.plugin-card') as HTMLElement | null;
            if (card && !target.closest('button') && !target.closest('input')) {
                const pluginName = card.dataset.plugin;
                if (pluginName) {
                    await this.togglePluginConfig(pluginName);
                }
                return;
            }
        });

        // Config input change handlers (event delegation)
        content?.addEventListener('input', (e: Event) => {
            const target = e.target as HTMLInputElement;
            if (target.closest('.plugin-config-input')) {
                const fieldName = target.dataset.field;
                if (fieldName && this.configState) {
                    this.configState.newConfig[fieldName] = target.value;
                    this.validateField(fieldName, target.value);
                    this.updateSaveButtonState();
                }
            }
        });
    }

    protected async onShow(): Promise<void> {
        this.showLoading('Loading plugins...');
        await this.fetchPlugins();
        this.hideLoading();
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
            content.innerHTML = '';
            const emptyState = this.createEmptyState(
                'No plugins installed',
                'Domain plugins extend QNTX with specialized functionality'
            );
            emptyState.classList.add('plugin-empty');
            content.appendChild(emptyState);
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
        const isExpanded = this.expandedPlugin === plugin.name;

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
            <div class="panel-card plugin-card ${isExpanded ? 'plugin-card-expanded' : ''}" data-plugin="${plugin.name}">
                <div class="plugin-card-header">
                    <div class="plugin-name-row">
                        <span class="plugin-name">${escapeHtml(plugin.name)}</span>
                        <span class="plugin-version panel-code">${escapeHtml(plugin.version)}</span>
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
                    ${escapeHtml(plugin.description || 'No description available')}
                </div>
                <div class="plugin-meta">
                    ${plugin.author ? `<span class="plugin-author" title="Author">&#128100; ${escapeHtml(plugin.author)}</span>` : ''}
                    ${plugin.license ? `<span class="plugin-license" title="License">&#128196; ${escapeHtml(plugin.license)}</span>` : ''}
                    ${plugin.qntx_version ? `<span class="plugin-qntx-version" title="QNTX Version Requirement">&#8805; ${escapeHtml(plugin.qntx_version)}</span>` : ''}
                </div>
                <div class="plugin-path panel-code" title="Plugin configuration path">
                    <span class="plugin-path-label">Path:</span> ~/.qntx/plugins/${escapeHtml(plugin.name)}.toml
                </div>
                ${controls ? `<div class="plugin-controls">${controls}</div>` : ''}
                ${plugin.message ? `<div class="plugin-message ${plugin.healthy ? '' : 'plugin-message-error'}">${escapeHtml(plugin.message)}</div>` : ''}
                ${this.renderDetails(plugin.details)}
                ${isExpanded ? this.renderConfigForm() : ''}
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
                <span class="plugin-detail-key">${escapeHtml(key)}:</span>
                <span class="plugin-detail-value">${escapeHtml(displayValue)}</span>
            </div>`;
        }).join('');

        return `<div class="plugin-details">${items}</div>`;
    }

    private async togglePluginConfig(pluginName: string): Promise<void> {
        if (this.expandedPlugin === pluginName) {
            // Collapse
            this.expandedPlugin = null;
            this.configState = null;
        } else {
            // Expand and fetch config
            this.expandedPlugin = pluginName;
            await this.fetchPluginConfig(pluginName);
        }
        this.render();
    }

    private async fetchPluginConfig(pluginName: string): Promise<void> {
        try {
            const response = await apiFetch(`/api/plugins/${pluginName}/config`);
            if (!response.ok) {
                throw new Error(`Failed to fetch config: ${response.statusText}`);
            }

            const data: PluginConfigResponse = await response.json();

            if (!data.schema) {
                toast.error('Plugin does not support configuration');
                this.expandedPlugin = null;
                return;
            }

            this.configState = {
                pluginName,
                currentConfig: { ...data.config },
                newConfig: { ...data.config },
                schema: data.schema,
                validationErrors: {}
            };
        } catch (error) {
            console.error('[Plugin Panel] Failed to fetch config:', error);
            toast.error(`Failed to load configuration: ${error}`);
            this.expandedPlugin = null;
        }
    }

    private renderConfigForm(): string {
        if (!this.configState) return '';

        const fields = Object.entries(this.configState.schema).map(([fieldName, schema]) => {
            const currentValue = this.configState!.currentConfig[fieldName] || schema.default_value;
            const newValue = this.configState!.newConfig[fieldName] || schema.default_value;
            const error = this.configState!.validationErrors[fieldName];
            const hasChanged = currentValue !== newValue;

            return `
                <div class="plugin-config-field ${error ? 'plugin-config-field-error' : ''} ${hasChanged ? 'plugin-config-field-changed' : ''}">
                    <div class="plugin-config-field-header">
                        <label class="plugin-config-label">${escapeHtml(fieldName)}</label>
                        ${schema.required ? '<span class="plugin-config-required">*</span>' : ''}
                    </div>
                    <div class="plugin-config-description">${escapeHtml(schema.description)}</div>
                    <div class="plugin-config-values">
                        <div class="plugin-config-current">
                            <span class="plugin-config-value-label">Current</span>
                            <input type="${this.getInputType(schema.type)}"
                                   value="${escapeHtml(currentValue)}"
                                   disabled
                                   class="plugin-config-input-readonly panel-code">
                        </div>
                        <div class="plugin-config-new">
                            <span class="plugin-config-value-label">New</span>
                            <input type="${this.getInputType(schema.type)}"
                                   value="${escapeHtml(newValue)}"
                                   data-field="${escapeHtml(fieldName)}"
                                   class="plugin-config-input panel-code"
                                   ${schema.min_value ? `min="${escapeHtml(schema.min_value)}"` : ''}
                                   ${schema.max_value ? `max="${escapeHtml(schema.max_value)}"` : ''}
                                   ${schema.pattern ? `pattern="${escapeHtml(schema.pattern)}"` : ''}
                                   ${schema.required ? 'required' : ''}>
                        </div>
                    </div>
                    ${error ? `<div class="plugin-config-error">${escapeHtml(error)}</div>` : ''}
                </div>
            `;
        }).join('');

        const hasErrors = Object.keys(this.configState.validationErrors).length > 0;
        const hasChanges = Object.entries(this.configState.newConfig).some(([key, value]) =>
            value !== (this.configState!.currentConfig[key] || this.configState!.schema[key].default_value)
        );

        return `
            <div class="plugin-config-form">
                <div class="plugin-config-form-header">
                    <h4>Configuration</h4>
                    <span class="plugin-config-hint">Click to collapse</span>
                </div>
                <div class="plugin-config-fields">
                    ${fields}
                </div>
                <div class="plugin-config-actions">
                    <button class="panel-btn plugin-config-cancel-btn">Cancel</button>
                    <button class="panel-btn panel-btn-primary plugin-config-save-btn"
                            ${hasErrors || !hasChanges ? 'disabled' : ''}>
                        Save & Restart Plugin
                    </button>
                </div>
            </div>
        `;
    }

    private getInputType(schemaType: string): string {
        switch (schemaType) {
            case 'number': return 'number';
            case 'boolean': return 'checkbox';
            default: return 'text';
        }
    }

    private validateField(fieldName: string, value: string): void {
        if (!this.configState) return;

        const schema = this.configState.schema[fieldName];
        if (!schema) return;

        // Clear previous error
        delete this.configState.validationErrors[fieldName];

        // Required check
        if (schema.required && !value) {
            this.configState.validationErrors[fieldName] = 'This field is required';
            return;
        }

        // Type-specific validation
        if (schema.type === 'number') {
            const num = parseFloat(value);
            if (isNaN(num)) {
                this.configState.validationErrors[fieldName] = 'Must be a valid number';
                return;
            }
            if (schema.min_value && num < parseFloat(schema.min_value)) {
                this.configState.validationErrors[fieldName] = `Must be at least ${schema.min_value}`;
                return;
            }
            if (schema.max_value && num > parseFloat(schema.max_value)) {
                this.configState.validationErrors[fieldName] = `Must be at most ${schema.max_value}`;
                return;
            }
        }

        // Pattern validation
        if (schema.pattern && value) {
            const regex = new RegExp(schema.pattern);
            if (!regex.test(value)) {
                this.configState.validationErrors[fieldName] = 'Invalid format';
                return;
            }
        }
    }

    private updateSaveButtonState(): void {
        const saveBtn = this.$('.plugin-config-save-btn') as HTMLButtonElement;
        if (!saveBtn || !this.configState) return;

        const hasErrors = Object.keys(this.configState.validationErrors).length > 0;
        const hasChanges = Object.entries(this.configState.newConfig).some(([key, value]) =>
            value !== (this.configState!.currentConfig[key] || this.configState!.schema[key].default_value)
        );

        saveBtn.disabled = hasErrors || !hasChanges;
    }

    private async savePluginConfig(): Promise<void> {
        if (!this.configState) return;

        // Show confirmation dialog
        const confirmed = confirm(
            `Save configuration changes and restart ${this.configState.pluginName} plugin?\n\n` +
            `This will apply your changes and reinitialize the plugin.`
        );

        if (!confirmed) return;

        try {
            const response = await apiFetch(`/api/plugins/${this.configState.pluginName}/config`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: this.configState.newConfig })
            });

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({ message: response.statusText }));
                throw new Error(errorData.message || 'Failed to save configuration');
            }

            toast.success('Plugin configuration updated successfully');

            // Collapse and refresh
            this.expandedPlugin = null;
            this.configState = null;
            await this.fetchPlugins();
            this.render();
        } catch (error) {
            console.error('[Plugin Panel] Failed to save config:', error);
            toast.error(`Failed to save configuration: ${error}`);
        }
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
