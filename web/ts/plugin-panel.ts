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
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';
import { buttonPlaceholder, hydrateButtons, registerButton, type HydrateConfig } from './components/button';

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

interface ErrorResponse {
    error: string;
    details: string;
}

interface ConfigFormState {
    pluginName: string;
    currentConfig: Record<string, string>;
    newConfig: Record<string, string>;
    schema: Record<string, ConfigFieldSchema>;
    validationErrors: Record<string, string>;
    needsConfirmation: boolean;
    editingFields: Set<string>;
    error?: { message: string; details: string; status: number };
}

interface ServerHealth {
    status: string;
    version: string;
    commit: string;
    build_time: string;
    clients: number;
    verbosity: number;
    owner: string;
}

export class PluginPanel extends BasePanel {
    private plugins: PluginInfo[] = [];
    private expandedPlugin: string | null = null;
    private configState: ConfigFormState | null = null;
    private serverHealth: ServerHealth | null = null;

    constructor() {
        super({
            id: 'plugin-panel',
            classes: ['panel-slide-left', 'plugin-panel'],
            useOverlay: true,
            closeOnEscape: true
            // Uses shared tooltip system via 'has-tooltip' class (enabled by default)
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

            // Note: Pause/Resume buttons are now hydrated Button components
            // that handle their own click events

            // Save config button
            if (target.closest('.plugin-config-save-btn')) {
                e.stopPropagation();
                await this.savePluginConfig();
                return;
            }

            // Cancel config button (close entire config form)
            if (target.closest('.plugin-config-cancel-btn')) {
                e.stopPropagation();
                this.expandedPlugin = null;
                this.configState = null;
                this.render();
                return;
            }

            // Click on value display to edit (replaces pencil button)
            const valueDisplay = target.closest('.plugin-config-value-display') as HTMLElement | null;
            if (valueDisplay) {
                e.stopPropagation();
                const fieldName = valueDisplay.dataset.field;
                if (fieldName && this.configState) {
                    this.configState.editingFields.add(fieldName);
                    this.render();
                    // Focus the input after render
                    setTimeout(() => {
                        const input = this.$<HTMLInputElement>(`.plugin-config-value-new[data-field="${fieldName}"]`);
                        input?.focus();
                    }, 0);
                }
                return;
            }

            // Cancel field edit button
            const cancelFieldBtn = target.closest('.plugin-config-field-cancel') as HTMLElement | null;
            if (cancelFieldBtn) {
                e.stopPropagation();
                const fieldName = cancelFieldBtn.dataset.field;
                if (fieldName && this.configState) {
                    // Revert to current value
                    const currentValue = this.configState.currentConfig[fieldName] || this.configState.schema[fieldName]?.default_value || '';
                    this.configState.newConfig[fieldName] = currentValue;
                    this.configState.editingFields.delete(fieldName);
                    delete this.configState.validationErrors[fieldName];
                    this.render();
                }
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
            if (target.classList.contains('plugin-config-value-new')) {
                const fieldName = target.dataset.field;
                if (fieldName && this.configState) {
                    this.configState.newConfig[fieldName] = target.value;
                    // Reset confirmation if user changes value after clicking save
                    this.configState.needsConfirmation = false;
                    this.validateField(fieldName, target.value);
                    this.updateSaveButtonState();
                }
            }
        });

        // Note: Tooltips are now handled by BasePanel's shared tooltip system
        // Elements with 'has-tooltip' class and data-tooltip attribute will show tooltips
    }

    protected async onShow(): Promise<void> {
        this.showLoading('Loading plugins...');
        await Promise.all([
            this.fetchPlugins(),
            this.fetchServerHealth()
        ]);
        this.hideLoading();
        this.render();

        // Focus search input
        const searchInput = this.$<HTMLInputElement>('.plugin-search-input');
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }
    }

