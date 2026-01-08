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

        // Source click handler (event delegation)
        const content = this.$('.config-panel-content');
        content?.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;
            const sourceSpan = target.closest('.source-clickable') as HTMLElement | null;
            if (sourceSpan?.dataset.source) {
                const source = sourceSpan.dataset.source;
                const path = sourceSpan.dataset.path || this.getSourcePath(source);
                this.handleSourceClick(source, path);
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
            console.error('[Config Panel] Failed to fetch config:', error);
            const errorMessage = error instanceof Error ? error.message : String(error);
            const errorStack = error instanceof Error ? error.stack : undefined;

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
        const editControl = isEditable ? `<button class="config-edit-btn" data-key="${setting.key}" title="Edit">✎</button>` : '';

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
