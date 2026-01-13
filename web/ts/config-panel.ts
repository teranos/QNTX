/**
 * Config Panel - Shows active configuration and sources
 *
 * Displays configuration introspection when clicking ≡ (am) in the symbol palette:
 * - Shows active config file path
 * - Lists all settings with their sources (environment, config_file, default)
 * - Color-coded by source for quick visual identification
 *
 * Uses /api/config?introspection=true endpoint from internal/config/introspection.go
 */

import { BasePanel } from './base-panel.ts';
import { apiFetch } from './api.ts';
import { AM } from '@generated/sym.js';
import { formatValue } from './html-utils.ts';
import { createRichErrorState, type RichError } from './base-panel-error.ts';
import { handleError, SEG } from './error-handler.ts';

interface ConfigSetting {
    key: string;
    value: unknown;
    source: string;
    source_path?: string;
}

interface ConfigResponse {
    config_file?: string;
    settings: ConfigSetting[];
    settingsEnhanced?: EnhancedSetting[];
}

interface SourceValue {
    source: string;
    value: unknown;
    source_path?: string;
}

interface EnhancedSetting extends ConfigSetting {
    isEffective: boolean;
    overriddenBy: string | null;
    allSources: SourceValue[];
}

class ConfigPanel extends BasePanel {
    private appConfig: ConfigResponse | null = null;
    private configError: RichError | null = null;
    private editingKey: string | null = null;
    private saveConfirmPending: boolean = false;
    private saveConfirmTimeout: number | null = null;

    constructor() {
        super({
            id: 'config-panel',
            classes: ['panel-slide-left', 'config-panel'],
            useOverlay: true,
            closeOnEscape: true
        });
    }

