/**
 * ≡'s settings — what the node was told to be, and by which source.
 *
 * This is not a panel that happens to hold ≡'s content. It is ≡'s content,
 * and panel is the manifestation it takes: a tree that scrolls does not fit
 * in the card ≡ draws, so the part of ≡ that does not fit is drawn beside it
 * and opened from it. There was never a panel, only glyphs in a manifestation.
 *
 * It shows and does not write. Every source is drawn for every setting — the
 * one in force, the ones it overrides, and the ones that never spoke — because
 * a value without its provenance is a number somebody has to go and verify.
 *
 * Reads /am/config?introspection=true (internal/config/introspection.go).
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client';
import { AM } from '@generated/sym.js';
import { formatValue } from './html-utils.ts';
import { createRichErrorState, type RichError } from './base-panel-error.ts';
import { extractHttpStatus } from './http-utils.ts';
import { handleError, SEG } from './error-handler.ts';
import { log } from './logger.ts';
import { toast } from './toast.ts';

/** The glyph id ≡ opens, and the tray registers. */
export const AM_CONFIG_GLYPH_ID = 'am-config-glyph';

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

// What the node last said it was told, and what it said instead when it would
// not say. The element is the glyph's content area, held while it is drawn.
let contentElement: HTMLElement | null = null;
let appConfig: ConfigResponse | null = null;
let configError: RichError | null = null;

// The order a source wins in. First named is first in force.
const PRECEDENCE = ['environment', 'project', 'user_ui', 'user', 'system'];

// Every source a setting could come from, drawn whether it spoke or not.
const EVERY_SOURCE = ['environment', 'project', 'user_ui', 'user', 'system', 'default'];

const SOURCE_LABELS: Record<string, string> = {
    environment: 'ENV',
    project: 'PROJECT',
    user_ui: 'USER_UI',
    user: 'USER',
    system: 'SYSTEM',
    default: 'DEFAULT',
    unknown: '?',
};

const SOURCE_PATHS: Record<string, string> = {
    system: '/etc/qntx/config.toml',
    user: '~/.qntx/config.toml',
    user_ui: '~/.qntx/am_from_ui.toml',
    project: 'config.toml (project root)',
    environment: 'Environment variable (QNTX_*)',
    default: 'Built-in default value',
};

function sourceLabel(source: string): string {
    return SOURCE_LABELS[source] || source.toUpperCase();
}

function sourcePath(source: string): string {
    return SOURCE_PATHS[source] || 'Unknown source';
}

/** Escape for an HTML attribute. Regex is banned; these are string swaps. */
function escapeAttr(str: string): string {
    return str
        .split('&').join('&amp;')
        .split('"').join('&quot;')
        .split('<').join('&lt;')
        .split('>').join('&gt;');
}

// ── Asking ──────────────────────────────────────────────────────────

async function fetchConfig(): Promise<void> {
    try {
        log.debug(SEG.UI, '[am/config] asking /am/config?introspection=true');
        configError = null;
        const data = await apiJson<ConfigResponse>('/am/config?introspection=true');

        if (!data || !Array.isArray(data.settings)) {
            throw new Error('Invalid config response: missing settings array');
        }

        appConfig = data;
        log.debug(SEG.UI, '[am/config] loaded', data.settings.length, 'settings');
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch config', { context: SEG.ERROR, silent: true });
        configError = buildConfigError(error);
        appConfig = null;
    }
}

function buildConfigError(error: unknown): RichError {
    const errorMessage = error instanceof Error ? error.message : String(error);
    const errorStack = error instanceof Error ? error.stack : undefined;

    const status = extractHttpStatus(errorMessage);
    if (status !== null) {
        if (status === 404) {
            return {
                title: 'Config Endpoint Not Found',
                message: 'The configuration API endpoint is not available',
                status: 404,
                suggestion: 'Ensure the QNTX server is running with the config endpoint enabled.',
                details: errorStack || errorMessage,
            };
        }
        if (status >= 500) {
            return {
                title: 'Server Error',
                message: 'The server encountered an error loading configuration',
                status: status,
                suggestion: 'Check the server logs for more details.',
                details: errorStack || errorMessage,
            };
        }
    }

    if (errorMessage.includes('NetworkError') || errorMessage.includes('Failed to fetch')) {
        return {
            title: 'Network Error',
            message: 'Unable to connect to the QNTX server',
            suggestion: 'Check your network connection and ensure the QNTX server is running.',
            details: errorStack || errorMessage,
        };
    }

    return {
        title: 'Configuration Error',
        message: errorMessage,
        suggestion: 'Check the error details for more information.',
        details: errorStack || errorMessage,
    };
}

