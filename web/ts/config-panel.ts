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
        await this.fetchConfig();
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
            this.appConfig = {
                config_file: `Error: ${errorMessage}`,
                settings: []
            };
        }
    }

    private render(): void {
        const content = this.$('#config-panel-content');
        if (!content) return;

        if (!this.appConfig || this.appConfig.settings.length === 0) {
            content.innerHTML = `
                <div class="config-empty">
                    <p>No configuration loaded</p>
                </div>
            `;
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
        const valueDisplay = this.formatValue(setting.value);
        const allPossibleSources = ['environment', 'project', 'user_ui', 'user', 'system', 'default'];
        const definedSources = new Set(setting.allSources.map(s => s.source));

        const sourcesDisplay = allPossibleSources
            .map(source => {
                const label = this.getSourceLabel(source);
                const isActive = source === setting.source;
                const isDefined = definedSources.has(source);
                const sourceData = setting.allSources.find(s => s.source === source);
                const path = sourceData?.source_path || this.getSourcePath(source);

                if (isActive) {
                    return `<span class="source-active source-clickable" data-source="${source}" data-path="${path}" title="${path} (active)">${label}</span>`;
                } else if (isDefined) {
                    return `<span class="source-inactive source-clickable" data-source="${source}" data-path="${path}" title="${path} (overridden)">${label}</span>`;
                } else {
                    return `<span class="source-undefined" title="${this.getSourcePath(source)} (not defined)">${label}</span>`;
                }
            })
            .join(' ');

        const isEditable = setting.source === 'user_ui';
        const editControl = isEditable ? `<button class="config-edit-btn" data-key="${setting.key}" title="Edit">✎</button>` : '';

        return `
            <div class="config-setting" data-key="${setting.key}">
                <div class="config-setting-key">${setting.key}</div>
                <div class="config-setting-value">${valueDisplay}</div>
                <div class="config-setting-sources">${sourcesDisplay}</div>
                ${editControl}
            </div>
        `;
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

    private formatValue(value: unknown): string {
        if (value === null || value === undefined) {
            return '<span class="config-value-null">null</span>';
        }
        if (typeof value === 'boolean') {
            return `<span class="config-value-bool">${value}</span>`;
        }
        if (typeof value === 'number') {
            return `<span class="config-value-number">${value}</span>`;
        }
        if (typeof value === 'object') {
            return `<span class="config-value-object">${JSON.stringify(value)}</span>`;
        }
        const str = String(value);
        if (this.looksLikeSecret(str)) {
            return `<span class="config-value-secret">********</span>`;
        }
        return `<span class="config-value-string">${this.escapeHtml(str)}</span>`;
    }

    private looksLikeSecret(value: string): boolean {
        const str = String(value).toLowerCase();
        return (
            str.includes('token') ||
            str.includes('key') ||
            str.includes('secret') ||
            str.includes('password') ||
            str.includes('bearer')
        );
    }

    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
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
