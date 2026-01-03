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
     * Load open PRs from GitHub and populate dropdown
     */
    async loadOpenPRs(): Promise<void> {
        const prSelect = this.$('#pr-select') as HTMLSelectElement;
        if (!prSelect) return;

        try {
            const response = await apiFetch('/api/code/github/pr');
            const prs: PRInfo[] = await response.json();

            console.log('[Go Editor] Loaded', prs.length, 'open PRs');

            if (prs.length === 0) {
                prSelect.innerHTML = '<option value="">No open PRs</option>';
                return;
            }

            // Populate dropdown with PRs
            prSelect.innerHTML = '<option value="">Select a PR...</option>' +
                prs.map(pr => `<option value="${pr.number}">#${pr.number}: ${pr.title}</option>`).join('');

            // Auto-select current PR if set
            if (this.currentPR) {
                prSelect.value = this.currentPR.toString();
                await this.loadPRSuggestions(this.currentPR);
            }

            // Add change listener
            prSelect.addEventListener('change', () => {
                const prNumber = parseInt(prSelect.value);
                if (prNumber > 0) {
                    this.loadPRSuggestions(prNumber);
                }
            });
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
            alert(`Failed to load suggestions for PR #${prNumber}`);
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

        console.log('[Go Editor] Rendering', suggestions.length, 'suggestions');
        console.log('[Go Editor] First suggestion:', suggestions[0]);

        if (suggestions.length === 0) {
            listElement.innerHTML = '<div class="no-suggestions">No suggestions found for this PR</div>';
            return;
        }

        const html = suggestions.map((s, idx) => {
            const suggestionHtml = `
                <div class="suggestion-item severity-${s.severity}" data-file="${s.file}" data-line="${s.start_line}">
                    <div class="suggestion-header">
                        <span class="suggestion-id">${s.id}</span>
                        ${s.category ? `<span class="suggestion-category">${s.category}</span>` : ''}
                        <span class="suggestion-severity severity-badge-${s.severity}">${s.severity}</span>
                    </div>
                    <div class="suggestion-title">${s.title || s.issue}</div>
                    <div class="suggestion-location">
                        <span class="suggestion-file">${s.file}</span>
                        <span class="suggestion-lines">Lines ${s.start_line}-${s.end_line}</span>
                    </div>
                    <div class="suggestion-issue">${s.issue}</div>
                </div>
            `;
            if (idx === 0) {
                console.log('[Go Editor] First suggestion HTML:', suggestionHtml);
            }
            return suggestionHtml;
        }).join('');

        listElement.innerHTML = html;
        console.log('[Go Editor] HTML set, suggestion items:', listElement.querySelectorAll('.suggestion-item').length);

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
