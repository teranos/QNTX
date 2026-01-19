/**
 * SO Panel - Prompt history (⟶)
 *
 * Displays prompt history when clicking so (⟶) in the symbol palette:
 * - Shows saved prompts with their versions
 * - Provides quick access to open prompts in the editor
 * - Shows active prompt executions
 * - Allows creating new prompts via + button
 *
 * Similar to hixtory-panel but for prompts instead of IX operations
 */

import { BasePanel } from './base-panel.ts';
import { SO } from '@generated/sym.js';
import { formatRelativeTime } from './html-utils.ts';
import { showPromptEditor, openPromptInEditor } from './prompt-editor-window.ts';
import { apiFetch } from './api.ts';
import { log, SEG } from './logger';

interface StoredPrompt {
    id: string;
    name: string;
    template: string;
    system_prompt?: string;
    ax_pattern?: string;
    provider?: string;
    model?: string;
    created_by: string;
    created_at: string;
    version: number;
}

interface PromptListResponse {
    prompts: StoredPrompt[];
    count: number;
}

class SoPanel extends BasePanel {
    private prompts: Map<string, StoredPrompt> = new Map();

    constructor() {
        super({
            id: 'so-panel',
            classes: ['so-panel'],
            useOverlay: false,
            closeOnEscape: true,
            insertAfter: '#symbolPalette'
        });
    }

    protected getTemplate(): string {
        return `
            <div class="so-panel-header">
                <h3 class="so-panel-title">${SO} Prompts <span class="prompt-count">(<span id="prompt-count">0</span>)</span></h3>
                <button class="panel-close" aria-label="Close">✕</button>
            </div>
            <div class="so-panel-content" id="so-panel-content">
                <div class="panel-empty so-panel-empty">
                    <p>No prompts yet</p>
                    <p class="so-panel-hint">Create a prompt to get started</p>
                </div>
            </div>
            <div class="so-panel-footer">
                <button class="so-new-prompt-btn" id="new-prompt-btn">
                    <span class="btn-icon">+</span>
                    <span class="btn-text">New Prompt</span>
                </button>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // New prompt button
        const newBtn = this.$('#new-prompt-btn');
        if (newBtn) {
            newBtn.addEventListener('click', () => {
                this.hide();
                showPromptEditor();
            });
        }
    }

    protected async onShow(): Promise<void> {
        this.showLoading('Loading prompts...');
        await this.fetchPrompts();
        this.hideLoading();
        this.render();
    }

    /**
     * Fetch prompts from /api/prompt/list
     */
    private async fetchPrompts(): Promise<void> {
        try {
            const response = await apiFetch('/api/prompt/list');
            if (!response.ok) {
                log.error(SEG.SO, 'Failed to fetch prompts:', response.statusText);
                return;
            }

            const data: PromptListResponse = await response.json();
            const prompts = data.prompts || [];

            this.prompts.clear();
            prompts.forEach((prompt: StoredPrompt) => {
                // Use name as key to dedupe versions (latest first from API)
                if (!this.prompts.has(prompt.name)) {
                    this.prompts.set(prompt.name, prompt);
                }
            });

            log.debug(SEG.SO, `Loaded ${prompts.length} prompts from API`);
        } catch (error) {
            log.error(SEG.SO, 'Error fetching prompts:', error);
        }
    }

    /**
     * Render prompt list
     */
    private render(): void {
        const content = this.$('#so-panel-content');
        const countSpan = this.$('#prompt-count');

        if (!content) return;

        if (this.prompts.size === 0) {
            content.innerHTML = '';
            content.appendChild(
                this.createEmptyState('No prompts yet', 'Create a prompt to get started')
            );
            content.firstElementChild?.classList.add('so-panel-empty');

            if (countSpan) countSpan.textContent = '0';
            return;
        }

        // Sort by created_at descending
        const sortedPrompts = Array.from(this.prompts.values())
            .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

        if (countSpan) {
            countSpan.textContent = sortedPrompts.length.toString();
        }

        content.innerHTML = '';

        sortedPrompts.forEach(prompt => {
            const item = this.renderPromptItem(prompt);
            content.appendChild(item);
        });
    }

    /**
     * Render a single prompt item
     */
    private renderPromptItem(prompt: StoredPrompt): HTMLElement {
        const timeAgo = formatRelativeTime(prompt.created_at);
        const absoluteTime = new Date(prompt.created_at).toLocaleString();

        const item = document.createElement('div');
        item.className = 'so-prompt-item';
        item.dataset.promptId = prompt.id;
        item.dataset.promptName = prompt.name;

        // Name with tooltip
        const nameDiv = document.createElement('div');
        nameDiv.className = 'so-prompt-name has-tooltip';
        nameDiv.textContent = prompt.name;
        nameDiv.dataset.tooltip = this.buildPromptTooltip(prompt);

        // Template preview (truncated)
        const previewDiv = document.createElement('div');
        previewDiv.className = 'so-prompt-preview';
        previewDiv.textContent = this.truncateTemplate(prompt.template, 60);

        // Meta info
        const metaDiv = document.createElement('div');
        metaDiv.className = 'so-prompt-meta';

        // Version badge
        const versionSpan = document.createElement('span');
        versionSpan.className = 'so-prompt-version';
        versionSpan.textContent = `v${prompt.version}`;

        // Time with tooltip
        const timeSpan = document.createElement('span');
        timeSpan.className = 'so-prompt-time has-tooltip';
        timeSpan.textContent = timeAgo;
        timeSpan.dataset.tooltip = `Created: ${absoluteTime}`;

        metaDiv.appendChild(versionSpan);
        metaDiv.appendChild(timeSpan);

        item.appendChild(nameDiv);
        item.appendChild(previewDiv);
        item.appendChild(metaDiv);

        // Click to open in editor
        item.addEventListener('click', () => {
            this.hide();
            openPromptInEditor(prompt);
        });

        return item;
    }

    /**
     * Build tooltip content for a prompt
     */
    private buildPromptTooltip(prompt: StoredPrompt): string {
        const parts: string[] = [];

        parts.push(`ID: ${prompt.id}`);
        parts.push(`Version: ${prompt.version}`);
        if (prompt.ax_pattern) parts.push(`Ax Pattern: ${prompt.ax_pattern}`);
        if (prompt.provider) parts.push(`Provider: ${prompt.provider}`);
        if (prompt.model) parts.push(`Model: ${prompt.model}`);
        parts.push(`---`);
        parts.push(`Template: ${this.truncateTemplate(prompt.template, 100)}`);
        if (prompt.system_prompt) {
            parts.push(`System: ${this.truncateTemplate(prompt.system_prompt, 50)}`);
        }

        return parts.join('\n');
    }

    /**
     * Truncate template for preview
     */
    private truncateTemplate(template: string, maxLength: number): string {
        if (template.length <= maxLength) return template;
        return template.substring(0, maxLength - 3) + '...';
    }

    /**
     * Refresh prompt list (called when prompts are saved)
     */
    public async refresh(): Promise<void> {
        await this.fetchPrompts();
        if (this.isVisible) {
            this.render();
        }
    }
}

// Initialize and export
const soPanel = new SoPanel();

export function showSoPanel(): void {
    soPanel.show();
}

export function hideSoPanel(): void {
    soPanel.hide();
}

export function toggleSoPanel(): void {
    soPanel.toggle();
}

export function refreshSoPanel(): void {
    soPanel.refresh();
}

export {};
