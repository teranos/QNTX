/**
 * Config Panel - Shows active configuration and sources
 *
 * Displays configuration introspection when clicking ⍟ (i) in the SEGpalette:
 * - Shows active config file path
 * - Lists all settings with their sources (environment, config_file, default)
 * - Color-coded by source for quick visual identification
 *
 * Uses /api/config?introspection=true endpoint from internal/config/introspection.go
 */

import { apiFetch } from './api.ts';
import { AM } from '@types/sym.js';

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

class ConfigPanel {
    private panel: HTMLElement | null = null;
    private overlay: HTMLElement;
    private isVisible: boolean = false;
    private config: ConfigResponse | null = null;

    constructor() {
        // Create overlay element
        this.overlay = document.createElement('div');
        this.overlay.className = 'panel-overlay config-panel-overlay';
        this.overlay.addEventListener('click', () => this.hide());
        document.body.appendChild(this.overlay);

        this.initialize();
    }

    initialize(): void {
        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'config-panel';
        this.panel.className = 'panel-slide-left config-panel';
        this.panel.innerHTML = this.getTemplate();
        document.body.appendChild(this.panel);

        // Click outside to close (now handled by overlay)
        // Kept for palette cell clicks
        document.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;
            if (this.panel && this.isVisible && !this.panel.contains(target) && !target.closest('.palette-cell[data-cmd="am"]')) {
                this.hide();
            }
        });

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
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

    setupEventListeners(): void {
        if (!this.panel) return;

        // Close button
        const closeBtn = this.panel.querySelector('.config-panel-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.hide());
        }

        // Search input
        const searchInput = this.panel.querySelector('.config-search-input') as HTMLInputElement | null;
        if (searchInput) {
            searchInput.addEventListener('input', (e: Event) => {
                const target = e.target as HTMLInputElement;
                this.filterSettings(target.value);
            });
        }

        // Source click handler (event delegation)
        const content = this.panel.querySelector('.config-panel-content');
        if (content) {
            content.addEventListener('click', (e: Event) => {
                const target = e.target as HTMLElement;
                const sourceSpan = target.closest('.source-clickable') as HTMLElement | null;
                if (sourceSpan && sourceSpan.dataset.source) {
                    const source = sourceSpan.dataset.source;
                    const path = sourceSpan.dataset.path || this.getSourcePath(source);
                    this.handleSourceClick(source, path);
                }
            });
        }
    }

    handleSourceClick(source: string, path: string): void {
        console.log(`[Config Panel] Clicked source: ${source} (${path})`);

        // Copy path to clipboard for easy access
        navigator.clipboard.writeText(path).then(() => {
            // Show toast notification
            const toast = document.createElement('div');
            toast.className = 'config-toast';
            toast.textContent = `Copied to clipboard: ${path}`;
            toast.style.cssText = `
                position: fixed;
                bottom: 20px;
                right: 20px;
                background: #333;
                color: #fff;
                padding: 12px 20px;
                border-radius: 4px;
                font-size: 12px;
                z-index: 10000;
                box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            `;
            document.body.appendChild(toast);

            setTimeout(() => {
                toast.remove();
            }, 2000);
        }).catch(err => {
            console.error('[Config Panel] Failed to copy path:', err);
        });
    }

    async show(): Promise<void> {
        if (!this.panel) return;

        this.isVisible = true;
        this.panel.classList.add('visible');
        this.overlay.classList.add('visible');

        // Fetch config introspection
        await this.fetchConfig();
        this.render();

        // Focus search input
        const searchInput = this.panel.querySelector('.config-search-input') as HTMLInputElement | null;
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }
    }

    hide(): void {
        if (!this.panel) return;

        this.isVisible = false;
        this.panel.classList.remove('visible');
        this.overlay.classList.remove('visible');
    }

    toggle(): void {
        if (this.isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    async fetchConfig(): Promise<void> {
        try {
            console.log('[Config Panel] Fetching config from /api/config?introspection=true...');
            const response = await apiFetch('/api/config?introspection=true');

            console.log('[Config Panel] Response status:', response.status);

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP ${response.status}: ${response.statusText}\n${errorText}`);
            }

            const data = await response.json();
            console.log('[Config Panel] Received config data:', data);

            // Validate response structure
            if (!data || !Array.isArray(data.settings)) {
                console.error('[Config Panel] Invalid response structure:', data);
                throw new Error('Invalid config response: missing settings array');
            }

            this.config = data;
            console.log('[Config Panel] Successfully loaded config with', data.settings.length, 'settings');
        } catch (error) {
            console.error('[Config Panel] Failed to fetch config:', error);
            const errorMessage = error instanceof Error ? error.message : String(error);
            this.config = {
                config_file: `Error: ${errorMessage}`,
                settings: []
            };
        }
    }

    render(): void {
        if (!this.panel) return;

        const content = this.panel.querySelector('#config-panel-content');
        if (!content) return;

        if (!this.config || this.config.settings.length === 0) {
            content.innerHTML = `
                <div class="config-empty">
                    <p>No configuration loaded</p>
                </div>
            `;
            return;
        }

        // Calculate precedence and get final merged config
        const mergedConfig = this.calculateMergedConfig(this.config.settings);

        // Store enhanced settings for AI provider setup
        this.config.settingsEnhanced = mergedConfig.allSettings;

        const html = `
            <div class="panel-card config-file-info">
                <strong>Final Merged Config</strong>
                <span class="config-file-hint">This is what the server sees</span>
            </div>
            <div class="config-settings">
                ${this.renderMergedConfig(mergedConfig.effectiveSettings)}
            </div>
        `;

        content.innerHTML = html;
    }

    calculateMergedConfig(settings: ConfigSetting[]): { effectiveSettings: EnhancedSetting[], allSettings: EnhancedSetting[] } {
        // Precedence: environment > project > user_ui > user > system (higher wins)
        const precedenceOrder = ['environment', 'project', 'user_ui', 'user', 'system'];

        // Build map of key -> all sources that define it
        const settingsByKey: Record<string, ConfigSetting[]> = {};

        settings.forEach(setting => {
            if (!settingsByKey[setting.key]) {
                settingsByKey[setting.key] = [];
            }
            settingsByKey[setting.key].push(setting);
        });

        // For each key, determine effective source and mark others as overridden
        const effectiveSettings: EnhancedSetting[] = [];
        const allSettings: EnhancedSetting[] = [];

        Object.entries(settingsByKey).forEach(([key, sources]) => {
            // Find highest precedence source
            let effectiveSource: ConfigSetting | null = null;
            let effectivePrecedence = Infinity;

            sources.forEach(source => {
                const precedence = precedenceOrder.indexOf(source.source);
                if (precedence >= 0 && precedence < effectivePrecedence) {
                    effectivePrecedence = precedence;
                    effectiveSource = source;
                }
            });

            // If no effective source found (shouldn't happen), use first source
            if (!effectiveSource && sources.length > 0) {
                effectiveSource = sources[0];
                console.warn('[Config Panel] No effective source found for key:', key, 'using first source:', effectiveSource.source);
            }

            // Skip if still no effective source
            if (!effectiveSource) {
                console.error('[Config Panel] No sources found for key:', key);
                return;
            }

            // Mark all settings for this key
            sources.forEach(source => {
                const isEffective = source === effectiveSource;
                const enhanced: EnhancedSetting = {
                    ...source,
                    isEffective,
                    overriddenBy: isEffective ? null : effectiveSource!.source,
                    allSources: sources.map(s => ({
                        source: s.source,
                        value: s.value,
                        source_path: s.source_path // Preserve source_path from backend
                    }))
                };

                allSettings.push(enhanced);

                // Only add effective one to final config
                if (isEffective) {
                    effectiveSettings.push(enhanced);
                }
            });
        });

        // Sort effective settings by key for readability
        effectiveSettings.sort((a, b) => a.key.localeCompare(b.key));

        return { effectiveSettings, allSettings };
    }

    renderMergedConfig(effectiveSettings: EnhancedSetting[]): string {
        // Group by top-level key for organization
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

    renderEffectiveSetting(setting: EnhancedSetting): string {
        const valueDisplay = this.formatValue(setting.value);

        // Build sources display - show ALL possible sources with active one bold
        const allPossibleSources = ['environment', 'project', 'user_ui', 'user', 'system', 'default'];

        // Create map of which sources actually define this setting
        const definedSources = new Set(setting.allSources.map(s => s.source));

        const sourcesDisplay = allPossibleSources
            .map(source => {
                const label = this.getSourceLabel(source);
                const isActive = source === setting.source;
                const isDefined = definedSources.has(source);

                // Get actual path from backend data
                const sourceData = setting.allSources.find(s => s.source === source);
                const path = sourceData?.source_path || this.getSourcePath(source);

                if (isActive) {
                    return `<span class="source-active source-clickable" data-source="${source}" data-path="${path}" title="${path} (active)">${label}</span>`;
                } else if (isDefined) {
                    return `<span class="source-inactive source-clickable" data-source="${source}" data-path="${path}" title="${path} (overridden)">${label}</span>`;
                } else {
                    // Source doesn't define this setting - show as very dim
                    return `<span class="source-undefined" title="${this.getSourcePath(source)} (not defined)">${label}</span>`;
                }
            })
            .join(' ');

        // Check if editable (user_ui source)
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

    getSourceLabel(source: string): string {
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

    getSourcePath(source: string): string {
        // Return likely file path for each source type
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

    formatValue(value: unknown): string {
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
        // String - check if it looks like a secret
        const str = String(value);
        if (this.looksLikeSecret(str)) {
            return `<span class="config-value-secret">********</span>`;
        }
        return `<span class="config-value-string">${this.escapeHtml(str)}</span>`;
    }

    looksLikeSecret(value: string): boolean {
        const str = String(value).toLowerCase();
        return (
            str.includes('token') ||
            str.includes('key') ||
            str.includes('secret') ||
            str.includes('password') ||
            str.includes('bearer')
        );
    }

    escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    filterSettings(searchText: string): void {
        if (!this.panel) return;

        const settings = this.panel.querySelectorAll('.config-setting');
        const search = searchText.toLowerCase();

        settings.forEach(setting => {
            const htmlSetting = setting as HTMLElement;
            const key = setting.querySelector('.config-setting-key')?.textContent || '';
            const value = setting.querySelector('.config-setting-value')?.textContent || '';

            const matches = key.toLowerCase().includes(search) ||
                          value.toLowerCase().includes(search);

            htmlSetting.style.display = matches ? 'grid' : 'none';
        });

        // Hide config groups with no visible settings
        const groups = this.panel.querySelectorAll('.config-group');
        groups.forEach(group => {
            const htmlGroup = group as HTMLElement;
            const visibleSettings = Array.from(group.querySelectorAll('.config-setting'))
                .filter(s => (s as HTMLElement).style.display !== 'none');
            htmlGroup.style.display = visibleSettings.length > 0 ? 'block' : 'none';
        });
    }

    async updateConfig(updates: Record<string, unknown>): Promise<unknown> {
        // Call backend API to update config
        const response = await apiFetch('/api/config', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({updates})
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
