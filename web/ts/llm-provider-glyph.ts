/**
 * LLM Provider Glyph — AI inference provider selection.
 *
 * Discovers available LLM providers from /api/plugins/routes (role: llm-provider)
 * and lets the user switch between them. Plugin-name agnostic — the UI never
 * hardcodes provider names.
 */

import type { Glyph } from '@qntx/glyphs';
import { BY } from '@generated/sym.js';
import { log, SEG } from './logger';
import { apiFetch } from './client';
import { assertOk, jsonBody } from './http-utils';
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
        title: `${BY} LLM Provider`,
        renderContent: () => {
            const content = document.createElement('div');
            setupLlmProviderContent(content);
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
                <div class="llm-provider-buttons" style="display: flex; gap: 8px; margin: 8px 0; flex-wrap: wrap;"></div>
                <div class="llm-provider-status-line" style="min-height: 20px; font-size: 12px; margin: 4px 0;"></div>
            </div>
            <div class="llm-provider-extra-config glyph-section" style="display: none;"></div>
        </div>
    `;

    const btnGroup = content.querySelector('.llm-provider-buttons')!;
    const statusEl = content.querySelector('.llm-provider-status-line')!;
    const extraConfig = content.querySelector('.llm-provider-extra-config') as HTMLElement;

    const providerButtons = new Map<string, HTMLElement>();

    function showStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        const color = type === 'success' ? '#4ade80' : type === 'error' ? '#f87171' : '#fbbf24';
        statusEl.textContent = message;
        (statusEl as HTMLElement).style.color = color;
        if (type !== 'warning') {
            setTimeout(() => { statusEl.textContent = ''; }, 3000);
        }
    }

    async function updateConfig(updates: Record<string, unknown>): Promise<void> {
        const response = await apiFetch('/api/config', jsonBody('POST', { updates }));
        await assertOk(response, 'Failed to update config');
    }

    function selectProvider(name: string): void {
        for (const [n, btn] of providerButtons) {
            btn.classList.toggle('active', n === name);
        }
        // Show OpenRouter key config only when openrouter is active
        renderExtraConfig(name);
    }

    function renderExtraConfig(provider: string): void {
        extraConfig.innerHTML = '';
        extraConfig.style.display = 'none';

        if (provider === 'openrouter') {
            extraConfig.style.display = '';
            extraConfig.innerHTML = `
                <h3 class="glyph-section-title">OpenRouter API Key</h3>
                <div style="display: flex; gap: 4px; align-items: center;">
                    <input type="password" class="llm-provider-key-input" placeholder="sk-or-v1-..." autocomplete="off"
                        style="flex: 1; background: var(--bg-secondary, #1e293b); color: var(--text-primary, #e2e8f0);
                        border: 1px solid var(--border-color, #334155); border-radius: 4px; padding: 6px 8px;
                        font-family: monospace; font-size: 12px;" />
                    <button class="llm-provider-key-toggle" style="background: transparent; border: 1px solid var(--border-color, #334155);
                        color: var(--text-primary, #e2e8f0); border-radius: 4px; padding: 4px 8px; cursor: pointer;">&#128065;</button>
                    <button class="llm-provider-key-save" style="background: transparent; border: 1px solid var(--border-color, #334155);
                        color: var(--text-primary, #e2e8f0); border-radius: 4px; padding: 4px 8px; cursor: pointer;">&#128190;</button>
                </div>
                <div style="font-size: 11px; color: #cbd5e1; margin-top: 4px;">
                    Get your key from <a href="https://openrouter.ai/keys" target="_blank" rel="noopener"
                        style="color: #60a5fa;">openrouter.ai/keys</a>
                </div>
            `;
            wireKeyConfig(extraConfig);
        }
    }

    function wireKeyConfig(container: HTMLElement): void {
        const keyInput = container.querySelector('.llm-provider-key-input') as HTMLInputElement;
        const keyToggle = container.querySelector('.llm-provider-key-toggle')!;
        const keySave = container.querySelector('.llm-provider-key-save')!;

        keyToggle.addEventListener('click', () => {
            keyInput.type = keyInput.type === 'password' ? 'text' : 'password';
        });

        async function saveKey(): Promise<void> {
            const apiKey = keyInput.value.trim();
            if (!apiKey) { showStatus('Please enter an API key', 'warning'); return; }
            if (!apiKey.startsWith('sk-or-')) { showStatus('Invalid key format (should start with sk-or-)', 'error'); return; }
            try {
                await updateConfig({ 'openrouter.api_key': apiKey });
                showStatus('API key saved', 'success');
                keyInput.value = '';
                keyInput.placeholder = apiKey.substring(0, 10) + '...(configured)';
            } catch (error: unknown) {
                handleError(error, 'Failed to save API key', { context: SEG.ACTOR, silent: true });
                showStatus('Failed to save API key', 'error');
            }
        }

        keySave.addEventListener('click', saveKey);
        keyInput.addEventListener('keypress', (e: KeyboardEvent) => { if (e.key === 'Enter') saveKey(); });

        // Restore saved key placeholder
        apiFetch('/api/config?introspection=true').then(async (resp) => {
            if (!resp.ok) return;
            const config = await resp.json();
            const keySetting = (config.settings as ConfigSetting[]).find(s => s.key === 'openrouter.api_key');
            if (keySetting?.value) {
                const v = keySetting.value as string;
                if (v.length > 10) keyInput.placeholder = v.substring(0, 10) + '...(configured)';
            }
        }).catch(() => {});
    }

    // --- Discover providers and current selection ---
    try {
        const [routesResp, configResp] = await Promise.all([
            apiFetch('/api/plugins/routes'),
            apiFetch('/api/config?introspection=true'),
        ]);

        // Discover LLM providers from plugin routes
        const providers: string[] = [];
        if (routesResp.ok) {
            const data = await routesResp.json();
            for (const route of (data.routes ?? []) as PluginRoute[]) {
                if (route.roles && route.roles.includes('llm-provider')) {
                    providers.push(route.name);
                }
            }
        }

        // Read current provider from config
        let configuredProvider = '';
        if (configResp.ok) {
            const config = await configResp.json();
            const setting = (config.settings as ConfigSetting[]).find(s => s.key === 'llm.provider');
            if (setting?.value) configuredProvider = setting.value as string;
        }

        // Ensure configured provider is in the list (may be built-in, not a plugin)
        if (configuredProvider && !providers.includes(configuredProvider)) {
            providers.unshift(configuredProvider);
        }

        // Fallback: if no providers discovered, show a placeholder
        if (providers.length === 0) {
            statusEl.textContent = 'No LLM providers available';
            (statusEl as HTMLElement).style.color = '#fbbf24';
            return;
        }

        // Build buttons for each provider
        for (const name of providers) {
            const btn = makeProviderButton(name);
            providerButtons.set(name, btn);
            btnGroup.appendChild(btn);

            btn.addEventListener('click', async () => {
                log.debug(SEG.ACTOR, `Switching to ${name}`);
                selectProvider(name);
                try {
                    await updateConfig({ 'llm.provider': name });
                    showStatus(`Using ${name}`, 'success');
                } catch (error: unknown) {
                    handleError(error, `Failed to switch to ${name}`, { context: SEG.ACTOR, silent: true });
                    showStatus('Failed to update config', 'error');
                }
            });
        }

        // Select current provider (or first available)
        selectProvider(configuredProvider || providers[0]);

    } catch (error: unknown) {
        handleError(error, 'Failed to discover LLM providers', { context: SEG.ACTOR, silent: true });
        statusEl.textContent = 'Failed to load providers';
        (statusEl as HTMLElement).style.color = '#f87171';
    }
}

function makeProviderButton(name: string): HTMLElement {
    const btn = document.createElement('button');
    btn.style.cssText = `
        flex: 1; display: flex; flex-direction: column; align-items: center; gap: 2px;
        padding: 8px 12px; border-radius: 6px; cursor: pointer;
        background: var(--bg-secondary, #1e293b); border: 1px solid var(--border-color, #334155);
        color: var(--text-primary, #e2e8f0); transition: border-color 0.15s, background 0.15s;
        min-width: 80px;
    `;

    const nameEl = document.createElement('span');
    nameEl.textContent = name;
    nameEl.style.cssText = 'font-weight: 600; font-size: 14px; color: #f1f5f9;';

    btn.appendChild(nameEl);

    const updateStyle = () => {
        if (btn.classList.contains('active')) {
            btn.style.borderColor = '#60a5fa';
            btn.style.background = 'rgba(96, 165, 250, 0.1)';
        } else {
            btn.style.borderColor = 'var(--border-color, #334155)';
            btn.style.background = 'var(--bg-secondary, #1e293b)';
        }
    };

    const observer = new MutationObserver(updateStyle);
    observer.observe(btn, { attributes: true, attributeFilter: ['class'] });

    return btn;
}