    private async fetchServerHealth(): Promise<void> {
        try {
            const response = await apiFetch('/health');
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            this.serverHealth = await response.json();
        } catch (error) {
            handleError(error, 'Failed to fetch server health', { context: SEG.UI, silent: true });
            this.serverHealth = null;
        }
    }

    private async fetchPlugins(): Promise<void> {
        try {
            log.debug(SEG.UI, 'Fetching plugins from /api/plugins...');
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
            log.debug(SEG.UI, 'Successfully loaded', this.plugins.length, 'plugins');
        } catch (error) {
            handleError(error, 'Failed to fetch plugins', { context: SEG.UI, silent: true });
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

        const serverBuildTime = this.formatBuildTime(this.serverHealth?.build_time);

        content.innerHTML = `
            <div class="plugin-summary panel-card">
                <div class="plugin-summary-stats">
                    <span class="plugin-count">${this.plugins.length} plugin${this.plugins.length !== 1 ? 's' : ''} installed</span>
                    <span class="plugin-health-summary">${this.getHealthSummary()}</span>
                </div>
                ${serverBuildTime ? `
                    <div class="plugin-server-info">
                        <span class="plugin-server-label">QNTX Server Built:</span>
                        <span class="plugin-server-value panel-code">${serverBuildTime}</span>
                    </div>
                ` : ''}
                <button class="panel-btn panel-btn-sm plugin-refresh-btn has-tooltip" data-tooltip="Refresh">&#8635; Refresh</button>
            </div>
            <div class="panel-list plugin-list">
                ${this.plugins.map(plugin => this.renderPlugin(plugin)).join('')}
            </div>
        `;

        // Hydrate plugin control buttons
        this.hydratePluginButtons(content);
    }

    /**
     * Hydrate plugin pause/resume button placeholders with Button instances
     */
    private hydratePluginButtons(container: Element): void {
        const config: HydrateConfig = {};

        for (const plugin of this.plugins) {
            if (!plugin.pausable) continue;

            if (plugin.state === 'running') {
                config[`plugin-pause-${plugin.name}`] = {
                    label: '❚❚ Pause',
                    onClick: async () => {
                        await this.pausePlugin(plugin.name);
                    },
                    variant: 'secondary',
                    size: 'small'
                };
            } else if (plugin.state === 'paused') {
                config[`plugin-resume-${plugin.name}`] = {
                    label: '▶ Resume',
                    onClick: async () => {
                        await this.resumePlugin(plugin.name);
                    },
                    variant: 'primary',
                    size: 'small'
                };
            }
        }

        const buttons = hydrateButtons(container as HTMLElement, config);

        // Register for WebSocket updates
        for (const [buttonId, button] of Object.entries(buttons)) {
            registerButton(buttonId, button);
        }
    }

    private getHealthSummary(): string {
        const healthy = this.plugins.filter(p => p.healthy).length;
        const unhealthy = this.plugins.length - healthy;

        if (unhealthy === 0) {
            return '<span class="plugin-health-good">All healthy</span>';
        }
        return `<span class="plugin-health-warning">${unhealthy} unhealthy</span>`;
    }

    private formatBuildTime(buildTime?: string): string | null {
        if (!buildTime || buildTime === 'dev' || buildTime === 'unknown') {
            return null;
        }

        // Parse RFC3339 timestamp
        const date = new Date(buildTime);
        if (isNaN(date.getTime())) {
            return null;
        }

        const now = new Date();
        const diffMs = now.getTime() - date.getTime();
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMins / 60);
        const diffDays = Math.floor(diffHours / 24);

        let relativeTime: string;
        if (diffMins < 1) {
            relativeTime = 'just now';
        } else if (diffMins < 60) {
            relativeTime = `${diffMins}m ago`;
        } else if (diffHours < 24) {
            relativeTime = `${diffHours}h ago`;
        } else {
            relativeTime = `${diffDays}d ago`;
        }

        const formattedDate = date.toLocaleString();
        return `${relativeTime} (${formattedDate})`;
    }

