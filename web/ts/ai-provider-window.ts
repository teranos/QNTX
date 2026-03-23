/**
 * AI Provider Panel - Actor/Agent Configuration
 *
 * TODO: Migrate to glyph manifestation, then delete. See components/window.ts.
 *
 * Shows AI inference provider selection when clicking ⌬ (by/actor) in the symbol palette.
 * Allows switching between OpenRouter (cloud) and llama.cpp (local) providers.
 */

import { Window } from './components/window.ts';
import { apiFetch } from './api.ts';
import { BY } from '@generated/sym.js';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';
import { tooltip } from './components/tooltip.ts';

interface ConfigResponse {
    config_file?: string;
    settings: Array<{
        key: string;
        value: unknown;
        source: string;
    }>;
}

class AIProviderPanel {
    private window: Window;
    private appConfig: ConfigResponse | null = null;

    constructor() {
        this.window = new Window({
            id: 'ai-provider-window',
            title: `${BY} Actor / AI Provider`,
            width: '480px',
            onShow: () => this.onShow(),
        });

        this.setupContent();
    }

    private setupContent(): void {
        const content = `
            <div class="ai-provider-content">
                <div class="config-toggle-header">
                    <span class="config-toggle-title">AI Inference Provider</span>
                    <span id="ai-provider-status" class="config-toggle-status"></span>
                </div>
                <div class="config-toggle-control">
                    <button id="provider-openrouter-btn" class="provider-btn active">
                        <span class="provider-icon">☁️</span>
                        <span class="provider-name">OpenRouter</span>
                        <span class="provider-detail">Cloud API</span>
                    </button>
                    <button id="provider-llama-cpp-btn" class="provider-btn">
                        <span class="provider-icon">🦙</span>
                        <span class="provider-name">llama.cpp</span>
                        <span class="provider-detail" id="llama-cpp-status">Local Metal</span>
                    </button>
                </div>
                <div id="openrouter-config" class="provider-config">
                    <div class="api-key-section">
                        <label for="openrouter-api-key">OpenRouter API Key:</label>
                        <div class="api-key-input-group">
                            <input
                                type="password"
                                id="openrouter-api-key"
                                class="api-key-input"
                                placeholder="sk-or-v1-..."
                                autocomplete="off"
                            />
                            <button id="openrouter-key-toggle" class="api-key-toggle has-tooltip" data-tooltip="Show/Hide">👁</button>
                            <button id="openrouter-key-save" class="api-key-save has-tooltip" data-tooltip="Save">💾</button>
                        </div>
                        <div class="api-key-hint">Get your key from <a href="https://openrouter.ai/keys" target="_blank" rel="noopener">openrouter.ai/keys</a></div>
                    </div>
                </div>
            </div>
        `;

        this.window.setContent(content);
        this.setupEventListeners();
        this.setupTooltips();
    }

    private setupTooltips(): void {
        const windowEl = this.window.getElement();
        tooltip.attach(windowEl, '.has-tooltip');
    }

    private setupEventListeners(): void {
        const windowEl = this.window.getElement();

        const openrouterBtn = windowEl.querySelector('#provider-openrouter-btn');
        const llamaCppBtn = windowEl.querySelector('#provider-llama-cpp-btn');

        openrouterBtn?.addEventListener('click', () => this.switchToOpenRouter());
        llamaCppBtn?.addEventListener('click', () => this.switchToLlamaCpp());

        // OpenRouter API key handling
        const keyInput = windowEl.querySelector<HTMLInputElement>('#openrouter-api-key');
        const keyToggle = windowEl.querySelector('#openrouter-key-toggle');
        const keySave = windowEl.querySelector('#openrouter-key-save');

        keyToggle?.addEventListener('click', () => {
            if (keyInput) {
                keyInput.type = keyInput.type === 'password' ? 'text' : 'password';
                if (keyToggle.textContent) {
                    keyToggle.textContent = keyInput.type === 'password' ? '👁' : '👁‍🗨';
                }
            }
        });

        keySave?.addEventListener('click', () => this.saveOpenRouterKey());
        keyInput?.addEventListener('keypress', (e: KeyboardEvent) => {
            if (e.key === 'Enter') {
                this.saveOpenRouterKey();
            }
        });
    }

    private async onShow(): Promise<void> {
        await this.fetchConfig();
        this.setupProviderButtons();
        await this.loadOpenRouterKey();
    }

    private async fetchConfig(): Promise<void> {
        try {
            const response = await apiFetch('/api/config?introspection=true');
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            this.appConfig = await response.json();
        } catch (error: unknown) {
            handleError(error, 'Failed to fetch config', { context: SEG.ACTOR, silent: true });
        }
    }

