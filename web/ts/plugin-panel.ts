/**
 * Plugin Panel - Shows installed domain plugins and their status
 *
 * Manifests as a glyph with 'panel' manifestationType — slides in from
 * the opposite edge of the system drawer.
 *
 * Displays plugin information:
 * - Lists all installed plugins with metadata
 * - Shows health status for each plugin
 * - Color-coded status indicators
 *
 * Uses /api/plugins endpoint from server/handlers.go
 */

import { apiFetch } from './api.ts';
import { toast } from './toast';
import { escapeHtml } from './html-utils.ts';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';
import { buttonPlaceholder, hydrateButtons, registerButton, type HydrateConfig } from './components/button';
import { tooltip } from './components/tooltip.ts';
import type { Glyph } from './components/glyph/glyph';

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

// Module-level state
let plugins: PluginInfo[] = [];
let expandedPlugin: string | null = null;
let configState: ConfigFormState | null = null;
let serverHealth: ServerHealth | null = null;

// The content element provided by renderContent()
let contentElement: HTMLElement | null = null;

// Tooltip cleanup function
let tooltipCleanup: (() => void) | null = null;

async function fetchServerHealth(): Promise<void> {
    try {
        const response = await apiFetch('/health');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        serverHealth = await response.json();
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch server health', { context: SEG.UI, silent: true });
        serverHealth = null;
    }
}

async function fetchPlugins(): Promise<void> {
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

        plugins = data.plugins;
        log.debug(SEG.UI, 'Successfully loaded', plugins.length, 'plugins');
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch plugins', { context: SEG.UI, silent: true });
        plugins = [];
    }
}

function render(): void {
    if (!contentElement) return;

    if (plugins.length === 0) {
        // Server unreachable — both fetches failed
        if (serverHealth === null) {
            contentElement.innerHTML = `
                <div class="glyph-content plugin-offline">
                    <div class="plugin-offline-message">
                        <div class="plugin-offline-title">Server offline</div>
                        <p>gRPC plugins require a running QNTX server</p>
                        <div class="plugin-offline-roadmap">WASM plugins are on the roadmap</div>
                    </div>
                </div>
            `;
            return;
        }

        contentElement.innerHTML = `
            <div class="glyph-content">
                <div class="plugin-search-container" style="padding: 8px 0;">
                    <input type="text" class="plugin-search-input plugin-mono" placeholder="Filter plugins..." style="width: 100%; padding: 6px 8px; background: rgba(0,0,0,0.2); border: 1px solid var(--border-on-dark, #555); border-radius: 4px; color: var(--text-on-dark); font-size: 13px;">
                </div>
                <div class="panel-empty plugin-empty">
                    <p>No plugins installed</p>
                    <p class="panel-empty-hint">Domain plugins extend QNTX with specialized functionality</p>
                </div>
            </div>
        `;
        refreshTooltips();
        return;
    }

    const serverBuildTime = formatBuildTime(serverHealth?.build_time);

    contentElement.innerHTML = `
        <div class="glyph-content">
            <div class="plugin-search-container" style="padding: 8px 0;">
                <input type="text" class="plugin-search-input plugin-mono" placeholder="Filter plugins..." style="width: 100%; padding: 6px 8px; background: rgba(0,0,0,0.2); border: 1px solid var(--border-on-dark, #555); border-radius: 4px; color: var(--text-on-dark); font-size: 13px;">
            </div>
            <div class="plugin-summary">
                <div class="plugin-summary-stats">
                    <span class="plugin-count">${plugins.length} plugin${plugins.length !== 1 ? 's' : ''} installed</span>
                    <span class="plugin-health-summary">${getHealthSummary()}</span>
                </div>
                ${serverBuildTime ? `
                    <div class="plugin-server-info">
                        <span class="plugin-server-label">QNTX Server Built:</span>
                        <span class="plugin-server-value plugin-mono">${serverBuildTime}</span>
                    </div>
                ` : ''}
                <button class="plugin-refresh-btn has-tooltip" data-tooltip="Refresh">&#8635; Refresh</button>
            </div>
            <div class="plugin-list">
                ${plugins.map(plugin => renderPlugin(plugin)).join('')}
            </div>
        </div>
    `;

    // Hydrate plugin control buttons
    hydratePluginButtons(contentElement);

    // Rebind tooltips for new DOM content
    refreshTooltips();
}