// ── What is in force, and what it overrode ──────────────────────────

function calculateMergedConfig(settings: ConfigSetting[]): { effectiveSettings: EnhancedSetting[], allSettings: EnhancedSetting[] } {
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
            const precedence = PRECEDENCE.indexOf(source.source);
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
                    source_path: s.source_path,
                })),
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

// ── Tooltips ────────────────────────────────────────────────────────

function buildSourceTooltip(source: string, path: string, isActive: boolean, isDefined: boolean, value?: unknown): string {
    const parts: string[] = [];

    parts.push(`Source: ${source.toUpperCase()}`);
    parts.push(`Path: ${path}`);
    parts.push('---');

    if (isActive) {
        parts.push('Status: ACTIVE (this value is used)');
    } else if (isDefined) {
        parts.push('Status: OVERRIDDEN');
        if (value !== undefined) {
            parts.push(`Value: ${JSON.stringify(value)}`);
        }
    } else {
        parts.push('Status: Not defined at this level');
    }

    if (isDefined) {
        parts.push('---');
        parts.push('Click to copy path');
    }

    return parts.join('\n');
}

function buildSettingTooltip(setting: EnhancedSetting): string {
    const parts: string[] = [];

    parts.push(`Setting: ${setting.key}`);
    parts.push(`Effective Value: ${JSON.stringify(setting.value)}`);
    parts.push(`Source: ${setting.source}`);

    if (setting.allSources.length > 1) {
        parts.push('---');
        parts.push(`Defined in ${setting.allSources.length} sources:`);
        setting.allSources.forEach(s => {
            const marker = s.source === setting.source ? '→' : ' ';
            parts.push(`${marker} ${s.source}: ${JSON.stringify(s.value)}`);
        });
    }

    return parts.join('\n');
}

// ── Drawing ─────────────────────────────────────────────────────────

function renderEffectiveSetting(setting: EnhancedSetting): string {
    const valueDisplay = formatValue(setting.value, true); // Secrets are always masked here
    const definedSources = new Set(setting.allSources.map(s => s.source));

    const sourcesDisplay = EVERY_SOURCE
        .map(source => {
            const label = sourceLabel(source);
            const isActive = source === setting.source;
            const isDefined = definedSources.has(source);
            const sourceData = setting.allSources.find(s => s.source === source);
            const path = sourceData?.source_path || sourcePath(source);
            const tooltip = escapeAttr(buildSourceTooltip(source, path, isActive, isDefined, sourceData?.value));

            if (isActive) {
                return `<span class="source-active source-clickable has-tooltip" data-source="${source}" data-path="${escapeAttr(path)}" data-tooltip="${tooltip}">${label}</span>`;
            }
            if (isDefined) {
                return `<span class="source-inactive source-clickable has-tooltip" data-source="${source}" data-path="${escapeAttr(path)}" data-tooltip="${tooltip}">${label}</span>`;
            }
            return `<span class="source-undefined has-tooltip" data-tooltip="${tooltip}">${label}</span>`;
        })
        .join(' ');

    return `
        <div class="config-setting" data-key="${escapeAttr(setting.key)}">
            <div class="config-setting-key has-tooltip" data-tooltip="${escapeAttr(buildSettingTooltip(setting))}">${setting.key}</div>
            <div class="config-setting-value">${valueDisplay}</div>
            <div class="config-setting-sources">${sourcesDisplay}</div>
        </div>
    `;
}

