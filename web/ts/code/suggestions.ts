/**
 * Code Suggestions - GitHub PR fix suggestions
 *
 * Manages loading and displaying PR fix suggestions from GitHub Code Review
 */

import { apiFetch } from '../api.ts';
import type { FixSuggestion, PRInfo } from '../../../types/generated/typescript/github.ts';

export interface SuggestionsOptions {
    panel: HTMLElement;
    onNavigateToFile: (filePath: string, line: number) => void;
}

export class CodeSuggestions {
    private panel: HTMLElement;
    private currentPR: number | null = null;
    private onNavigateToFile: (filePath: string, line: number) => void;
    private prSelectListener: ((e: Event) => void) | null = null;

    constructor(options: SuggestionsOptions) {
        this.panel = options.panel;
        this.onNavigateToFile = options.onNavigateToFile;
    }

    /**
     * Helper to query elements within the panel
     */
    private $(selector: string): HTMLElement | null {
        return this.panel.querySelector(selector);
    }

    /**
     * Escape HTML to prevent XSS
     */
    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    /**
     * Load open PRs from GitHub and populate dropdown
     */
    async loadOpenPRs(): Promise<void> {
        const prSelect = this.$('#pr-select') as HTMLSelectElement;
        if (!prSelect) return;

        try {
            const response = await apiFetch('/api/code/github/pr');
            const prs: PRInfo[] = await response.json();

            if (prs.length === 0) {
                prSelect.innerHTML = '<option value="">No open PRs</option>';
                return;
            }

            // Populate dropdown with PRs
            prSelect.innerHTML = '<option value="">Select a PR...</option>' +
                prs.map(pr => `<option value="${pr.number}">#${pr.number}: ${this.escapeHtml(pr.title)}</option>`).join('');

            // Auto-select current PR if set
            if (this.currentPR) {
                prSelect.value = this.currentPR.toString();
                await this.loadPRSuggestions(this.currentPR);
            }

            // Remove old listener if exists
            if (this.prSelectListener) {
                prSelect.removeEventListener('change', this.prSelectListener);
            }

            // Add change listener
            this.prSelectListener = () => {
                const prNumber = parseInt(prSelect.value);
                if (prNumber > 0) {
                    this.loadPRSuggestions(prNumber);
                }
            };
            prSelect.addEventListener('change', this.prSelectListener);
        } catch (error) {
            console.error('[Go Editor] Failed to load open PRs:', error);
            prSelect.innerHTML = '<option value="">Failed to load PRs</option>';
        }
    }

    /**
     * Load fix suggestions for a specific PR
     */
    async loadPRSuggestions(prNumber?: number): Promise<void> {
        // Get PR number from parameter or dropdown
        if (!prNumber) {
            const prSelect = this.$('#pr-select') as HTMLSelectElement;
            prNumber = parseInt(prSelect?.value || '0');
        }

        if (!prNumber || prNumber <= 0) {
            return;
        }

        this.currentPR = prNumber;

        try {
            const response = await apiFetch(`/api/code/github/pr/${prNumber}/suggestions`);
            const suggestions: FixSuggestion[] = await response.json();

            this.renderSuggestions(suggestions);

            // Show PR info
            const prInfo = this.$('#pr-info');
            const suggestionCount = this.$('#suggestion-count');
            if (prInfo && suggestionCount) {
                prInfo.classList.remove('hidden');
                suggestionCount.textContent = `${suggestions.length} suggestion${suggestions.length !== 1 ? 's' : ''}`;
            }
        } catch (error) {
            console.error('[Go Editor] Failed to load PR suggestions:', error);
            this.showError(`Failed to load suggestions for PR #${prNumber}`);
        }
    }

    /**
     * Show error message in suggestions UI
     */
    private showError(message: string): void {
        const listElement = this.$('#suggestions-list');
        if (listElement) {
            const errorDiv = document.createElement('div');
            errorDiv.className = 'suggestion-error';
            errorDiv.textContent = `Error: ${message}`;
            listElement.innerHTML = '';
            listElement.appendChild(errorDiv);
        }
    }

    /**
     * Render suggestions in the UI
     */
    renderSuggestions(suggestions: FixSuggestion[]): void {
        const listElement = this.$('#suggestions-list');
        if (!listElement) {
            console.error('[Go Editor] suggestions-list element not found');
            return;
        }

        if (suggestions.length === 0) {
            listElement.innerHTML = '<div class="no-suggestions">No suggestions found for this PR</div>';
            return;
        }

        const html = suggestions.map((s) => `
                <div class="suggestion-item severity-${this.escapeHtml(s.severity)}" data-file="${this.escapeHtml(s.file)}" data-line="${s.start_line}">
                    <div class="suggestion-header">
                        <span class="suggestion-id">${this.escapeHtml(s.id)}</span>
                        ${s.category ? `<span class="suggestion-category">${this.escapeHtml(s.category)}</span>` : ''}
                        <span class="suggestion-severity severity-badge-${this.escapeHtml(s.severity)}">${this.escapeHtml(s.severity)}</span>
                    </div>
                    <div class="suggestion-title">${this.escapeHtml(s.title || s.issue)}</div>
                    <div class="suggestion-location">
                        <span class="suggestion-file">${this.escapeHtml(s.file)}</span>
                        <span class="suggestion-lines">Lines ${s.start_line}-${s.end_line}</span>
                    </div>
                    <div class="suggestion-issue">${this.escapeHtml(s.issue)}</div>
                </div>
            `).join('');

        listElement.innerHTML = html;

        // Add click handlers to navigate to file
        const items = listElement.querySelectorAll('.suggestion-item');
        items.forEach(item => {
            item.addEventListener('click', () => {
                const file = item.getAttribute('data-file');
                const line = parseInt(item.getAttribute('data-line') || '0');
                if (file) {
                    this.onNavigateToFile(file, line);
                }
            });
        });
    }

    /**
     * Get current PR number
     */
    getCurrentPR(): number | null {
        return this.currentPR;
    }
}