    private buildVersionTooltip(plugin: PluginInfo): string {
        const parts: string[] = [];

        // Add metadata
        if (plugin.author) parts.push(`Author: ${plugin.author}`);
        if (plugin.license) parts.push(`License: ${plugin.license}`);
        if (plugin.qntx_version) parts.push(`QNTX Version: ≥${plugin.qntx_version}`);

        // Add separator if we have both metadata and details
        if (parts.length > 0 && plugin.details && Object.keys(plugin.details).length > 0) {
            parts.push('---');
        }

        // Add details
        if (plugin.details) {
            Object.entries(plugin.details).forEach(([key, value]) => {
                let displayValue: string;

                // Format binary_built timestamps specially
                if (key === 'binary_built' && typeof value === 'string') {
                    const timestamp = parseInt(value, 10);
                    if (!isNaN(timestamp)) {
                        const date = new Date(timestamp * 1000);
                        displayValue = date.toLocaleString();
                    } else {
                        displayValue = String(value);
                    }
                } else {
                    displayValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
                }

                parts.push(`${key}: ${displayValue}`);
            });
        }

        return parts.join('\n');
    }

    private renderPlugin(plugin: PluginInfo): string {
        const statusClass = plugin.healthy ? 'plugin-status-healthy' : 'plugin-status-unhealthy';
        const statusIcon = plugin.healthy ? '&#10003;' : '&#10007;';
        const statusText = plugin.healthy ? 'Healthy' : 'Unhealthy';
        const isExpanded = this.expandedPlugin === plugin.name;

        // Build tooltips
        const versionTooltip = this.buildVersionTooltip(plugin);
        const nameTooltip = [
            plugin.description || 'No description available',
            '---',
            `Path: ~/.qntx/plugins/${plugin.name}.toml`
        ].join('\n');

        // State badge
        const stateClass = this.getStateClass(plugin.state);
        const stateIcon = this.getStateIcon(plugin.state);

        // Control buttons (only for pausable plugins)
        let controls = '';
        if (plugin.pausable) {
            if (plugin.state === 'running') {
                controls = buttonPlaceholder(`plugin-pause-${plugin.name}`, '❚❚ Pause', 'panel-btn panel-btn-sm plugin-pause-btn');
            } else if (plugin.state === 'paused') {
                controls = buttonPlaceholder(`plugin-resume-${plugin.name}`, '▶ Resume', 'panel-btn panel-btn-sm plugin-resume-btn');
            }
        }

        return `
            <div class="panel-card plugin-card ${isExpanded ? 'plugin-card-expanded' : ''}" data-plugin="${plugin.name}">
                <div class="plugin-card-header">
                    <div class="plugin-name-row">
                        <span class="plugin-name has-tooltip" data-tooltip="${escapeHtml(nameTooltip)}">${escapeHtml(plugin.name)}</span>
                        <span class="plugin-version has-tooltip panel-code" data-tooltip="${escapeHtml(versionTooltip)}">${escapeHtml(plugin.version)}</span>
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
                ${controls ? `<div class="plugin-controls">${controls}</div>` : ''}
                ${!plugin.healthy && plugin.message ? `<div class="plugin-message plugin-message-error">${escapeHtml(plugin.message)}</div>` : ''}
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
            log.debug(SEG.UI, 'Pausing plugin:', name);
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
            log.debug(SEG.UI, 'Plugin paused:', name);
        } catch (error) {
            handleError(error, 'Failed to pause plugin', { context: SEG.UI });
        }
    }

    private async resumePlugin(name: string): Promise<void> {
        try {
            log.debug(SEG.UI, 'Resuming plugin:', name);
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
            log.debug(SEG.UI, 'Plugin resumed:', name);
        } catch (error) {
            handleError(error, 'Failed to resume plugin', { context: SEG.UI });
        }
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
                // Try to parse rich error response
                try {
                    const errorData: ErrorResponse = await response.json();
                    this.configState = {
                        pluginName,
                        currentConfig: {},
                        newConfig: {},
                        schema: {},
                        validationErrors: {},
                        needsConfirmation: false,
                        editingFields: new Set(),
                        error: { message: errorData.error, details: errorData.details, status: response.status }
                    };
                } catch {
                    // Fallback to text if JSON parsing fails
                    const errorText = await response.text();
                    this.configState = {
                        pluginName,
                        currentConfig: {},
                        newConfig: {},
                        schema: {},
                        validationErrors: {},
                        needsConfirmation: false,
                        editingFields: new Set(),
                        error: { message: errorText || response.statusText, details: '', status: response.status }
                    };
                }
                this.render();
                return;
            }

            const data: PluginConfigResponse = await response.json();

            this.configState = {
                pluginName,
                currentConfig: { ...data.config },
                newConfig: { ...data.config },
                schema: data.schema || {},
                validationErrors: {},
                needsConfirmation: false,
                editingFields: new Set()
            };
        } catch (error) {
            handleError(error, `Failed to fetch config for ${pluginName}`, { context: SEG.UI, silent: true });
            this.configState = {
                pluginName,
                currentConfig: {},
                newConfig: {},
                schema: {},
                validationErrors: {},
                needsConfirmation: false,
                editingFields: new Set(),
                error: { message: `Failed to load configuration: ${error}`, details: '', status: 0 }
            };
            this.render();
        }
    }

    private renderConfigForm(): string {
        if (!this.configState) return '';

        // Show error if configuration fetch failed
        if (this.configState.error) {
            const errorTitle = this.configState.error.status >= 500 ? 'Internal Server Error' : 'Error';

            return `
                <div class="panel-error">
                    <div class="panel-error-title">${errorTitle}</div>
                    <div class="panel-error-message">${escapeHtml(this.configState.error.message)}</div>
                    ${this.configState.error.details ? `
                        <div class="plugin-config-error-details">
                            <div class="panel-error-details-header">Error Details</div>
                            <pre>${escapeHtml(this.configState.error.details)}</pre>
                        </div>
                    ` : ''}
                </div>
            `;
        }

        const fields = Object.entries(this.configState.schema).map(([fieldName, schema]) => {
            const currentValue = this.configState!.currentConfig[fieldName] || schema.default_value;
            const newValue = this.configState!.newConfig[fieldName] || schema.default_value;
            const error = this.configState!.validationErrors[fieldName];
            const hasChanged = currentValue !== newValue;
            const isEditing = this.configState!.editingFields.has(fieldName);

            // Value cell content depends on editing state
            let valueCellContent: string;
            if (isEditing) {
                valueCellContent = `
                    <div class="plugin-config-edit-container">
                        <input type="${this.getInputType(schema.type)}"
                               value="${escapeHtml(newValue)}"
                               data-field="${escapeHtml(fieldName)}"
                               class="plugin-config-value-new panel-code"
                               ${schema.min_value ? `min="${escapeHtml(schema.min_value)}"` : ''}
                               ${schema.max_value ? `max="${escapeHtml(schema.max_value)}"` : ''}
                               ${schema.pattern ? `pattern="${escapeHtml(schema.pattern)}"` : ''}
                               ${schema.required ? 'required' : ''}>
                        <button class="plugin-config-field-cancel has-tooltip" data-field="${escapeHtml(fieldName)}" data-tooltip="Cancel">&#10005;</button>
                    </div>
                `;
            } else {
                valueCellContent = `
                    <span class="plugin-config-value-display ${hasChanged ? 'plugin-config-value-changed' : ''}" data-field="${escapeHtml(fieldName)}">${escapeHtml(newValue)}</span>
                `;
            }

            return `
                <div class="plugin-config-row ${error ? 'plugin-config-row-error' : ''} ${hasChanged ? 'plugin-config-row-changed' : ''}">
                    <label class="plugin-config-label has-tooltip" data-tooltip="${escapeHtml(schema.description)}">
                        ${escapeHtml(fieldName)}${schema.required ? '<span class="plugin-config-required">*</span>' : ''}
                    </label>
                    <div class="plugin-config-value-cell">
                        ${valueCellContent}
                    </div>
                    ${error ? `<div class="plugin-config-row-error-msg">${escapeHtml(error)}</div>` : ''}
                </div>
            `;
        }).join('');

        const hasErrors = Object.keys(this.configState.validationErrors).length > 0;
        const hasChanges = Object.entries(this.configState.newConfig).some(([key, value]) =>
            value !== (this.configState!.currentConfig[key] || this.configState!.schema[key].default_value)
        );
        const isEditing = this.configState.editingFields.size > 0;

        return `
            <div class="plugin-config-form">
                <div class="plugin-config-table">
                    <div class="plugin-config-header">
                        <div class="plugin-config-header-label">Setting</div>
                        <div class="plugin-config-header-value">Value</div>
                    </div>
                    ${fields}
                </div>
                ${(hasChanges || isEditing) ? `
                    <div class="plugin-config-actions">
                        <div class="plugin-config-actions-buttons">
                            <button class="panel-btn plugin-config-cancel-btn">Cancel</button>
                            <button class="panel-btn ${this.configState.needsConfirmation ? 'panel-btn-warning' : 'panel-btn-primary'} plugin-config-save-btn"
                                    ${hasErrors ? 'disabled' : ''}>
                                ${this.configState.needsConfirmation ? 'Confirm Restart' : 'Save Changes'}
                            </button>
                        </div>
                        ${this.configState.needsConfirmation ? `
                            <div class="plugin-config-warning">
                                This will apply your changes and reinitialize the plugin.
                            </div>
                        ` : ''}
                    </div>
                ` : ''}
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

        // First click: show confirmation state
        if (!this.configState.needsConfirmation) {
            this.configState.needsConfirmation = true;
            this.render();
            return;
        }

        // Second click: actually save
        try {
            const requestPayload = { config: this.configState.newConfig };
            log.debug(SEG.UI, 'Saving config for', this.configState.pluginName);
            log.debug(SEG.UI, 'Request payload:', requestPayload);
            log.debug(SEG.UI, 'Payload JSON:', JSON.stringify(requestPayload, null, 2));

            const response = await apiFetch(`/api/plugins/${this.configState.pluginName}/config`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(requestPayload)
            });

            log.debug(SEG.UI, 'Response status:', response.status);

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({ message: response.statusText }));
                log.debug(SEG.UI, 'Error response:', errorData);

                // Format error details - handle backend validation errors
                let errorDetails = errorData.details || '';
                if (errorData.errors && Object.keys(errorData.errors).length > 0) {
                    errorDetails = 'Field-specific validation errors:\n\n';
                    for (const [field, error] of Object.entries(errorData.errors)) {
                        errorDetails += `• ${field}: ${error}\n`;
                    }
                }

                if (!errorDetails) {
                    errorDetails = JSON.stringify(errorData, null, 2);
                }

                // Set error in config state and reset confirmation
                this.configState.error = {
                    message: errorData.message || 'Failed to save configuration',
                    details: errorDetails,
                    status: response.status
                };
                this.configState.needsConfirmation = false;
                this.render();
                return;
            }

            toast.success('Plugin configuration updated successfully');

            // Collapse and refresh
            this.expandedPlugin = null;
            this.configState = null;
            await this.fetchPlugins();
            this.render();
        } catch (error) {
            handleError(error, 'Failed to save config', { context: SEG.UI, silent: true });

            // Set error in config state and reset confirmation
            if (this.configState) {
                this.configState.error = {
                    message: `Failed to save configuration: ${error}`,
                    details: '',
                    status: 0
                };
                this.configState.needsConfirmation = false;
                this.render();
            }
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