    protected getTemplate(): string {
        return `
            <div class="panel-header config-panel-header">
                <h3 class="panel-title config-panel-title">${AM} Configuration</h3>
                <button class="panel-close config-panel-close" aria-label="Close">✕</button>
            </div>

            <div class="config-panel-search">
                <input type="text" placeholder="Filter settings..." class="config-search-input">
            </div>
            <div class="panel-content config-panel-content" id="config-panel-content">
                <div class="panel-loading config-loading">
                    <p>Loading configuration...</p>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Search input
        const searchInput = this.$<HTMLInputElement>('.config-search-input');
        searchInput?.addEventListener('input', (e: Event) => {
            const target = e.target as HTMLInputElement;
            this.filterSettings(target.value);
        });

        // Content click handler (event delegation)
        const content = this.$('.config-panel-content');
        content?.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;

            // Source click - copy path
            const sourceSpan = target.closest('.source-clickable') as HTMLElement | null;
            if (sourceSpan?.dataset.source) {
                const source = sourceSpan.dataset.source;
                const path = sourceSpan.dataset.path || this.getSourcePath(source);
                this.handleSourceClick(source, path);
                return;
            }

            // Edit button click
            const editBtn = target.closest('.config-edit-btn') as HTMLElement | null;
            if (editBtn?.dataset.key) {
                this.startEditing(editBtn.dataset.key);
                return;
            }

            // Save button click (two-click confirmation)
            const saveBtn = target.closest('.config-save-btn') as HTMLElement | null;
            if (saveBtn?.dataset.key) {
                this.handleSaveClick(saveBtn.dataset.key);
                return;
            }

            // Cancel button click
            const cancelBtn = target.closest('.config-cancel-btn') as HTMLElement | null;
            if (cancelBtn) {
                this.cancelEditing();
                return;
            }
        });
    }

    protected async onShow(): Promise<void> {
        this.showLoading('Loading configuration...');
        await this.fetchConfig();
        this.hideLoading();
        this.render();

        // Focus search input
        const searchInput = this.$<HTMLInputElement>('.config-search-input');
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }
    }

    private handleSourceClick(source: string, path: string): void {
        console.log(`[Config Panel] Clicked source: ${source} (${path})`);

        navigator.clipboard.writeText(path).then(() => {
            const toast = document.createElement('div');
            toast.className = 'config-toast';
            toast.textContent = `Copied to clipboard: ${path}`;
            document.body.appendChild(toast);
            setTimeout(() => toast.remove(), 2000);
        }).catch(err => {
            console.error('[Config Panel] Failed to copy path:', err);
            const toast = document.createElement('div');
            toast.className = 'config-toast config-toast-error';
            toast.textContent = `Failed to copy: ${err.message || 'Clipboard access denied'}`;
            document.body.appendChild(toast);
            setTimeout(() => toast.remove(), 3000);
        });
    }

    private async fetchConfig(): Promise<void> {
        try {
            console.log('[Config Panel] Fetching config from /api/config?introspection=true...');
            this.configError = null;
            const response = await apiFetch('/api/config?introspection=true');

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP ${response.status}: ${response.statusText}\n${errorText}`);
            }

            const data = await response.json();

            if (!data || !Array.isArray(data.settings)) {
                throw new Error('Invalid config response: missing settings array');
            }

            this.appConfig = data;
            console.log('[Config Panel] Successfully loaded config with', data.settings.length, 'settings');
        } catch (error) {
            handleError(error, 'Failed to fetch config', { context: SEG.ERROR, silent: true });

            // Build rich error for display
            this.configError = this.buildConfigError(error);
            this.appConfig = null;
        }
    }

    /**
     * Build rich error from config fetch error
     */
    private buildConfigError(error: unknown): RichError {
        const errorMessage = error instanceof Error ? error.message : String(error);
        const errorStack = error instanceof Error ? error.stack : undefined;

        // Check for HTTP errors
        const httpMatch = errorMessage.match(/HTTP\s*(\d{3})/i);
        if (httpMatch) {
            const status = parseInt(httpMatch[1], 10);
            if (status === 404) {
                return {
                    title: 'Config Endpoint Not Found',
                    message: 'The configuration API endpoint is not available',
                    status: 404,
                    suggestion: 'Ensure the QNTX server is running with the config endpoint enabled.',
                    details: errorStack || errorMessage
                };
            }
            if (status >= 500) {
                return {
                    title: 'Server Error',
                    message: 'The server encountered an error loading configuration',
                    status: status,
                    suggestion: 'Check the server logs for more details.',
                    details: errorStack || errorMessage
                };
            }
        }

        // Check for network errors
        if (errorMessage.includes('NetworkError') || errorMessage.includes('Failed to fetch')) {
            return {
                title: 'Network Error',
                message: 'Unable to connect to the QNTX server',
                suggestion: 'Check your network connection and ensure the QNTX server is running.',
                details: errorStack || errorMessage
            };
        }

        // Generic error
        return {
            title: 'Configuration Error',
            message: errorMessage,
            suggestion: 'Check the error details for more information.',
            details: errorStack || errorMessage
        };
    }

    private render(): void {
        const content = this.$('#config-panel-content');
        if (!content) return;

        // Show rich error if there was an error fetching config
        if (this.configError) {
            content.innerHTML = '';
            content.appendChild(createRichErrorState(this.configError, async () => {
                // Retry fetching config
                this.showLoading('Retrying...');
                await this.fetchConfig();
                this.hideLoading();
                this.render();
            }));
            return;
        }

        if (!this.appConfig || this.appConfig.settings.length === 0) {
            content.innerHTML = '';
            content.appendChild(this.createEmptyState('No configuration loaded'));
            return;
        }

        const mergedConfig = this.calculateMergedConfig(this.appConfig.settings);
        this.appConfig.settingsEnhanced = mergedConfig.allSettings;

        content.innerHTML = `
            <div class="panel-card config-file-info">
                <strong>Final Merged Config</strong>
                <span class="config-file-hint">This is what the server sees</span>
            </div>
            <div class="config-settings">
                ${this.renderMergedConfig(mergedConfig.effectiveSettings)}
            </div>
        `;
    }

    private calculateMergedConfig(settings: ConfigSetting[]): { effectiveSettings: EnhancedSetting[], allSettings: EnhancedSetting[] } {
        const precedenceOrder = ['environment', 'project', 'user_ui', 'user', 'system'];
        const settingsByKey: Record<string, ConfigSetting[]> = {};

        settings.forEach(setting => {
            if (!settingsByKey[setting.key]) {
                settingsByKey[setting.key] = [];
            }
            settingsByKey[setting.key].push(setting);
        });

        const effectiveSettings: EnhancedSetting[] = [];
        const allSettings: EnhancedSetting[] = [];

        Object.entries(settingsByKey).forEach(([_key, sources]) => {
            let effectiveSource: ConfigSetting | null = null;
            let effectivePrecedence = Infinity;

            sources.forEach(source => {
                const precedence = precedenceOrder.indexOf(source.source);
                if (precedence >= 0 && precedence < effectivePrecedence) {
                    effectivePrecedence = precedence;
                    effectiveSource = source;
                }
            });

            if (!effectiveSource && sources.length > 0) {
                effectiveSource = sources[0];
            }

            if (!effectiveSource) return;

            sources.forEach(source => {
                const isEffective = source === effectiveSource;
                const enhanced: EnhancedSetting = {
                    ...source,
                    isEffective,
                    overriddenBy: isEffective ? null : effectiveSource!.source,
                    allSources: sources.map(s => ({
                        source: s.source,
                        value: s.value,
                        source_path: s.source_path
                    }))
                };

                allSettings.push(enhanced);
                if (isEffective) {
                    effectiveSettings.push(enhanced);
                }
            });
        });

        effectiveSettings.sort((a, b) => a.key.localeCompare(b.key));
        return { effectiveSettings, allSettings };
    }

    private renderMergedConfig(effectiveSettings: EnhancedSetting[]): string {
        const grouped: Record<string, EnhancedSetting[]> = {};
        effectiveSettings.forEach(setting => {
            const parts = setting.key.split('.');
            const group = parts.length > 1 ? parts[0] : 'general';
            if (!grouped[group]) {
                grouped[group] = [];
            }
            grouped[group].push(setting);
        });

        return Object.entries(grouped).map(([group, settings]) => `
            <div class="config-group">
                <h4 class="config-group-title">${group}</h4>
                ${settings.map(setting => this.renderEffectiveSetting(setting)).join('')}
            </div>
        `).join('');
    }

    private renderEffectiveSetting(setting: EnhancedSetting): string {
        const valueDisplay = formatValue(setting.value, true); // Always mask secrets in config panel
        const allPossibleSources = ['environment', 'project', 'user_ui', 'user', 'system', 'default'];
        const definedSources = new Set(setting.allSources.map(s => s.source));

        const sourcesDisplay = allPossibleSources
            .map(source => {
                const label = this.getSourceLabel(source);
                const isActive = source === setting.source;
                const isDefined = definedSources.has(source);
                const sourceData = setting.allSources.find(s => s.source === source);
                const path = sourceData?.source_path || this.getSourcePath(source);

                // Build rich tooltip content
                const tooltip = this.buildSourceTooltip(source, path, isActive, isDefined, sourceData?.value);

                if (isActive) {
                    return `<span class="source-active source-clickable has-tooltip" data-source="${source}" data-path="${path}" data-tooltip="${this.escapeAttr(tooltip)}">${label}</span>`;
                } else if (isDefined) {
                    return `<span class="source-inactive source-clickable has-tooltip" data-source="${source}" data-path="${path}" data-tooltip="${this.escapeAttr(tooltip)}">${label}</span>`;
                } else {
                    return `<span class="source-undefined has-tooltip" data-tooltip="${this.escapeAttr(tooltip)}">${label}</span>`;
                }
            })
            .join(' ');

        const isEditable = setting.source === 'user_ui';
        const editControl = isEditable ? `<button class="config-edit-btn has-tooltip" data-key="${setting.key}" data-tooltip="Edit">✎</button>` : '';

        // Build setting key tooltip with all source values
        const keyTooltip = this.buildSettingTooltip(setting);

        return `
            <div class="config-setting" data-key="${setting.key}">
                <div class="config-setting-key has-tooltip" data-tooltip="${this.escapeAttr(keyTooltip)}">${setting.key}</div>
                <div class="config-setting-value">${valueDisplay}</div>
                <div class="config-setting-sources">${sourcesDisplay}</div>
                ${editControl}
            </div>
        `;
    }

    /**
     * Build tooltip for a config source badge
     */
    private buildSourceTooltip(source: string, path: string, isActive: boolean, isDefined: boolean, value?: unknown): string {
        const parts: string[] = [];

        parts.push(`Source: ${source.toUpperCase()}`);
        parts.push(`Path: ${path}`);
        parts.push(`---`);

        if (isActive) {
            parts.push(`Status: ACTIVE (this value is used)`);
        } else if (isDefined) {
            parts.push(`Status: OVERRIDDEN`);
            if (value !== undefined) {
                parts.push(`Value: ${JSON.stringify(value)}`);
            }
        } else {
            parts.push(`Status: Not defined at this level`);
        }

        if (isDefined) {
            parts.push(`---`);
            parts.push(`Click to copy path`);
        }

        return parts.join('\n');
    }

    /**
     * Build tooltip for a setting key showing all source values
     */
    private buildSettingTooltip(setting: EnhancedSetting): string {
        const parts: string[] = [];

        parts.push(`Setting: ${setting.key}`);
        parts.push(`Effective Value: ${JSON.stringify(setting.value)}`);
        parts.push(`Source: ${setting.source}`);

        if (setting.allSources.length > 1) {
            parts.push(`---`);
            parts.push(`Defined in ${setting.allSources.length} sources:`);
            setting.allSources.forEach(s => {
                const marker = s.source === setting.source ? '→' : ' ';
                parts.push(`${marker} ${s.source}: ${JSON.stringify(s.value)}`);
            });
        }

        return parts.join('\n');
    }

    /**
     * Escape string for use in HTML attribute
     */
    private escapeAttr(str: string): string {
        return str
            .replace(/&/g, '&amp;')
            .replace(/"/g, '&quot;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    private getSourceLabel(source: string): string {
        const labels: Record<string, string> = {
            'environment': 'ENV',
            'project': 'PROJECT',
            'user_ui': 'USER_UI',
            'user': 'USER',
            'system': 'SYSTEM',
            'default': 'DEFAULT',
            'unknown': '?'
        };
        return labels[source] || source.toUpperCase();
    }

    private getSourcePath(source: string): string {
        const paths: Record<string, string> = {
            'system': '/etc/qntx/config.toml',
            'user': '~/.qntx/config.toml',
            'user_ui': '~/.qntx/config_from_ui.toml',
            'project': 'config.toml (project root)',
            'environment': 'Environment variable (QNTX_*)',
            'default': 'Built-in default value'
        };
        return paths[source] || 'Unknown source';
    }

    /**
     * Start editing a config setting
     */
    private startEditing(key: string): void {
        // Cancel any existing edit
        if (this.editingKey) {
            this.cancelEditing();
        }

        this.editingKey = key;
        this.saveConfirmPending = false;

        // Find the setting
        const setting = this.appConfig?.settingsEnhanced?.find(s => s.key === key && s.isEffective);
        if (!setting) return;

        // Find the setting row and replace value with input
        const settingRow = this.$<HTMLElement>(`.config-setting[data-key="${key}"]`);
        if (!settingRow) return;

        const valueEl = settingRow.querySelector('.config-setting-value');
        if (!valueEl) return;

        // Create appropriate input based on value type
        const currentValue = setting.value;
        let inputHtml: string;

        if (typeof currentValue === 'boolean') {
            inputHtml = `
                <select class="config-edit-input" data-type="boolean">
                    <option value="true" ${currentValue ? 'selected' : ''}>true</option>
                    <option value="false" ${!currentValue ? 'selected' : ''}>false</option>
                </select>
            `;
        } else if (typeof currentValue === 'number') {
            inputHtml = `<input type="number" class="config-edit-input" data-type="number" value="${currentValue}" step="any">`;
        } else {
            const strValue = typeof currentValue === 'string' ? currentValue : JSON.stringify(currentValue);
            inputHtml = `<input type="text" class="config-edit-input" data-type="string" value="${this.escapeAttr(strValue)}">`;
        }

        valueEl.innerHTML = `
            <div class="config-edit-container">
                ${inputHtml}
                <div class="config-edit-actions">
                    <button class="config-save-btn" data-key="${key}">Save</button>
                    <button class="config-cancel-btn">Cancel</button>
                </div>
            </div>
        `;

        // Focus the input
        const input = valueEl.querySelector<HTMLInputElement | HTMLSelectElement>('.config-edit-input');
        input?.focus();

        // Hide the edit button
        const editBtn = settingRow.querySelector('.config-edit-btn') as HTMLElement;
        if (editBtn) editBtn.style.display = 'none';
    }

    /**
     * Cancel editing without saving
     */
    private cancelEditing(): void {
        if (!this.editingKey) return;

        // Clear confirmation state
        if (this.saveConfirmTimeout) {
            clearTimeout(this.saveConfirmTimeout);
            this.saveConfirmTimeout = null;
        }
        this.saveConfirmPending = false;

        this.editingKey = null;
        this.render();
    }

    /**
     * Handle save button click with two-click confirmation
     */
    private handleSaveClick(key: string): void {
        if (!this.saveConfirmPending) {
            // First click - enter confirmation state
            this.saveConfirmPending = true;

            const saveBtn = this.$<HTMLElement>(`.config-save-btn[data-key="${key}"]`);
            if (saveBtn) {
                saveBtn.textContent = 'Confirm';
                saveBtn.classList.add('config-save-confirming');
            }

            // Auto-reset after 5 seconds
            this.saveConfirmTimeout = window.setTimeout(() => {
                this.saveConfirmPending = false;
                if (saveBtn) {
                    saveBtn.textContent = 'Save';
                    saveBtn.classList.remove('config-save-confirming');
                }
            }, 5000);

            return;
        }

        // Second click - actually save
        if (this.saveConfirmTimeout) {
            clearTimeout(this.saveConfirmTimeout);
            this.saveConfirmTimeout = null;
        }
        this.saveConfirmPending = false;

        this.saveConfig(key);
    }

    /**
     * Save the edited config value
     */
    private async saveConfig(key: string): Promise<void> {
        const settingRow = this.$<HTMLElement>(`.config-setting[data-key="${key}"]`);
        if (!settingRow) return;

        const input = settingRow.querySelector<HTMLInputElement | HTMLSelectElement>('.config-edit-input');
        if (!input) return;

        const dataType = input.dataset.type;
        let newValue: unknown;

        // Parse the value based on type
        if (dataType === 'boolean') {
            newValue = input.value === 'true';
        } else if (dataType === 'number') {
            newValue = parseFloat(input.value);
            if (isNaN(newValue as number)) {
                this.showToast('Invalid number value', 'error');
                return;
            }
        } else {
            newValue = input.value;
        }

        // Show saving state
        const saveBtn = settingRow.querySelector<HTMLElement>('.config-save-btn');
        if (saveBtn) {
            saveBtn.textContent = 'Saving...';
            saveBtn.classList.add('config-save-saving');
        }

        try {
            await this.updateConfig({ [key]: newValue });
            this.showToast(`Updated ${key}`, 'success');

            // Refresh the config
            this.editingKey = null;
            await this.fetchConfig();
            this.render();
        } catch (error) {
            handleError(error, 'Failed to save config', { context: SEG.ERROR, silent: true });
            this.showToast(`Failed to save: ${(error as Error).message}`, 'error');

            // Reset button state
            if (saveBtn) {
                saveBtn.textContent = 'Save';
                saveBtn.classList.remove('config-save-saving');
            }
        }
    }

    /**
     * Show a toast notification
     */
    private showToast(message: string, type: 'success' | 'error'): void {
        const toast = document.createElement('div');
        toast.className = `config-toast config-toast-${type}`;
        toast.textContent = message;
        document.body.appendChild(toast);
        setTimeout(() => toast.remove(), 3000);
    }

    private filterSettings(searchText: string): void {
        const settings = this.$$('.config-setting');
        const search = searchText.toLowerCase();

        settings.forEach(setting => {
            const htmlSetting = setting as HTMLElement;
            const key = setting.querySelector('.config-setting-key')?.textContent || '';
            const value = setting.querySelector('.config-setting-value')?.textContent || '';
            const matches = key.toLowerCase().includes(search) || value.toLowerCase().includes(search);
            if (matches) {
                htmlSetting.classList.remove('u-hidden');
                htmlSetting.classList.add('u-grid');
            } else {
                htmlSetting.classList.remove('u-grid');
                htmlSetting.classList.add('u-hidden');
            }
        });

        const groups = this.$$('.config-group');
        groups.forEach(group => {
            const htmlGroup = group as HTMLElement;
            const visibleSettings = Array.from(group.querySelectorAll('.config-setting'))
                .filter(s => !(s as HTMLElement).classList.contains('u-hidden'));
            if (visibleSettings.length > 0) {
                htmlGroup.classList.remove('u-hidden');
                htmlGroup.classList.add('u-block');
            } else {
                htmlGroup.classList.remove('u-block');
                htmlGroup.classList.add('u-hidden');
            }
        });
    }

    async updateConfig(updates: Record<string, unknown>): Promise<unknown> {
        const response = await apiFetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ updates })
        });

        if (!response.ok) {
            const error = await response.text();
            throw new Error(`Config update failed: ${error}`);
        }

        return await response.json();
    }

    protected override onDestroy(): void {
        // Clean up pending timeouts to prevent memory leaks
        if (this.saveConfirmTimeout) {
            clearTimeout(this.saveConfirmTimeout);
            this.saveConfirmTimeout = null;
        }
    }
}

// Initialize and export
const configPanel = new ConfigPanel();

export function showConfig(): void {
    configPanel.show();
}

export function hideConfig(): void {
    configPanel.hide();
}

export function toggleConfig(): void {
    configPanel.toggle();
}

export {};
