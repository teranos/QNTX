/**
 * LLM Provider Glyph — AI inference provider selection.
 *
 * Replaces ai-provider-window.ts (Window component → glyph system).
 * Tray glyph: dot in GlyphRun, morphs to window on click.
 * Provider selection: OpenRouter (cloud) and llama.cpp (local Metal).
 */

import type { Glyph } from './components/glyph/glyph';
import { BY } from '@generated/sym.js';
import { log, SEG } from './logger';
import { apiFetch } from './api';
import { handleError } from './error-handler';

interface ConfigResponse {
    config_file?: string;
    settings: Array<{
        key: string;
        value: unknown;
        source: string;
    }>;
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
                <div class="llm-provider-buttons" style="display: flex; gap: 8px; margin: 8px 0;"></div>
                <div class="llm-provider-status-line" style="min-height: 20px; font-size: 12px; margin: 4px 0;"></div>
            </div>
            <div class="llm-provider-openrouter-config glyph-section" style="display: none;">
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
            </div>
        </div>
    `;

    const btnGroup = content.querySelector('.llm-provider-buttons')!;
    const statusEl = content.querySelector('.llm-provider-status-line')!;
    const openrouterConfig = content.querySelector('.llm-provider-openrouter-config') as HTMLElement;
    const keyInput = content.querySelector('.llm-provider-key-input') as HTMLInputElement;
    const keyToggle = content.querySelector('.llm-provider-key-toggle')!;
    const keySave = content.querySelector('.llm-provider-key-save')!;

    // Create provider buttons
    const openrouterBtn = makeProviderButton('OpenRouter', 'Cloud API');
    const llamaCppBtn = makeProviderButton('llama.cpp', 'Local Metal');
    btnGroup.appendChild(openrouterBtn);
    btnGroup.appendChild(llamaCppBtn);

    // --- Behavior ---

    function updateProviderUI(provider: 'openrouter' | 'llama-cpp'): void {
        openrouterBtn.classList.toggle('active', provider === 'openrouter');
        llamaCppBtn.classList.toggle('active', provider === 'llama-cpp');
        openrouterConfig.style.display = provider === 'openrouter' ? '' : 'none';
    }

    function showStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        const color = type === 'success' ? '#4ade80' : type === 'error' ? '#f87171' : '#fbbf24';
        statusEl.textContent = message;
        (statusEl as HTMLElement).style.color = color;
        if (type !== 'warning') {
            setTimeout(() => { statusEl.textContent = ''; }, 3000);
        }
    }

    async function updateConfig(updates: Record<string, unknown>): Promise<void> {
        const response = await apiFetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ updates }),
        });
        if (!response.ok) {
            throw new Error(`Failed to update config: ${response.statusText}`);
        }
    }

    openrouterBtn.addEventListener('click', async () => {
        log.debug(SEG.ACTOR, 'Switching to OpenRouter');
        updateProviderUI('openrouter');
        try {
            await updateConfig({ 'llm.provider': 'openrouter' });
            showStatus('Using OpenRouter (cloud API)', 'success');
        } catch (error: unknown) {
            handleError(error, 'Failed to switch to OpenRouter', { context: SEG.ACTOR, silent: true });
            showStatus('Failed to update config', 'error');
        }
    });

    llamaCppBtn.addEventListener('click', async () => {
        log.debug(SEG.ACTOR, 'Switching to llama.cpp');
        updateProviderUI('llama-cpp');
        try {
            await updateConfig({ 'llm.provider': 'llama-cpp' });
            showStatus('Using llama.cpp (local Metal)', 'success');
        } catch (error: unknown) {
            handleError(error, 'Failed to switch to llama.cpp', { context: SEG.ACTOR, silent: true });
            showStatus('Failed to update config', 'error');
        }
    });

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

    // --- Load initial state ---
    try {
        const response = await apiFetch('/api/config?introspection=true');
        if (response.ok) {
            const config: ConfigResponse = await response.json();
            const settings = config.settings;

            const providerSetting = settings.find(s => s.key === 'llm.provider');
            const configuredProvider = providerSetting?.value as string;

            if (configuredProvider === 'llama-cpp') {
                updateProviderUI('llama-cpp');
            } else {
                updateProviderUI('openrouter');
            }

            const keySetting = settings.find(s => s.key === 'openrouter.api_key');
            if (keySetting?.value) {
                const keyValue = keySetting.value as string;
                if (keyValue.length > 10) {
                    keyInput.placeholder = keyValue.substring(0, 10) + '...(configured)';
                }
            }
        }
    } catch (error: unknown) {
        handleError(error, 'Failed to fetch config', { context: SEG.ACTOR, silent: true });
    }
}

function makeProviderButton(name: string, detail: string): HTMLElement {
    const btn = document.createElement('button');
    btn.style.cssText = `
        flex: 1; display: flex; flex-direction: column; align-items: center; gap: 2px;
        padding: 8px 12px; border-radius: 6px; cursor: pointer;
        background: var(--bg-secondary, #1e293b); border: 1px solid var(--border-color, #334155);
        color: var(--text-primary, #e2e8f0); transition: border-color 0.15s, background 0.15s;
    `;

    const nameEl = document.createElement('span');
    nameEl.textContent = name;
    nameEl.style.cssText = 'font-weight: 600; font-size: 14px; color: #f1f5f9;';

    const detailEl = document.createElement('span');
    detailEl.textContent = detail;
    detailEl.style.cssText = 'font-size: 11px; color: #cbd5e1;';

    btn.appendChild(nameEl);
    btn.appendChild(detailEl);

    // Active state styling
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
