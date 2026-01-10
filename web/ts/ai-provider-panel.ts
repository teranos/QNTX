/**
 * AI Provider Panel - Actor/Agent Configuration
 *
 * Shows AI inference provider selection when clicking ⌬ (by/actor) in the symbol palette.
 * Allows switching between OpenRouter (cloud) and Ollama (local) providers.
 */

import { BasePanel } from './base-panel.ts';
import { apiFetch } from './api.ts';
import { BY } from '@generated/sym.js';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';

interface ConfigResponse {
    config_file?: string;
    settings: Array<{
        key: string;
        value: unknown;
        source: string;
    }>;
}

class AIProviderPanel extends BasePanel {
    private appConfig: ConfigResponse | null = null;
    private ollamaAvailable: boolean = false;

    constructor() {
        super({
            id: 'ai-provider-panel',
            classes: ['ai-provider-panel'],
            useOverlay: false,  // No overlay, uses click-outside
            closeOnEscape: true,
            insertAfter: '#symbolPalette'
        });
    }

    protected getTemplate(): string {
        return `
            <div class="ai-provider-header">
                <h3 class="ai-provider-title">${BY} Actor / AI Provider</h3>
                <button class="panel-close" aria-label="Close">✕</button>
            </div>
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
                    <button id="provider-ollama-btn" class="provider-btn">
                        <span class="provider-icon">🖥️</span>
                        <span class="provider-name">Ollama</span>
                        <span class="provider-detail" id="ollama-status">Local GPU/CPU</span>
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
                            <button id="openrouter-key-toggle" class="api-key-toggle" title="Show/Hide">👁</button>
                            <button id="openrouter-key-save" class="api-key-save" title="Save">💾</button>
                        </div>
                        <div class="api-key-hint">Get your key from <a href="https://openrouter.ai/keys" target="_blank" rel="noopener">openrouter.ai/keys</a></div>
                    </div>
                </div>
                <div id="ollama-model-selector" class="ollama-model-selector provider-config hidden">
                    <label for="ollama-model-select">Model:</label>
                    <select id="ollama-model-select">
                        <option value="llama3.2:3b">llama3.2:3b (3B, very fast)</option>
                        <option value="mistral">mistral (7B, fast, general)</option>
                        <option value="qwen2.5-coder:7b">qwen2.5-coder:7b (code/technical)</option>
                        <option value="deepseek-r1:7b">deepseek-r1:7b (reasoning)</option>
                    </select>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Close button is now handled automatically by BasePanel (.panel-close)

        // AI Provider toggle buttons
        const openrouterBtn = this.$('#provider-openrouter-btn');
        const ollamaBtn = this.$('#provider-ollama-btn');
        const modelSelect = this.$<HTMLSelectElement>('#ollama-model-select');

        openrouterBtn?.addEventListener('click', () => this.switchToOpenRouter());
        ollamaBtn?.addEventListener('click', () => this.switchToOllama());
        modelSelect?.addEventListener('change', (e: Event) => {
            const target = e.target as HTMLSelectElement;
            this.updateOllamaModel(target.value);
        });

        // OpenRouter API key handling
        const keyInput = this.$<HTMLInputElement>('#openrouter-api-key');
        const keyToggle = this.$('#openrouter-key-toggle');
        const keySave = this.$('#openrouter-key-save');

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

    protected async onShow(): Promise<void> {
        await this.fetchConfig();
        this.setupProviderButtons();
        await this.loadOpenRouterKey();
        await this.checkOllamaStatus();
    }

    private async fetchConfig(): Promise<void> {
        try {
            const response = await apiFetch('/api/config?introspection=true');
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            this.appConfig = await response.json();
        } catch (error) {
            handleError(error, 'Failed to fetch config', { context: SEG.ACTOR, silent: true });
        }
    }

    private setupProviderButtons(): void {
        if (!this.appConfig?.settings) return;

        // Find local_inference.enabled setting
        const localInferenceSetting = this.appConfig.settings.find(s => s.key === 'local_inference.enabled');
        const isOllamaEnabled = localInferenceSetting?.value === true;

        // Find local_inference.model setting
        const modelSetting = this.appConfig.settings.find(s => s.key === 'local_inference.model');
        const effectiveModel = (modelSetting?.value as string) || 'llama3.2:3b';

        // Update UI
        this.updateProviderUI(isOllamaEnabled ? 'ollama' : 'openrouter');

        // Update model dropdown
        const modelSelect = this.$<HTMLSelectElement>('#ollama-model-select');
        if (modelSelect) {
            modelSelect.value = effectiveModel;
        }
    }

    private async switchToOpenRouter(): Promise<void> {
        log.debug(SEG.ACTOR, 'Switching to OpenRouter');

        this.updateProviderUI('openrouter');

        try {
            await this.updateConfig({
                'local_inference.enabled': false
            });

            this.updateStatus('Using OpenRouter (cloud API)', 'success');
        } catch (error) {
            handleError(error, 'Failed to switch to OpenRouter', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to update config', 'error');
        }
    }

    private async switchToOllama(): Promise<void> {
        log.debug(SEG.ACTOR, 'Switching to Ollama');

        this.updateProviderUI('ollama');

        const modelSelect = this.$<HTMLSelectElement>('#ollama-model-select');
        const model = modelSelect ? modelSelect.value : 'llama3.2:3b';

        try {
            await this.updateConfig({
                'local_inference.enabled': true,
                'local_inference.model': model
            });

            this.updateStatus(`Using Ollama (${model})`, 'success');
        } catch (error) {
            handleError(error, 'Failed to switch to Ollama', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to update config - is Ollama running?', 'error');
        }
    }

    private async updateOllamaModel(model: string): Promise<void> {
        log.debug(SEG.ACTOR, 'Updating Ollama model to:', model);

        try {
            await this.updateConfig({
                'local_inference.model': model
            });

            this.updateStatus(`Using Ollama (${model})`, 'success');
        } catch (error) {
            handleError(error, 'Failed to update Ollama model', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to update model', 'error');
        }
    }

    private updateProviderUI(provider: 'openrouter' | 'ollama'): void {
        const openrouterBtn = this.$('#provider-openrouter-btn');
        const ollamaBtn = this.$('#provider-ollama-btn');
        const modelSelector = this.$('#ollama-model-selector');
        const openrouterConfig = this.$('#openrouter-config');

        if (provider === 'openrouter') {
            openrouterBtn?.classList.add('active');
            ollamaBtn?.classList.remove('active');
            modelSelector?.classList.add('u-hidden');
            openrouterConfig?.classList.remove('u-hidden');
        } else {
            openrouterBtn?.classList.remove('active');
            ollamaBtn?.classList.add('active');
            modelSelector?.classList.remove('u-hidden');
            openrouterConfig?.classList.add('u-hidden');
        }
    }

    private updateStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        const statusEl = this.$('#ai-provider-status');
        if (statusEl) {
            statusEl.textContent = message;
            statusEl.className = `config-toggle-status status-${type}`;

            // Don't auto-clear warning messages
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
            body: JSON.stringify(updates),
        });

        if (!response.ok) {
            throw new Error(`Failed to update config: ${response.statusText}`);
        }

        return response.json();
    }

    private async loadOpenRouterKey(): Promise<void> {
        if (!this.appConfig?.settings) return;

        // Find the OpenRouter API key setting
        const keySetting = this.appConfig.settings.find(s => s.key === 'openrouter.api_key');
        const keyInput = this.$<HTMLInputElement>('#openrouter-api-key');

        if (keyInput && keySetting?.value) {
            const keyValue = keySetting.value as string;
            // Show masked version of key (first 10 chars + ...)
            if (keyValue.length > 10) {
                keyInput.placeholder = keyValue.substring(0, 10) + '...(configured)';
            }
        }
    }

    private async saveOpenRouterKey(): Promise<void> {
        const keyInput = this.$<HTMLInputElement>('#openrouter-api-key');
        if (!keyInput) return;

        const apiKey = keyInput.value.trim();
        if (!apiKey) {
            this.updateStatus('Please enter an API key', 'warning');
            return;
        }

        // Basic validation for OpenRouter key format
        if (!apiKey.startsWith('sk-or-')) {
            this.updateStatus('Invalid key format (should start with sk-or-)', 'error');
            return;
        }

        try {
            await this.updateConfig({
                'openrouter.api_key': apiKey
            });

            this.updateStatus('API key saved successfully', 'success');
            keyInput.value = ''; // Clear the input
            keyInput.placeholder = apiKey.substring(0, 10) + '...(configured)';
        } catch (error) {
            handleError(error, 'Failed to save API key', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to save API key', 'error');
        }
    }

    private async checkOllamaStatus(): Promise<void> {
        try {
            // Try to connect to Ollama API
            const response = await fetch('http://localhost:11434/api/tags', {
                method: 'GET',
                signal: AbortSignal.timeout(2000), // 2 second timeout
            });

            this.ollamaAvailable = response.ok;

            const ollamaBtn = this.$('#provider-ollama-btn');
            const ollamaStatus = this.$('#ollama-status');

            if (this.ollamaAvailable) {
                ollamaBtn?.classList.remove('ollama-unavailable');
                if (ollamaStatus) {
                    ollamaStatus.textContent = 'Local GPU/CPU';
                }
            } else {
                ollamaBtn?.classList.add('ollama-unavailable');
                if (ollamaStatus) {
                    ollamaStatus.textContent = 'Offline';
                }
            }
        } catch (error) {
            // Ollama is not running or unreachable
            this.ollamaAvailable = false;

            const ollamaBtn = this.$('#provider-ollama-btn');
            const ollamaStatus = this.$('#ollama-status');

            ollamaBtn?.classList.add('ollama-unavailable');
            if (ollamaStatus) {
                ollamaStatus.textContent = 'Offline';
            }

            handleError(error, 'Ollama not available', { context: SEG.ACTOR, silent: true });
        }
    }
}

// Initialize and export
const aiProviderPanel = new AIProviderPanel();

export function toggleAIProvider(): void {
    aiProviderPanel.toggle();
}

export {};
