/**
 * AI Provider Panel - Actor/Agent Configuration
 *
 * Shows AI inference provider selection when clicking ⌬ (by/actor) in the symbol palette.
 * Allows switching between OpenRouter (cloud) and Ollama (local) providers.
 */

import { apiFetch } from './api.ts';
import { BY } from '@types/sym.js';

interface ConfigResponse {
    config_file?: string;
    settings: Array<{
        key: string;
        value: unknown;
        source: string;
    }>;
}

class AIProviderPanel {
    private panel: HTMLElement | null = null;
    private isVisible: boolean = false;
    private config: ConfigResponse | null = null;

    constructor() {
        this.initialize();
    }

    initialize(): void {
        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'ai-provider-panel';
        this.panel.className = 'ai-provider-panel hidden';
        this.panel.innerHTML = this.getTemplate();

        // Insert after symbol palette (same as IX panel)
        const symbolPalette = document.getElementById('symbolPalette');
        if (symbolPalette && symbolPalette.parentNode) {
            symbolPalette.parentNode.insertBefore(this.panel, symbolPalette.nextSibling);
        }

        // Click outside to close
        document.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;
            if (this.panel && this.isVisible && !this.panel.contains(target) && !target.closest('.palette-cell[data-cmd="by"]')) {
                this.hide();
            }
        });

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
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
                    <button id="provider-openrouter-btn" class="provider-btn active">
                        <span class="provider-icon">☁️</span>
                        <span class="provider-name">OpenRouter</span>
                        <span class="provider-detail">Cloud API</span>
                    </button>
                    <button id="provider-ollama-btn" class="provider-btn">
                        <span class="provider-icon">🖥️</span>
                        <span class="provider-name">Ollama</span>
                        <span class="provider-detail">Local GPU/CPU</span>
                    </button>
                </div>
                <div id="ollama-model-selector" class="ollama-model-selector hidden">
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

    setupEventListeners(): void {
        if (!this.panel) return;

        // Close button
        const closeBtn = this.panel.querySelector('.ai-provider-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.hide());
        }

        // AI Provider toggle buttons
        const openrouterBtn = this.panel.querySelector('#provider-openrouter-btn');
        const ollamaBtn = this.panel.querySelector('#provider-ollama-btn');
        const modelSelect = this.panel.querySelector('#ollama-model-select') as HTMLSelectElement | null;

        if (openrouterBtn) {
            openrouterBtn.addEventListener('click', () => this.switchToOpenRouter());
        }

        if (ollamaBtn) {
            ollamaBtn.addEventListener('click', () => this.switchToOllama());
        }

        if (modelSelect) {
            modelSelect.addEventListener('change', (e: Event) => {
                const target = e.target as HTMLSelectElement;
                this.updateOllamaModel(target.value);
            });
        }
    }

    async show(): Promise<void> {
        if (!this.panel) return;

        this.isVisible = true;
        this.panel.classList.remove('hidden');
        this.panel.classList.add('visible');

        // Fetch current config
        await this.fetchConfig();
        this.setupProviderButtons();
    }

    hide(): void {
        if (!this.panel) return;

        this.isVisible = false;
        this.panel.classList.remove('visible');
        this.panel.classList.add('hidden');
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
            const response = await apiFetch('/api/config?introspection=true');
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            this.config = await response.json();
        } catch (error) {
            console.error('[AI Provider Panel] Failed to fetch config:', error);
        }
    }

    setupProviderButtons(): void {
        if (!this.panel || !this.config || !this.config.settings) return;

        // Find local_inference.enabled setting
        const localInferenceSetting = this.config.settings.find(s => s.key === 'local_inference.enabled');
        const isOllamaEnabled = localInferenceSetting?.value === true;

        // Find local_inference.model setting
        const modelSetting = this.config.settings.find(s => s.key === 'local_inference.model');
        const effectiveModel = (modelSetting?.value as string) || 'llama3.2:3b';

        // Update UI
        this.updateProviderUI(isOllamaEnabled ? 'ollama' : 'openrouter');

        // Update model dropdown
        const modelSelect = this.panel.querySelector('#ollama-model-select') as HTMLSelectElement | null;
        if (modelSelect) {
            modelSelect.value = effectiveModel;
        }
    }

    async switchToOpenRouter(): Promise<void> {
        console.log('[AI Provider Panel] Switching to OpenRouter');

        // Update UI immediately
        this.updateProviderUI('openrouter');

        // Send config update to backend
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

    async switchToOllama(): Promise<void> {
        if (!this.panel) return;

        console.log('[AI Provider Panel] Switching to Ollama');

        // Update UI immediately
        this.updateProviderUI('ollama');

        // Get selected model
        const modelSelect = this.panel.querySelector('#ollama-model-select') as HTMLSelectElement | null;
        const model = modelSelect ? modelSelect.value : 'llama3.2:3b';

        // Send config update to backend
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

    async updateOllamaModel(model: string): Promise<void> {
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

    updateProviderUI(provider: 'openrouter' | 'ollama'): void {
        if (!this.panel) return;

        const openrouterBtn = this.panel.querySelector('#provider-openrouter-btn');
        const ollamaBtn = this.panel.querySelector('#provider-ollama-btn');
        const modelSelector = this.panel.querySelector('#ollama-model-selector');

        if (provider === 'openrouter') {
            openrouterBtn?.classList.add('active');
            ollamaBtn?.classList.remove('active');
            modelSelector?.classList.add('hidden');
        } else {
            openrouterBtn?.classList.remove('active');
            ollamaBtn?.classList.add('active');
            modelSelector?.classList.remove('hidden');
        }
    }

    updateStatus(message: string, type: 'success' | 'error' | 'warning'): void {
        if (!this.panel) return;

        const statusEl = this.panel.querySelector('#ai-provider-status');
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

    async updateConfig(updates: Record<string, unknown>): Promise<unknown> {
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
}

// Initialize and export
const aiProviderPanel = new AIProviderPanel();

export function toggleAIProvider(): void {
    aiProviderPanel.toggle();
}

export {};
