/**
 * Prompt Preview Panel - Side-by-side prompt output comparison
 *
 * Shows a preview panel on the right side of the Prose editor when editing prompt files.
 * Displays sampled attestations with old outputs vs new outputs for visual comparison.
 * Supports fullscreen expansion for focused prompt development sessions.
 */

import { BasePanel } from '../base-panel.ts';
import { apiFetch } from '../api.ts';
import { log, SEG } from '../logger.ts';

interface PromptPreviewOptions {
    onClose?: () => void;
    getEditorContent?: () => string;
}

interface ComparisonResult {
    attestationId: string;
    oldOutput?: string;
    newOutput: string;
    prompt: string;
    tokens?: {
        prompt: number;
        completion: number;
        total: number;
    };
}

export class PromptPreviewPanel extends BasePanel {
    private options: PromptPreviewOptions;
    private currentResults: ComparisonResult[] = [];

    constructor(options: PromptPreviewOptions = {}) {
        super({
            id: 'prompt-preview-panel',
            classes: ['prompt-preview-panel'],
            useOverlay: false, // No overlay since it's a secondary panel
            closeOnEscape: true,
            slideFromRight: true
        });

        this.options = options;
    }

    // TODO(issue #339): Add vertical "PROMPT" indicator on right edge for manual toggle
    // Should show when editing prompt files, click to open/close preview panel
    // Position at ~33% from top to avoid window tray, z-index above tray (10000+)
    /**
     * Show/hide the prompt preview based on whether we're editing a prompt file
     */
    public setPromptFileActive(isPromptFile: boolean): void {
        // Auto-hide panel if not a prompt file
        if (!isPromptFile) {
            this.hide();
        }
    }