function refreshTooltips(): void {
    if (!contentElement) return;
    if (tooltipCleanup) {
        tooltipCleanup();
    }
    tooltipCleanup = tooltip.attach(contentElement, '.has-tooltip');
}

function attachEventDelegation(): void {
    if (!contentElement) return;

    // Click delegation — attached once, works with dynamic content via .closest()
    contentElement.addEventListener('click', async (e: Event) => {
        const target = e.target as HTMLElement;

        // Refresh button
        if (target.closest('.plugin-refresh-btn')) {
            await fetchPlugins();
            render();
            return;
        }

        // Save config button
        if (target.closest('.plugin-config-save-btn')) {
            e.stopPropagation();
            await savePluginConfig();
            return;
        }

        // Cancel config button
        if (target.closest('.plugin-config-cancel-btn')) {
            e.stopPropagation();
            expandedPlugin = null;
            configState = null;
            render();
            return;
        }

        // Click on value display to edit
        const valueDisplay = target.closest('.plugin-config-value-display') as HTMLElement | null;
        if (valueDisplay) {
            e.stopPropagation();
            const fieldName = valueDisplay.dataset.field;
            if (fieldName && configState) {
                configState.editingFields.add(fieldName);
                render();
                setTimeout(() => {
                    const input = contentElement?.querySelector<HTMLInputElement>(`.plugin-config-value-new[data-field="${fieldName}"]`);
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
            if (fieldName && configState) {
                const currentValue = configState.currentConfig[fieldName] || configState.schema[fieldName]?.default_value || '';
                configState.newConfig[fieldName] = currentValue;
                configState.editingFields.delete(fieldName);
                delete configState.validationErrors[fieldName];
                render();
            }
            return;
        }

        // Plugin card click - toggle config expansion
        const card = target.closest('.plugin-card') as HTMLElement | null;
        if (card && !target.closest('button') && !target.closest('input')) {
            const pluginName = card.dataset.plugin;
            if (pluginName) {
                await togglePluginConfig(pluginName);
            }
            return;
        }
    });

    // Config input change handlers
    contentElement.addEventListener('input', (e: Event) => {
        const target = e.target as HTMLInputElement;
        if (target.classList.contains('plugin-config-value-new')) {
            const fieldName = target.dataset.field;
            if (fieldName && configState) {
                configState.newConfig[fieldName] = target.value;
                configState.needsConfirmation = false;
                validateField(fieldName, target.value);
                updateSaveButtonState();
            }
        }

        // Search input filtering
        if (target.classList.contains('plugin-search-input')) {
            filterPlugins(target.value);
        }
    });
}

function hydratePluginButtons(container: HTMLElement): void {
    const config: HydrateConfig = {};

    for (const plugin of plugins) {
        if (!plugin.pausable) continue;

        if (plugin.state === 'running') {
            config[`plugin-pause-${plugin.name}`] = {
                label: '\u275A\u275A Pause',
                onClick: async () => {
                    await pausePlugin(plugin.name);
                },
                variant: 'secondary',
                size: 'small'
            };
        } else if (plugin.state === 'paused') {
            config[`plugin-resume-${plugin.name}`] = {
                label: '\u25B6 Resume',
                onClick: async () => {
                    await resumePlugin(plugin.name);
                },
                variant: 'primary',
                size: 'small'
            };
        }
    }

    const buttons = hydrateButtons(container, config);

    for (const [buttonId, button] of Object.entries(buttons)) {
        registerButton(buttonId, button);
    }
}

function getHealthSummary(): string {
    const healthy = plugins.filter(p => p.healthy).length;
    const unhealthy = plugins.length - healthy;

    if (unhealthy === 0) {
        return '<span class="plugin-health-good">All healthy</span>';
    }
    return `<span class="plugin-health-warning">${unhealthy} unhealthy</span>`;
}

export function formatBuildTime(buildTime?: string): string | null {
    if (!buildTime || buildTime === 'dev' || buildTime === 'unknown') {
        return null;
    }

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

function buildVersionTooltip(plugin: PluginInfo): string {
    const parts: string[] = [];

    if (plugin.author) parts.push(`Author: ${plugin.author}`);
    if (plugin.license) parts.push(`License: ${plugin.license}`);
    if (plugin.qntx_version) parts.push(`QNTX Version: \u2265${plugin.qntx_version}`);

    if (parts.length > 0 && plugin.details && Object.keys(plugin.details).length > 0) {
        parts.push('---');
    }

    if (plugin.details) {
        Object.entries(plugin.details).forEach(([key, value]) => {
            let displayValue: string;

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

function renderPlugin(plugin: PluginInfo): string {
    const statusClass = plugin.healthy ? 'plugin-status-healthy' : 'plugin-status-unhealthy';
    const statusIcon = plugin.healthy ? '&#10003;' : '&#10007;';
    const statusText = plugin.healthy ? 'Healthy' : 'Unhealthy';
    const isExpanded = expandedPlugin === plugin.name;

    const versionTooltip = buildVersionTooltip(plugin);
    const nameTooltip = [
        plugin.description || 'No description available',
        '---',
        `Path: ~/.qntx/plugins/${plugin.name}.toml`
    ].join('\n');

    const stateClass = getStateClass(plugin.state);
    const stateIcon = getStateIcon(plugin.state);

    let controls = '';
    if (plugin.pausable) {
        if (plugin.state === 'running') {
            controls = buttonPlaceholder(`btn:plugin-pause:${plugin.name}`, '\u275A\u275A Pause', 'plugin-pause-btn');
        } else if (plugin.state === 'paused') {
            controls = buttonPlaceholder(`btn:plugin-resume:${plugin.name}`, '\u25B6 Resume', 'plugin-resume-btn');
        }
    }

    return `
        <div class="plugin-card ${isExpanded ? 'plugin-card-expanded' : ''}" data-plugin="${plugin.name}">
            <div class="plugin-card-header">
                <div class="plugin-name-row">
                    <span class="plugin-name has-tooltip" data-tooltip="${escapeHtml(nameTooltip)}">${escapeHtml(plugin.name)}</span>
                    <span class="plugin-version has-tooltip plugin-mono" data-tooltip="${escapeHtml(versionTooltip)}">${escapeHtml(plugin.version)}</span>
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
            ${isExpanded ? renderConfigForm() : ''}
        </div>
    `;
}

function getStateClass(state: string): string {
    switch (state) {
        case 'running': return 'plugin-state-running';
        case 'paused': return 'plugin-state-paused';
        case 'stopped': return 'plugin-state-stopped';
        default: return '';
    }
}

function getStateIcon(state: string): string {
    switch (state) {
        case 'running': return '&#9654;';
        case 'paused': return '&#10074;&#10074;';
        case 'stopped': return '&#9632;';
        default: return '';
    }
}

async function pausePlugin(name: string): Promise<void> {
    try {
        log.debug(SEG.UI, 'Pausing plugin:', name);
        const response = await apiFetch(`/api/plugins/${name}/pause`, {
            method: 'POST'
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Failed to pause: ${errorText}`);
        }

        await fetchPlugins();
        render();
        log.debug(SEG.UI, 'Plugin paused:', name);
    } catch (error: unknown) {
        handleError(error, 'Failed to pause plugin', { context: SEG.UI });
    }
}

async function resumePlugin(name: string): Promise<void> {
    try {
        log.debug(SEG.UI, 'Resuming plugin:', name);
        const response = await apiFetch(`/api/plugins/${name}/resume`, {
            method: 'POST'
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Failed to resume: ${errorText}`);
        }

        await fetchPlugins();
        render();
        log.debug(SEG.UI, 'Plugin resumed:', name);
    } catch (error: unknown) {
        handleError(error, 'Failed to resume plugin', { context: SEG.UI });
    }
}

async function togglePluginConfig(pluginName: string): Promise<void> {
    if (expandedPlugin === pluginName) {
        expandedPlugin = null;
        configState = null;
    } else {
        expandedPlugin = pluginName;
        await fetchPluginConfig(pluginName);
    }
    render();
}

async function fetchPluginConfig(pluginName: string): Promise<void> {
    try {
        const response = await apiFetch(`/api/plugins/${pluginName}/config`);
        if (!response.ok) {
            try {
                const errorData: ErrorResponse = await response.json();
                configState = {
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
                const errorText = await response.text();
                configState = {
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
            render();
            return;
        }

        const data: PluginConfigResponse = await response.json();

        configState = {
            pluginName,
            currentConfig: { ...data.config },
            newConfig: { ...data.config },
            schema: data.schema || {},
            validationErrors: {},
            needsConfirmation: false,
            editingFields: new Set()
        };
    } catch (error: unknown) {
        handleError(error, `Failed to fetch config for ${pluginName}`, { context: SEG.UI, silent: true });
        configState = {
            pluginName,
            currentConfig: {},
            newConfig: {},
            schema: {},
            validationErrors: {},
            needsConfirmation: false,
            editingFields: new Set(),
            error: { message: `Failed to load configuration: ${error}`, details: '', status: 0 }
        };
        render();
    }
}

function renderConfigForm(): string {
    if (!configState) return '';

    if (configState.error) {
        const errorTitle = configState.error.status >= 500 ? 'Internal Server Error' : 'Error';

        return `
            <div class="panel-error">
                <div class="panel-error-title">${errorTitle}</div>
                <div class="panel-error-message">${escapeHtml(configState.error.message)}</div>
                ${configState.error.details ? `
                    <div class="plugin-config-error-details">
                        <div class="panel-error-details-header">Error Details</div>
                        <pre>${escapeHtml(configState.error.details)}</pre>
                    </div>
                ` : ''}
            </div>
        `;
    }

    const fields = Object.entries(configState.schema).map(([fieldName, schema]) => {
        const currentValue = configState!.currentConfig[fieldName] || schema.default_value;
        const newValue = configState!.newConfig[fieldName] || schema.default_value;
        const error = configState!.validationErrors[fieldName];
        const hasChanged = currentValue !== newValue;
        const isEditing = configState!.editingFields.has(fieldName);

        let valueCellContent: string;
        if (isEditing) {
            valueCellContent = `
                <div class="plugin-config-edit-container">
                    <input type="${getInputType(schema.type)}"
                           value="${escapeHtml(newValue)}"
                           data-field="${escapeHtml(fieldName)}"
                           class="plugin-config-value-new plugin-mono"
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

    const hasErrors = Object.keys(configState.validationErrors).length > 0;
    const hasChanges = Object.entries(configState.newConfig).some(([key, value]) =>
        value !== (configState!.currentConfig[key] || configState!.schema[key].default_value)
    );
    const isEditing = configState.editingFields.size > 0;

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
                        <button class="plugin-config-cancel-btn">Cancel</button>
                        <button class="${configState.needsConfirmation ? 'panel-btn-warning' : ''} plugin-config-save-btn"
                                ${hasErrors ? 'disabled' : ''}>
                            ${configState.needsConfirmation ? 'Confirm Restart' : 'Save Changes'}
                        </button>
                    </div>
                    ${configState.needsConfirmation ? `
                        <div class="plugin-config-warning">
                            This will apply your changes and reinitialize the plugin.
                        </div>
                    ` : ''}
                </div>
            ` : ''}
        </div>
    `;
}

function getInputType(schemaType: string): string {
    switch (schemaType) {
        case 'number': return 'number';
        case 'boolean': return 'checkbox';
        default: return 'text';
    }
}

function validateField(fieldName: string, value: string): void {
    if (!configState) return;

    const schema = configState.schema[fieldName];
    if (!schema) return;

    delete configState.validationErrors[fieldName];

    if (schema.required && !value) {
        configState.validationErrors[fieldName] = 'This field is required';
        return;
    }

    if (schema.type === 'number') {
        const num = parseFloat(value);
        if (isNaN(num)) {
            configState.validationErrors[fieldName] = 'Must be a valid number';
            return;
        }
        if (schema.min_value && num < parseFloat(schema.min_value)) {
            configState.validationErrors[fieldName] = `Must be at least ${schema.min_value}`;
            return;
        }
        if (schema.max_value && num > parseFloat(schema.max_value)) {
            configState.validationErrors[fieldName] = `Must be at most ${schema.max_value}`;
            return;
        }
    }

    if (schema.pattern && value) {
        const regex = new RegExp(schema.pattern);
        if (!regex.test(value)) {
            configState.validationErrors[fieldName] = 'Invalid format';
            return;
        }
    }
}

function updateSaveButtonState(): void {
    const saveBtn = contentElement?.querySelector('.plugin-config-save-btn') as HTMLButtonElement | null;
    if (!saveBtn || !configState) return;

    const hasErrors = Object.keys(configState.validationErrors).length > 0;
    const hasChanges = Object.entries(configState.newConfig).some(([key, value]) =>
        value !== (configState!.currentConfig[key] || configState!.schema[key].default_value)
    );

    saveBtn.disabled = hasErrors || !hasChanges;
}

async function savePluginConfig(): Promise<void> {
    if (!configState) return;

    if (!configState.needsConfirmation) {
        configState.needsConfirmation = true;
        render();
        return;
    }

    try {
        const requestPayload = { config: configState.newConfig };
        log.debug(SEG.UI, 'Saving config for', configState.pluginName);
        log.debug(SEG.UI, 'Request payload:', requestPayload);
        log.debug(SEG.UI, 'Payload JSON:', JSON.stringify(requestPayload, null, 2));

        const response = await apiFetch(`/api/plugins/${configState.pluginName}/config`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(requestPayload)
        });

        log.debug(SEG.UI, 'Response status:', response.status);

        if (!response.ok) {
            const errorData = await response.json().catch(() => ({ message: response.statusText }));
            log.debug(SEG.UI, 'Error response:', errorData);

            let errorDetails = errorData.details || '';
            if (errorData.errors && Object.keys(errorData.errors).length > 0) {
                errorDetails = 'Field-specific validation errors:\n\n';
                for (const [field, error] of Object.entries(errorData.errors)) {
                    errorDetails += `\u2022 ${field}: ${error}\n`;
                }
            }

            if (!errorDetails) {
                errorDetails = JSON.stringify(errorData, null, 2);
            }

            configState.error = {
                message: errorData.message || 'Failed to save configuration',
                details: errorDetails,
                status: response.status
            };
            configState.needsConfirmation = false;
            render();
            return;
        }

        toast.success('Plugin configuration updated successfully');

        expandedPlugin = null;
        configState = null;
        await fetchPlugins();
        render();
    } catch (error: unknown) {
        handleError(error, 'Failed to save config', { context: SEG.UI, silent: true });

        if (configState) {
            configState.error = {
                message: `Failed to save configuration: ${error}`,
                details: '',
                status: 0
            };
            configState.needsConfirmation = false;
            render();
        }
    }
}

function filterPlugins(searchText: string): void {
    if (!contentElement) return;
    const cards = contentElement.querySelectorAll('.plugin-card');
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

/**
 * Create a Glyph definition for the plugin panel
 */
export function createPluginGlyph(): Glyph {
    return {
        id: 'plugin-glyph',
        title: '\u2699 Domain Plugins',
        manifestationType: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;

            // Attach delegated listeners once — they survive innerHTML replacements
            attachEventDelegation();

            // Show loading, then fetch data
            content.innerHTML = '<div class="glyph-loading">Loading plugins...</div>';

            Promise.all([
                fetchPlugins(),
                fetchServerHealth()
            ]).then(() => {
                render();
                // Focus search input after render
                setTimeout(() => {
                    const searchInput = contentElement?.querySelector<HTMLInputElement>('.plugin-search-input');
                    searchInput?.focus();
                }, 100);
            });

            return content;
        }
    };
}
