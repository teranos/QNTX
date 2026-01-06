/**
 * AI Provider Panel - Actor/Agent Configuration
 *
 * Shows AI inference provider selection when clicking ⌬ (by/actor) in the symbol palette.
 * Allows switching between OpenRouter (cloud), Anthropic (Claude), and Ollama (local) providers.
 */

import { BasePanel } from './base-panel.ts';
import { apiFetch } from './api.ts';
import { BY } from '@generated/sym.js';

interface ConfigResponse {
    config_file?: string;
    settings: Array<{
        key: string;
        value: unknown;
        source: string;
    }>;
}

type ProviderType = 'openrouter' | 'anthropic' | 'ollama';

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
                <button class="ai-provider-close" aria-label="Close">✕</button>
            </div>
            <div class="ai-provider-content">
                <div class="config-toggle-header">
                    <span class="config-toggle-title">AI Inference Provider</span>
                    <span id="ai-provider-status" class="config-toggle-status"></span>
                </div>
                <div class="config-toggle-control">
                    <button id="provider-openrouter-btn" class="provider-btn">
                        <span class="provider-icon">☁️</span>
                        <span class="provider-name">OpenRouter</span>
                        <span class="provider-detail">100+ Models</span>
                    </button>
                    <button id="provider-anthropic-btn" class="provider-btn">
                        <span class="provider-icon">🧠</span>
                        <span class="provider-name">Anthropic</span>
                        <span class="provider-detail">Claude Direct</span>
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
                <div id="anthropic-config" class="provider-config hidden">
                    <div class="api-key-section">
                        <label for="anthropic-api-key">Anthropic API Key:</label>
                        <div class="api-key-input-group">
                            <input
                                type="password"
                                id="anthropic-api-key"
                                class="api-key-input"
                                placeholder="sk-ant-..."
                                autocomplete="off"
                            />
                            <button id="anthropic-key-toggle" class="api-key-toggle" title="Show/Hide">👁</button>
                            <button id="anthropic-key-save" class="api-key-save" title="Save">💾</button>
                        </div>
                        <div class="api-key-hint">Get your key from <a href="https://console.anthropic.com/settings/keys" target="_blank" rel="noopener">console.anthropic.com</a></div>
                    </div>
                    <div class="model-selector">
                        <label for="anthropic-model-select">Model:</label>
                        <select id="anthropic-model-select">
                            <option value="claude-sonnet-4-20250514">Claude Sonnet 4 (balanced)</option>
                            <option value="claude-opus-4-20250514">Claude Opus 4 (powerful)</option>
                            <option value="claude-3-5-haiku-latest">Claude 3.5 Haiku (fast)</option>
                        </select>
                    </div>
                </div>
                <div id="ollama-config" class="provider-config hidden">
                    <div class="model-selector">
                        <label for="ollama-model-select">Model:</label>
                        <select id="ollama-model-select">
                            <option value="llama3.2:3b">llama3.2:3b (3B, very fast)</option>
                            <option value="mistral">mistral (7B, fast, general)</option>
                            <option value="qwen2.5-coder:7b">qwen2.5-coder:7b (code/technical)</option>
                            <option value="deepseek-r1:7b">deepseek-r1:7b (reasoning)</option>
                        </select>
                    </div>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Close button
        const closeBtn = this.$('.ai-provider-close');
        closeBtn?.addEventListener('click', () => this.hide());

        // AI Provider toggle buttons
        const openrouterBtn = this.$('#provider-openrouter-btn');
        const anthropicBtn = this.$('#provider-anthropic-btn');
        const ollamaBtn = this.$('#provider-ollama-btn');
        const ollamaModelSelect = this.$<HTMLSelectElement>('#ollama-model-select');
        const anthropicModelSelect = this.$<HTMLSelectElement>('#anthropic-model-select');

        openrouterBtn?.addEventListener('click', () => this.switchToOpenRouter());
        anthropicBtn?.addEventListener('click', () => this.switchToAnthropic());
        ollamaBtn?.addEventListener('click', () => this.switchToOllama());

        ollamaModelSelect?.addEventListener('change', (e: Event) => {
            const target = e.target as HTMLSelectElement;
            this.updateOllamaModel(target.value);
        });

        anthropicModelSelect?.addEventListener('change', (e: Event) => {
            const target = e.target as HTMLSelectElement;
            this.updateAnthropicModel(target.value);
        });

        // OpenRouter API key handling
        this.setupApiKeyHandlers('openrouter', 'sk-or-');

        // Anthropic API key handling
        this.setupApiKeyHandlers('anthropic', 'sk-ant-');
    }

    private setupApiKeyHandlers(provider: 'openrouter' | 'anthropic', prefix: string): void {
        const keyInput = this.$<HTMLInputElement>(`#${provider}-api-key`);
        const keyToggle = this.$(`#${provider}-key-toggle`);
        const keySave = this.$(`#${provider}-key-save`);

        keyToggle?.addEventListener('click', () => {
            if (keyInput) {
                keyInput.type = keyInput.type === 'password' ? 'text' : 'password';
                if (keyToggle.textContent) {
                    keyToggle.textContent = keyInput.type === 'password' ? '👁' : '👁‍🗨';
                }
            }
        });

        keySave?.addEventListener('click', () => this.saveApiKey(provider, prefix));
        keyInput?.addEventListener('keypress', (e: KeyboardEvent) => {
            if (e.key === 'Enter') {
                this.saveApiKey(provider, prefix);
            }
        });
    }

    protected async onShow(): Promise<void> {
        await this.fetchConfig();
        this.setupProviderButtons();
        await this.loadApiKeys();
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
            console.error('[AI Provider Panel] Failed to fetch config:', error);
        }
    }

    private setupProviderButtons(): void {
        if (!this.appConfig?.settings) return;

        // Determine active provider based on config
        // Priority: local_inference.enabled → anthropic.api_key set → openrouter (default)
        const localInferenceSetting = this.appConfig.settings.find(s => s.key === 'local_inference.enabled');
        const isOllamaEnabled = localInferenceSetting?.value === true;

        const anthropicKeySetting = this.appConfig.settings.find(s => s.key === 'anthropic.api_key');
        const hasAnthropicKey = !!(anthropicKeySetting?.value);

        let activeProvider: ProviderType = 'openrouter';
        if (isOllamaEnabled) {
            activeProvider = 'ollama';
        } else if (hasAnthropicKey) {
            activeProvider = 'anthropic';
        }

        // Update UI
        this.updateProviderUI(activeProvider);

        // Update model dropdowns
        const ollamaModelSetting = this.appConfig.settings.find(s => s.key === 'local_inference.model');
        const ollamaModel = (ollamaModelSetting?.value as string) || 'llama3.2:3b';
        const ollamaSelect = this.$<HTMLSelectElement>('#ollama-model-select');
        if (ollamaSelect) {
            ollamaSelect.value = ollamaModel;
        }

        const anthropicModelSetting = this.appConfig.settings.find(s => s.key === 'anthropic.model');
        const anthropicModel = (anthropicModelSetting?.value as string) || 'claude-sonnet-4-20250514';
        const anthropicSelect = this.$<HTMLSelectElement>('#anthropic-model-select');
        if (anthropicSelect) {
            anthropicSelect.value = anthropicModel;
        }
    }

    private async switchToOpenRouter(): Promise<void> {
        console.log('[AI Provider Panel] Switching to OpenRouter');

        this.updateProviderUI('openrouter');

        try {
            await this.updateConfig({
                'local_inference.enabled': false
            });

            this.updateStatus('Using OpenRouter (cloud API)', 'success');
        } catch (error) {
            console.error('[AI Provider Panel] Failed to switch to OpenRouter:', error);
            this.updateStatus('Failed to update config', 'error');
        }
    }

    private async switchToAnthropic(): Promise<void> {
        console.log('[AI Provider Panel] Switching to Anthropic');

        this.updateProviderUI('anthropic');

        const modelSelect = this.$<HTMLSelectElement>('#anthropic-model-select');
        const model = modelSelect ? modelSelect.value : 'claude-sonnet-4-20250514';

        try {
            await this.updateConfig({
                'local_inference.enabled': false,
                'anthropic.model': model
            });

            this.updateStatus(`Using Anthropic (${model})`, 'success');
        } catch (error) {
            console.error('[AI Provider Panel] Failed to switch to Anthropic:', error);
            this.updateStatus('Failed to update config', 'error');
        }
    }

    private async switchToOllama(): Promise<void> {
        console.log('[AI Provider Panel] Switching to Ollama');

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
            console.error('[AI Provider Panel] Failed to switch to Ollama:', error);
            this.updateStatus('Failed to update config - is Ollama running?', 'error');
        }
    }

    private async updateOllamaModel(model: string): Promise<void> {
        console.log('[AI Provider Panel] Updating Ollama model to:', model);

        try {
            await this.updateConfig({
                'local_inference.model': model
            });

            this.updateStatus(`Using Ollama (${model})`, 'success');
        } catch (error) {
            console.error('[AI Provider Panel] Failed to update Ollama model:', error);
            this.updateStatus('Failed to update model', 'error');
        }
    }

    private async updateAnthropicModel(model: string): Promise<void> {
        console.log('[AI Provider Panel] Updating Anthropic model to:', model);

        try {
            await this.updateConfig({
                'anthropic.model': model
            });

            this.updateStatus(`Using Anthropic (${model})`, 'success');
        } catch (error) {
            console.error('[AI Provider Panel] Failed to update Anthropic model:', error);
            this.updateStatus('Failed to update model', 'error');
        }
    }

    private updateProviderUI(provider: ProviderType): void {
        const openrouterBtn = this.$('#provider-openrouter-btn');
        const anthropicBtn = this.$('#provider-anthropic-btn');
        const ollamaBtn = this.$('#provider-ollama-btn');
        const openrouterConfig = this.$('#openrouter-config');
        const anthropicConfig = this.$('#anthropic-config');
        const ollamaConfig = this.$('#ollama-config');

        // Reset all buttons
        openrouterBtn?.classList.remove('active');
        anthropicBtn?.classList.remove('active');
        ollamaBtn?.classList.remove('active');

        // Hide all configs
        openrouterConfig?.classList.add('hidden');
        anthropicConfig?.classList.add('hidden');
        ollamaConfig?.classList.add('hidden');

        // Activate selected provider
        switch (provider) {
            case 'openrouter':
                openrouterBtn?.classList.add('active');
                openrouterConfig?.classList.remove('hidden');
                break;
            case 'anthropic':
                anthropicBtn?.classList.add('active');
                anthropicConfig?.classList.remove('hidden');
                break;
            case 'ollama':
                ollamaBtn?.classList.add('active');
                ollamaConfig?.classList.remove('hidden');
                break;
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

    private async loadApiKeys(): Promise<void> {
        if (!this.appConfig?.settings) return;

        // Load OpenRouter key
        const openrouterKeySetting = this.appConfig.settings.find(s => s.key === 'openrouter.api_key');
        const openrouterInput = this.$<HTMLInputElement>('#openrouter-api-key');
        if (openrouterInput && openrouterKeySetting?.value) {
            const keyValue = openrouterKeySetting.value as string;
            if (keyValue.length > 10) {
                openrouterInput.placeholder = keyValue.substring(0, 10) + '...(configured)';
            }
        }

        // Load Anthropic key
        const anthropicKeySetting = this.appConfig.settings.find(s => s.key === 'anthropic.api_key');
        const anthropicInput = this.$<HTMLInputElement>('#anthropic-api-key');
        if (anthropicInput && anthropicKeySetting?.value) {
            const keyValue = anthropicKeySetting.value as string;
            if (keyValue.length > 10) {
                anthropicInput.placeholder = keyValue.substring(0, 10) + '...(configured)';
            }
        }
    }

    private async saveApiKey(provider: 'openrouter' | 'anthropic', expectedPrefix: string): Promise<void> {
        const keyInput = this.$<HTMLInputElement>(`#${provider}-api-key`);
        if (!keyInput) return;

        const apiKey = keyInput.value.trim();
        if (!apiKey) {
            this.updateStatus('Please enter an API key', 'warning');
            return;
        }

        // Basic validation for key format
        if (!apiKey.startsWith(expectedPrefix)) {
            this.updateStatus(`Invalid key format (should start with ${expectedPrefix})`, 'error');
            return;
        }

        const configKey = provider === 'openrouter' ? 'openrouter.api_key' : 'anthropic.api_key';

        try {
            await this.updateConfig({
                [configKey]: apiKey
            });

            this.updateStatus('API key saved successfully', 'success');
            keyInput.value = ''; // Clear the input
            keyInput.placeholder = apiKey.substring(0, 10) + '...(configured)';
        } catch (error) {
            console.error(`[AI Provider Panel] Failed to save ${provider} API key:`, error);
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

            console.log('[AI Provider Panel] Ollama not available:', error);
        }
    }
}

// Initialize and export
const aiProviderPanel = new AIProviderPanel();

export function toggleAIProvider(): void {
    aiProviderPanel.toggle();
}

export {};