function renderMergedConfig(effectiveSettings: EnhancedSetting[]): string {
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
            ${settings.map(setting => renderEffectiveSetting(setting)).join('')}
        </div>
    `).join('');
}

function render(): void {
    if (!contentElement) return;

    if (configError) {
        contentElement.innerHTML = '';
        contentElement.appendChild(createRichErrorState(configError, async () => {
            contentElement!.innerHTML = '<div class="glyph-loading">Retrying...</div>';
            await fetchConfig();
            render();
        }));
        return;
    }

    if (!appConfig || appConfig.settings.length === 0) {
        contentElement.innerHTML = '<div class="glyph-empty">No configuration loaded</div>';
        return;
    }

    const mergedConfig = calculateMergedConfig(appConfig.settings);
    appConfig.settingsEnhanced = mergedConfig.allSettings;

    contentElement.innerHTML = `
        <div class="config-search">
            <input type="text" class="config-search-input" placeholder="Filter settings..." />
        </div>
        <div class="panel-card config-file-info">
            <strong>Final Merged Config</strong>
            <span class="config-file-hint">This is what the server sees</span>
        </div>
        <div class="config-settings">
            ${renderMergedConfig(mergedConfig.effectiveSettings)}
        </div>
    `;
}

// ── What a click and a keystroke do ─────────────────────────────────

function copySourcePath(source: string, path: string): void {
    log.debug(SEG.UI, `[am/config] source clicked: ${source} (${path})`);

    navigator.clipboard.writeText(path).then(() => {
        toast.success(`Copied to clipboard: ${path}`);
    }).catch((error: unknown) => {
        log.error(SEG.ERROR, '[am/config] path not copied:', error);
        const message = error instanceof Error ? error.message : 'Clipboard access denied';
        toast.error(`Failed to copy: ${message}`);
    });
}

function filterSettings(searchText: string): void {
    if (!contentElement) return;
    const search = searchText.toLowerCase();

    contentElement.querySelectorAll('.config-setting').forEach(setting => {
        const htmlSetting = setting as HTMLElement;
        const key = setting.querySelector('.config-setting-key')?.textContent || '';
        const value = setting.querySelector('.config-setting-value')?.textContent || '';
        const matches = key.toLowerCase().includes(search) || value.toLowerCase().includes(search);
        htmlSetting.classList.toggle('u-hidden', !matches);
        htmlSetting.classList.toggle('u-grid', matches);
    });

    contentElement.querySelectorAll('.config-group').forEach(group => {
        const htmlGroup = group as HTMLElement;
        const shown = Array.from(group.querySelectorAll('.config-setting'))
            .some(s => !(s as HTMLElement).classList.contains('u-hidden'));
        htmlGroup.classList.toggle('u-hidden', !shown);
        htmlGroup.classList.toggle('u-block', shown);
    });
}

// Delegated once on the content element, so it survives every innerHTML.
function attachEventDelegation(content: HTMLElement): void {
    content.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;
        const sourceSpan = target.closest('.source-clickable') as HTMLElement | null;
        if (sourceSpan?.dataset.source) {
            copySourcePath(sourceSpan.dataset.source, sourceSpan.dataset.path || sourcePath(sourceSpan.dataset.source));
        }
    });

    content.addEventListener('input', (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.classList.contains('config-search-input')) {
            filterSettings((target as HTMLInputElement).value);
        }
    });
}

// ── The glyph ───────────────────────────────────────────────────────

/** ≡'s settings in the tray: a panel, because a tree that scrolls is one. */
export function createAmConfigGlyph(): Glyph {
    return {
        id: AM_CONFIG_GLYPH_ID,
        title: `${AM} Configuration`,
        symbol: AM,
        manifestationType: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;
            attachEventDelegation(content);

            content.innerHTML = '<div class="glyph-loading">Loading configuration...</div>';

            fetchConfig().then(() => {
                render();
                setTimeout(() => {
                    contentElement?.querySelector<HTMLInputElement>('.config-search-input')?.focus();
                }, 100);
            }).catch((err: unknown) => log.error(SEG.UI, '[am/config] initial load failed:', err));

            return content;
        },
    };
}

/** Open ≡'s settings. The runtime's morph is what showing is. */
export function showConfig(): void {
    glyphRun.openGlyph(AM_CONFIG_GLYPH_ID);
}

/**
 * Cmd+, and the Tauri menu both mean "show me the settings". A glyph already
 * drawn stays drawn — morphGlyph returns early on one in a manifestation — so
 * this is show, not a toggle that could hide what somebody just asked for.
 */
export function toggleConfig(): void {
    showConfig();
}