    protected getTemplate(): string {
        // TODO: Add ARIA attributes (role="region", aria-live, aria-busy) for accessibility
        return `
            <div class="prompt-preview-header">
                <div class="prompt-preview-title">
                    <span class="prompt-preview-icon">⚡</span>
                    <span class="prompt-preview-name">Prompt Preview</span>
                    <span class="prompt-preview-status"></span>
                </div>
                <div class="prompt-preview-controls">
                    <button class="prompt-preview-refresh" aria-label="Refresh preview" title="Refresh preview">↻</button>
                    <button class="prompt-preview-fullscreen" aria-label="Toggle fullscreen" title="Toggle fullscreen">⛶</button>
                    <button class="panel-close" aria-label="Close">✕</button>
                </div>
            </div>
            <div class="prompt-preview-body">
                <div class="prompt-preview-settings">
                    <div class="prompt-preview-sample-control">
                        <label for="sample-count">Sample Size:</label>
                        <input type="number" id="sample-count" min="1" max="20" value="3" />
                        <button class="prompt-preview-run">Run Preview</button>
                    </div>
                    <div class="prompt-preview-filter">
                        <input type="text" class="prompt-preview-ax-filter" placeholder="ax filter (e.g., subject=user)" />
                    </div>
                </div>
                <div class="prompt-preview-results">
                    <div class="prompt-preview-empty">
                        <p>No preview results yet.</p>
                        <p>Click "Run Preview" to sample attestations and compare outputs.</p>
                    </div>
                    <div class="prompt-preview-comparisons" style="display: none;">
                        <!-- Comparison results will be rendered here -->
                    </div>
                </div>
            </div>
            <div class="prompt-preview-footer">
                <span class="prompt-preview-token-count"></span>
                <span class="prompt-preview-cost-estimate"></span>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // TODO: Add keyboard shortcuts (Cmd/Ctrl+R for refresh, Cmd/Ctrl+Enter for run)

        // Refresh button
        const refreshBtn = this.panel?.querySelector('.prompt-preview-refresh');
        refreshBtn?.addEventListener('click', () => this.refreshPreview());

        // Fullscreen toggle
        const fullscreenBtn = this.panel?.querySelector('.prompt-preview-fullscreen');
        fullscreenBtn?.addEventListener('click', () => this.toggleFullscreen());

        // Run preview button
        const runBtn = this.panel?.querySelector('.prompt-preview-run');
        runBtn?.addEventListener('click', () => this.runPreview());

        // Close button handled by BasePanel
    }


    /**
     * Run preview with current settings
     */
    private async runPreview(): Promise<void> {
        // TODO(issue #340): Add loading state UI (disable button, show spinner)
        const sampleCount = parseInt((this.panel?.querySelector('#sample-count') as HTMLInputElement)?.value || '5', 10);
        const axFilter = (this.panel?.querySelector('.prompt-preview-ax-filter') as HTMLInputElement)?.value || 'find all';

        // Update status
        this.updateStatus('Loading preview...');

        try {
            // Get the current template from the editor
            if (!this.options.getEditorContent) {
                this.updateStatus('Error: No editor access');
                return;
            }

            const template = this.options.getEditorContent();
            if (!template || !template.trim()) {
                this.updateStatus('Error: Empty template');
                return;
            }

            // Build request to backend API
            // TODO(issue #341): Read provider from frontmatter or add UI control
            const request = {
                ax_query: axFilter,
                template: template,
                sample_size: sampleCount,
                provider: 'openrouter' // Hardcoded for now
            };

            // Call the preview API
            const response = await apiFetch('/api/prompt/preview', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(request)
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`API error: ${response.status} - ${errorText}`);
            }

            const data = await response.json() as any;

            // Transform API response to ComparisonResult format
            this.currentResults = this.transformApiResponse(data);
            this.renderComparisons();
        } catch (error) {
            log.error(SEG.ERROR, '[Prompt Preview] Error running preview:', error);
            this.updateStatus(`Error: ${error instanceof Error ? error.message : 'Unknown error'}`);
        }
    }

    /**
     * Refresh the current preview
     */
    private refreshPreview(): void {
        if (this.currentResults.length > 0) {
            this.runPreview();
        }
    }

    /**
     * Transform API response to ComparisonResult format
     */
    private transformApiResponse(data: any): ComparisonResult[] {
        if (!data.samples || !Array.isArray(data.samples)) {
            return [];
        }

        return data.samples.map((sample: any) => {
            const attestation = sample.attestation || {};
            const attestationId = attestation.id || `att-${Math.random().toString(36).substr(2, 9)}`;

            return {
                attestationId,
                oldOutput: undefined, // No old output from new API
                newOutput: sample.response || sample.error || 'No response',
                prompt: sample.interpolated_prompt || '',
                tokens: sample.total_tokens ? {
                    prompt: sample.prompt_tokens || 0,
                    completion: sample.completion_tokens || 0,
                    total: sample.total_tokens
                } : undefined
            };
        });
    }

    /**
     * Render comparison results
     */
    private renderComparisons(): void {
        // TODO: Add diff highlighting to show what changed between old and new outputs
        const emptyState = this.panel?.querySelector('.prompt-preview-empty') as HTMLElement;
        const comparisons = this.panel?.querySelector('.prompt-preview-comparisons') as HTMLElement;

        if (!comparisons || !emptyState) return;

        if (this.currentResults.length === 0) {
            emptyState.style.display = 'block';
            comparisons.style.display = 'none';
            return;
        }

        emptyState.style.display = 'none';
        comparisons.style.display = 'block';

        // Clear existing content
        comparisons.innerHTML = '';

        // Render each comparison
        this.currentResults.forEach((result, index) => {
            const comparisonEl = document.createElement('div');
            comparisonEl.className = 'prompt-preview-comparison';
            comparisonEl.innerHTML = `
                <div class="comparison-header">
                    <span class="comparison-number">#${index + 1}</span>
                    <span class="comparison-id">${result.attestationId}</span>
                    ${result.tokens ? `<span class="comparison-tokens">${result.tokens.total} tokens</span>` : ''}
                </div>
                <div class="comparison-prompt">
                    <code>${this.escapeHtml(result.prompt)}</code>
                </div>
                <div class="comparison-outputs">
                    <div class="comparison-old ${!result.oldOutput ? 'empty' : ''}">
                        <div class="comparison-label">Previous Output</div>
                        <div class="comparison-content">
                            ${result.oldOutput ? this.escapeHtml(result.oldOutput) : '<em>No previous execution</em>'}
                        </div>
                    </div>
                    <div class="comparison-new">
                        <div class="comparison-label">New Output</div>
                        <div class="comparison-content">
                            ${this.escapeHtml(result.newOutput)}
                        </div>
                    </div>
                </div>
            `;
            comparisons.appendChild(comparisonEl);
        });

        // Update footer with totals
        this.updateTokenCount();
        this.updateStatus(`Showing ${this.currentResults.length} comparison${this.currentResults.length !== 1 ? 's' : ''}`);
    }

    /**
     * Update status message
     */
    private updateStatus(message: string): void {
        const statusEl = this.panel?.querySelector('.prompt-preview-status');
        if (statusEl) {
            statusEl.textContent = message;
        }
    }

    /**
     * Update token count in footer
     */
    private updateTokenCount(): void {
        const tokenEl = this.panel?.querySelector('.prompt-preview-token-count');
        if (!tokenEl) return;

        const totalTokens = this.currentResults.reduce((sum, r) => sum + (r.tokens?.total || 0), 0);
        tokenEl.textContent = `Total tokens: ${totalTokens.toLocaleString()}`;
    }

    /**
     * Escape HTML for safe rendering
     */
    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    protected async onShow(): Promise<void> {
        // Panel is showing
    }

    protected onHide(): void {
        // Clear results
        this.currentResults = [];

        // Notify parent if needed
        this.options.onClose?.();
    }

    protected onDestroy(): void {
        // Cleanup
    }

    /**
     * Get current comparison results
     */
    public getResults(): ComparisonResult[] {
        return this.currentResults;
    }
}