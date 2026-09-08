/**
 * ⌬ — which provider the node infers with.
 *
 * Discovers LLM providers from /api/plugins/routes (role: llm-provider) and
 * says which one am.toml names. Plugin-name agnostic — the UI never hardcodes
 * provider names.
 *
 * It shows and does not set. A node is configured by am.toml and the
 * environment; nothing writes configuration from here, so switching provider
 * is an edit to that file and a restart, not a click.
 */

import type { Glyph } from '@qntx/glyphs';
import { BY } from '@generated/sym.js';
import { log, SEG } from './logger';
import { apiFetch } from './client';
import { escapeHtml } from './html-utils';
import { handleError } from './error-handler';

interface PluginRoute {
    name: string;
    roles?: string[];
}

interface ConfigSetting {
    key: string;
    value: unknown;
    source: string;
}

export function createLlmProviderGlyph(): Glyph {
    return {
        id: 'llm-provider-glyph',
        title: 'LLM Provider',
        symbol: BY,
        renderContent: () => {
            const content = document.createElement('div');
            setupLlmProviderContent(content).catch((err: unknown) => log.error(SEG.CONFIG, '[LLMProvider] panel content failed:', err));
            return content;
        },
        initialWidth: '420px',
        initialHeight: '240px',
    };
}

async function setupLlmProviderContent(content: HTMLElement): Promise<void> {
    content.innerHTML = `
        <div class="glyph-content">
            <div class="glyph-section">
                <h3 class="glyph-section-title">AI Inference Provider</h3>
                <div class="llm-provider-list"></div>
                <div class="llm-provider-status-line"></div>
            </div>
        </div>
    `;

    const listEl = content.querySelector('.llm-provider-list')!;
    const statusEl = content.querySelector('.llm-provider-status-line') as HTMLElement;

    function saySo(message: string, well: boolean): void {
        statusEl.textContent = message;
        statusEl.className = `llm-provider-status-line ${well ? 'glyph-well' : 'glyph-unwell'}`;
    }

    try {
        const [routesResp, configResp] = await Promise.all([
            apiFetch('/api/plugins/routes'),
            apiFetch('/am/config?introspection=true'),
        ]);

        const providers: string[] = [];
        if (routesResp.ok) {
            const data = await routesResp.json();
            for (const route of (data.routes ?? []) as PluginRoute[]) {
                if (route.roles && route.roles.includes('llm-provider')) {
                    providers.push(route.name);
                }
            }
        }

        // Which one am.toml names, and where that answer came from.
        let configuredProvider = '';
        let configuredSource = '';
        if (configResp.ok) {
            const config = await configResp.json();
            const setting = (config.settings as ConfigSetting[]).find(s => s.key === 'llm.provider');
            if (setting?.value) {
                configuredProvider = setting.value as string;
                configuredSource = setting.source;
            }
        }

        // The named provider may be built in rather than a plugin, so it is not
        // always among the discovered routes.
        if (configuredProvider && !providers.includes(configuredProvider)) {
            providers.unshift(configuredProvider);
        }

        if (providers.length === 0) {
            saySo('No LLM providers available', false);
            return;
        }

        listEl.innerHTML = providers.map(name => {
            const inUse = name === configuredProvider;
            return `
                <div class="glyph-row llm-provider-row${inUse ? ' llm-provider-row--in-use' : ''}">
                    <span class="glyph-label">${escapeHtml(name)}</span>
                    <span class="glyph-value">${inUse ? '<span class="glyph-well">in use</span>' : ''}</span>
                </div>
            `;
        }).join('');

        if (configuredProvider) {
            saySo(`llm.provider is ${configuredProvider}${configuredSource ? ` (${configuredSource})` : ''}`, true);
        } else {
            saySo('llm.provider is unset; the node uses its default', false);
        }
    } catch (error: unknown) {
        handleError(error, 'Failed to discover LLM providers', { context: SEG.ACTOR, silent: true });
        saySo('Failed to load providers', false);
    }
}
