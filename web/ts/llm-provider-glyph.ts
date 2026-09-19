/**
 * ⌬ — which provider the node infers with.
 *
 * Discovers LLM providers from /api/plugins/routes (role: llm-provider).
 * Plugin-name agnostic — the UI never hardcodes provider names.
 *
 * It shows and does not set. A node is configured by am.toml and the
 * environment; nothing writes configuration from here, so switching provider
 * is an edit to that file and a restart, not a click.
 */

import type { Element } from '@teranos/elements';
import { BY } from './sym';
import { log, SEG } from './logger';
import { apiFetch } from './client';
import { escapeHtml } from './html-utils';
import { handleError } from './error-handler';

interface PluginRoute {
    name: string;
    roles?: string[];
}

export function createLlmProviderGlyph(): Element {
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
        const routesResp = await apiFetch('/api/plugins/routes');

        const providers: string[] = [];
        if (routesResp.ok) {
            const data = await routesResp.json();
            for (const route of (data.routes ?? []) as PluginRoute[]) {
                if (route.roles && route.roles.includes('llm-provider')) {
                    providers.push(route.name);
                }
            }
        }

        if (providers.length === 0) {
            saySo('No LLM providers available', false);
            return;
        }

        listEl.innerHTML = providers.map(name => `
                <div class="glyph-row llm-provider-row">
                    <span class="label">${escapeHtml(name)}</span>
                </div>
            `).join('');

        saySo('which of these the node infers with is not answered here', false);
    } catch (error: unknown) {
        handleError(error, 'Failed to discover LLM providers', { context: SEG.ACTOR, silent: true });
        saySo('Failed to load providers', false);
    }
}