    private setupProviderButtons(): void {
        if (!this.appConfig?.settings) return;

        const providerSetting = this.appConfig.settings.find(s => s.key === 'llm.provider');
        const configuredProvider = providerSetting?.value as string;

        let activeProvider: 'openrouter' | 'llama-cpp' = 'openrouter';
        if (configuredProvider === 'llama-cpp') {
            activeProvider = 'llama-cpp';
        }

        this.updateProviderUI(activeProvider);
    }

    private async switchToOpenRouter(): Promise<void> {
        log.debug(SEG.ACTOR, 'Switching to OpenRouter');

        this.updateProviderUI('openrouter');

        try {
            await this.updateConfig({
                'llm.provider': 'openrouter',
            });

            this.updateStatus('Using OpenRouter (cloud API)', 'success');
        } catch (error: unknown) {
            handleError(error, 'Failed to switch to OpenRouter', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to update config', 'error');
        }
    }

    private async switchToLlamaCpp(): Promise<void> {
        log.debug(SEG.ACTOR, 'Switching to llama.cpp');

        this.updateProviderUI('llama-cpp');

        try {
            await this.updateConfig({
                'llm.provider': 'llama-cpp',
            });

            this.updateStatus('Using llama.cpp (local Metal)', 'success');
        } catch (error: unknown) {
            handleError(error, 'Failed to switch to llama.cpp', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to update config', 'error');
        }
    }

    private updateProviderUI(provider: 'openrouter' | 'llama-cpp'): void {
        const windowEl = this.window.getElement();
        const openrouterBtn = windowEl.querySelector('#provider-openrouter-btn');
        const llamaCppBtn = windowEl.querySelector('#provider-llama-cpp-btn');
        const openrouterConfig = windowEl.querySelector('#openrouter-config');

        openrouterBtn?.classList.remove('active');
        llamaCppBtn?.classList.remove('active');
        openrouterConfig?.classList.add('u-hidden');

        if (provider === 'openrouter') {
            openrouterBtn?.classList.add('active');
            openrouterConfig?.classList.remove('u-hidden');
        } else {
            llamaCppBtn?.classList.add('active');
        }
    }

    private updateStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        const windowEl = this.window.getElement();
        const statusEl = windowEl.querySelector('#ai-provider-status');
        if (statusEl) {
            statusEl.textContent = message;
            statusEl.className = `config-toggle-status status-${type}`;

            if (type !== 'warning') {
                setTimeout(() => {
                    statusEl.textContent = '';
                    statusEl.className = 'config-toggle-status';
                }, 3000);
            }
        }
    }

    private async updateConfig(updates: Record<string, unknown>): Promise<unknown> {
        const response = await apiFetch('/api/config', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ updates }),
        });

        if (!response.ok) {
            throw new Error(`Failed to update config: ${response.statusText}`);
        }

        return response.json();
    }

    private async loadOpenRouterKey(): Promise<void> {
        if (!this.appConfig?.settings) return;

        const keySetting = this.appConfig.settings.find(s => s.key === 'openrouter.api_key');
        const windowEl = this.window.getElement();
        const keyInput = windowEl.querySelector<HTMLInputElement>('#openrouter-api-key');

        if (keyInput && keySetting?.value) {
            const keyValue = keySetting.value as string;
            if (keyValue.length > 10) {
                keyInput.placeholder = keyValue.substring(0, 10) + '...(configured)';
            }
        }
    }

    private async saveOpenRouterKey(): Promise<void> {
        const windowEl = this.window.getElement();
        const keyInput = windowEl.querySelector<HTMLInputElement>('#openrouter-api-key');
        if (!keyInput) return;

        const apiKey = keyInput.value.trim();
        if (!apiKey) {
            this.updateStatus('Please enter an API key', 'warning');
            return;
        }

        if (!apiKey.startsWith('sk-or-')) {
            this.updateStatus('Invalid key format (should start with sk-or-)', 'error');
            return;
        }

        try {
            await this.updateConfig({
                'openrouter.api_key': apiKey
            });

            this.updateStatus('API key saved successfully', 'success');
            keyInput.value = '';
            keyInput.placeholder = apiKey.substring(0, 10) + '...(configured)';
        } catch (error: unknown) {
            handleError(error, 'Failed to save API key', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to save API key', 'error');
        }
    }

    public toggle(): void {
        this.window.toggle();
    }

    public show(): void {
        this.window.show();
    }

    public hide(): void {
        this.window.hide();
    }
}

// Initialize and export
const aiProviderPanel = new AIProviderPanel();

export function toggleAIProvider(): void {
    aiProviderPanel.toggle();
}

export {};
