/**
 * AI Provider Panel - Actor/Agent Configuration
 *
 * Shows AI inference provider selection when clicking ⌬ (by/actor) in the symbol palette.
 * Allows switching between OpenRouter (cloud) and Ollama (local) providers.
 */

import { Window } from './components/window.ts';
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

class AIProviderPanel {
    private window: Window;
    private appConfig: ConfigResponse | null = null;
    private ollamaAvailable: boolean = false;

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
                    <label for="ollama-model-select">Ollama Model:</label>
                    <select id="ollama-model-select">
                        <option value="llama3.2:3b">llama3.2:3b (3B, very fast)</option>
                        <option value="mistral">mistral (7B, fast, general)</option>
                        <option value="qwen2.5-coder:7b">qwen2.5-coder:7b (code/technical)</option>
                        <option value="deepseek-r1:7b">deepseek-r1:7b (reasoning)</option>
                    </select>
                </div>
                <div id="onnx-model-config" class="provider-config hidden">
                    <label for="onnx-model-path">ONNX Model Path (VidStream):</label>
                    <div class="api-key-input-group">
                        <input
                            type="text"
                            id="onnx-model-path"
                            class="api-key-input"
                            placeholder="ats/vidstream/models/yolo11n.onnx"
                            autocomplete="off"
                        />
                        <button id="onnx-model-save" class="api-key-save" title="Save">💾</button>
                    </div>
                </div>
            </div>
        `;

        this.window.setContent(content);
        this.setupEventListeners();
    }

    private setupEventListeners(): void {
        const windowEl = this.window.getElement();

        // AI Provider toggle buttons
        const openrouterBtn = windowEl.querySelector('#provider-openrouter-btn');
        const ollamaBtn = windowEl.querySelector('#provider-ollama-btn');
        const modelSelect = windowEl.querySelector<HTMLSelectElement>('#ollama-model-select');

        openrouterBtn?.addEventListener('click', () => this.switchToOpenRouter());
        ollamaBtn?.addEventListener('click', () => this.switchToOllama());
        modelSelect?.addEventListener('change', (e: Event) => {
            const target = e.target as HTMLSelectElement;
            this.updateOllamaModel(target.value);
        });

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

        // ONNX model path handling
        const onnxPathInput = windowEl.querySelector<HTMLInputElement>('#onnx-model-path');
        const onnxSave = windowEl.querySelector('#onnx-model-save');

        onnxSave?.addEventListener('click', () => this.saveONNXModelPath());
        onnxPathInput?.addEventListener('keypress', (e: KeyboardEvent) => {
            if (e.key === 'Enter') {
                this.saveONNXModelPath();
            }
        });
    }

    private async onShow(): Promise<void> {
        await this.fetchConfig();
        this.setupProviderButtons();
        await this.loadOpenRouterKey();
        await this.loadONNXModelPath();
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
        const windowEl = this.window.getElement();
        const modelSelect = windowEl.querySelector<HTMLSelectElement>('#ollama-model-select');
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

        const windowEl = this.window.getElement();
        const modelSelect = windowEl.querySelector<HTMLSelectElement>('#ollama-model-select');
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
        const windowEl = this.window.getElement();
        const openrouterBtn = windowEl.querySelector('#provider-openrouter-btn');
        const ollamaBtn = windowEl.querySelector('#provider-ollama-btn');
        const modelSelector = windowEl.querySelector('#ollama-model-selector');
        const openrouterConfig = windowEl.querySelector('#openrouter-config');
        const onnxConfig = windowEl.querySelector('#onnx-model-config');

        if (provider === 'openrouter') {
            openrouterBtn?.classList.add('active');
            ollamaBtn?.classList.remove('active');
            modelSelector?.classList.add('u-hidden');
            openrouterConfig?.classList.remove('u-hidden');
            onnxConfig?.classList.add('u-hidden');
        } else {
            openrouterBtn?.classList.remove('active');
            ollamaBtn?.classList.add('active');
            modelSelector?.classList.remove('u-hidden');
            openrouterConfig?.classList.add('u-hidden');
            onnxConfig?.classList.remove('u-hidden');
        }
    }

    private updateStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        const windowEl = this.window.getElement();
        const statusEl = windowEl.querySelector('#ai-provider-status');
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
        const windowEl = this.window.getElement();
        const keyInput = windowEl.querySelector<HTMLInputElement>('#openrouter-api-key');

        if (keyInput && keySetting?.value) {
            const keyValue = keySetting.value as string;
            // Show masked version of key (first 10 chars + ...)
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

    private async loadONNXModelPath(): Promise<void> {
        if (!this.appConfig?.settings) return;

        const pathSetting = this.appConfig.settings.find(s => s.key === 'local_inference.onnx_model_path');
        const windowEl = this.window.getElement();
        const pathInput = windowEl.querySelector<HTMLInputElement>('#onnx-model-path');

        if (pathInput && pathSetting?.value) {
            pathInput.value = pathSetting.value as string;
        }
    }

    private async saveONNXModelPath(): Promise<void> {
        const windowEl = this.window.getElement();
        const pathInput = windowEl.querySelector<HTMLInputElement>('#onnx-model-path');
        if (!pathInput) return;

        const path = pathInput.value.trim();
        if (!path) {
            this.updateStatus('Please enter a model path', 'warning');
            return;
        }

        try {
            await this.updateConfig({
                'local_inference.onnx_model_path': path
            });

            this.updateStatus('ONNX model path saved', 'success');
        } catch (error) {
            handleError(error, 'Failed to save ONNX model path', { context: SEG.ACTOR, silent: true });
            this.updateStatus('Failed to save model path', 'error');
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

            const windowEl = this.window.getElement();
            const ollamaBtn = windowEl.querySelector('#provider-ollama-btn');
            const ollamaStatus = windowEl.querySelector('#ollama-status');

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

            const windowEl = this.window.getElement();
            const ollamaBtn = windowEl.querySelector('#provider-ollama-btn');
            const ollamaStatus = windowEl.querySelector('#ollama-status');

            ollamaBtn?.classList.add('ollama-unavailable');
            if (ollamaStatus) {
                ollamaStatus.textContent = 'Offline';
            }

            handleError(error, 'Ollama not available', { context: SEG.ACTOR, silent: true });
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
