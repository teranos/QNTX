/**
 * Search Results View Component
 * Displays search results in an fzf-style list format
 * Supports local results (commands, subcanvas navigation) alongside server search results
 */

import { typeDefinitionWindow } from './type-definition-window.ts';
import { escapeHtml } from './html-utils.ts';

// Search strategy constants — must match ats/storage/rich_search.go
export const STRATEGY_SUBSTRING = 'substring';
export const STRATEGY_FUZZY = 'fuzzy';
export const STRATEGY_SEMANTIC = 'semantic';

// Local result type constants
export const TYPE_COMMAND = 'command';
export const TYPE_SUBCANVAS = 'subcanvas';

export interface SearchMatch {
    node_id: string;
    type_name: string;
    type_label: string;
    field_name: string;
    field_value: string;
    excerpt: string;
    score: number;
    strategy: string;
    display_label: string;
    attributes: Record<string, any>;
    matched_words?: string[];  // The actual words that were matched for highlighting
}

export interface SearchResultsMessage {
    type: 'rich_search_results';
    query: string;
    matches: SearchMatch[];
    total: number;
}

export class SearchView {
    private resultsElement: HTMLElement | null = null;
    private currentQuery: string = '';
    private parented: boolean = false;
    private selectedIndex: number = -1;
    private localResults: SearchMatch[] = [];
    private serverResults: SearchMatch[] = [];
    private allResults: SearchMatch[] = [];

    constructor(parent?: HTMLElement) {
        this.createElements(parent);
    }

    private createElements(parent?: HTMLElement): void {
        this.resultsElement = document.createElement('div');
        this.resultsElement.className = 'search-results';

        if (parent) {
            this.parented = true;
            parent.appendChild(this.resultsElement);
        } else {
            // Legacy: standalone overlay on body
            const container = document.createElement('div');
            container.id = 'search-view';
            container.className = 'search-view';
            container.appendChild(this.resultsElement);
            document.body.appendChild(container);
        }
    }

    public show(): void {
        if (this.parented) return;
        const container = this.resultsElement?.parentElement;
        if (container) {
            container.style.display = 'block';
        }
    }

    public hide(): void {
        if (this.parented) return;
        const container = this.resultsElement?.parentElement;
        if (container) {
            container.style.display = 'none';
        }
    }

    public getIsVisible(): boolean {
        if (this.parented) return true;
        const container = this.resultsElement?.parentElement;
        return container?.style.display !== 'none';
    }

    /** Set local results (commands, subcanvases) — rendered above server results */
    public setLocalResults(matches: SearchMatch[]): void {
        this.localResults = matches;
        this.render();
    }

    /** Update server search results */
    public updateResults(message: SearchResultsMessage): void {
        if (!this.resultsElement) return;
        this.currentQuery = message.query;
        this.serverResults = message.matches;
        this.render();
    }

    /** Select next result (ArrowDown / Tab) */
    public selectNext(): void {
        if (this.allResults.length === 0) return;
        this.selectedIndex = this.selectedIndex < this.allResults.length - 1
            ? this.selectedIndex + 1
            : 0; // wrap
        this.applySelection();
    }

    /** Select previous result (ArrowUp / Shift+Tab) */
    public selectPrev(): void {
        if (this.allResults.length === 0) return;
        this.selectedIndex = this.selectedIndex > 0
            ? this.selectedIndex - 1
            : this.allResults.length - 1; // wrap
        this.applySelection();
    }

    /** Get the currently selected match, or null */
    public getSelectedMatch(): SearchMatch | null {
        if (this.selectedIndex < 0 || this.selectedIndex >= this.allResults.length) return null;
        return this.allResults[this.selectedIndex];
    }

    /** Clear selection without clearing results */
    public clearSelection(): void {
        this.selectedIndex = -1;
        this.applySelection();
    }

    public clear(): void {
        if (this.resultsElement) {
            this.resultsElement.innerHTML = '';
        }
        this.currentQuery = '';
        this.localResults = [];
        this.serverResults = [];
        this.allResults = [];
        this.selectedIndex = -1;
    }

    // --- Rendering ---

    private render(): void {
        if (!this.resultsElement) return;

        this.resultsElement.innerHTML = '';
        this.allResults = [...this.localResults, ...this.serverResults];
        this.selectedIndex = -1;

        // Local results section
        if (this.localResults.length > 0) {
            for (let i = 0; i < this.localResults.length; i++) {
                const line = this.createResultLine(this.localResults[i], i);
                this.resultsElement.appendChild(line);
            }
        }

        // Divider between local and server results
        if (this.localResults.length > 0 && this.serverResults.length > 0) {
            const divider = document.createElement('div');
            divider.className = 'search-section-divider';
            this.resultsElement.appendChild(divider);
        }

        // Server results section
        if (this.serverResults.length > 0) {
            const header = document.createElement('div');
            header.className = 'search-header';

            const headerText = document.createElement('span');
            headerText.textContent = `Found ${this.serverResults.length} matches`;
            header.appendChild(headerText);

            const newTypeBtn = document.createElement('button');
            newTypeBtn.className = 'search-new-type-btn';
            newTypeBtn.textContent = '+';
            newTypeBtn.title = 'Define new type';
            newTypeBtn.onclick = (e) => {
                e.stopPropagation();
                typeDefinitionWindow.createNewType();
            };
            header.appendChild(newTypeBtn);

            this.resultsElement.appendChild(header);

            const offset = this.localResults.length;
            for (let i = 0; i < this.serverResults.length; i++) {
                const line = this.createResultLine(this.serverResults[i], offset + i);
                this.resultsElement.appendChild(line);
            }
        }

        // No results at all
        if (this.allResults.length === 0 && this.currentQuery) {
            const noResults = document.createElement('div');
            noResults.className = 'search-no-results';
            noResults.textContent = 'No matches found';
            this.resultsElement.appendChild(noResults);
        }
    }

    private createResultLine(match: SearchMatch, index: number): HTMLElement {
        const line = document.createElement('div');
        line.className = 'search-result-line';
        line.dataset.resultIndex = String(index);
        line.dataset.resultType = match.type_name;

        line.onclick = () => {
            this.handleResultClick(match);
        };

        const isLocal = match.type_name === TYPE_COMMAND || match.type_name === TYPE_SUBCANVAS;

        // Type badge
        const typeBadge = document.createElement('span');
        typeBadge.className = 'search-type-badge';
        typeBadge.textContent = match.type_label || match.type_name;

        // Excerpt
        const excerpt = document.createElement('span');
        excerpt.className = 'search-excerpt';
        if (isLocal) {
            // Local results: bold, no highlighting
            excerpt.textContent = match.excerpt;
            excerpt.style.fontWeight = '600';
        } else {
            excerpt.innerHTML = this.highlightMatch(match.excerpt, this.currentQuery, match.matched_words);
        }

        if (isLocal) {
            // Minimal layout for commands/subcanvases: badge + excerpt
            line.appendChild(typeBadge);
            line.appendChild(excerpt);
        } else {
            // Full layout for search results
            const nodeLabel = document.createElement('span');
            nodeLabel.className = 'search-node-id';
            nodeLabel.textContent = (match.node_id || '').substring(0, 7);

            const fieldName = document.createElement('span');
            fieldName.className = 'search-field-name';
            fieldName.textContent = `[${match.field_name}]`;

            const score = document.createElement('span');
            score.className = 'search-score';
            score.textContent = `${Math.round(match.score * 100)}%`;

            const strategy = document.createElement('span');
            strategy.className = 'search-strategy';
            strategy.textContent = match.strategy === STRATEGY_SEMANTIC ? '⊨' : '≡';
            strategy.title = match.strategy;

            line.appendChild(nodeLabel);
            line.appendChild(typeBadge);
            line.appendChild(fieldName);
            line.appendChild(excerpt);
            line.appendChild(score);
            line.appendChild(strategy);
        }

        return line;
    }

    private applySelection(): void {
        if (!this.resultsElement) return;
        const lines = this.resultsElement.querySelectorAll('.search-result-line');
        lines.forEach((el, i) => {
            el.classList.toggle('search-result-selected', i === this.selectedIndex);
        });

        // Scroll selected into view
        if (this.selectedIndex >= 0 && lines[this.selectedIndex]) {
            lines[this.selectedIndex].scrollIntoView({ block: 'nearest' });
        }
    }

    private highlightMatch(text: string, query: string, matchedWords?: string[]): string {
        // Escape HTML entities first to prevent XSS from server-provided text
        const safeText = escapeHtml(text);

        if (!query && !matchedWords) return safeText;

        // If we have matched words from search, use those for highlighting
        if (matchedWords && matchedWords.length > 0) {
            let highlightedText = safeText;
            const sortedWords = [...matchedWords].sort((a, b) => b.length - a.length);

            for (const word of sortedWords) {
                const regex = new RegExp(`\\b(${this.escapeRegex(escapeHtml(word))})\\b`, 'gi');
                highlightedText = highlightedText.replace(regex, '<mark class="search-highlight">$1</mark>');
            }
            return highlightedText;
        }

        // Fallback to exact query matching
        const regex = new RegExp(`(${this.escapeRegex(escapeHtml(query))})`, 'gi');
        return safeText.replace(regex, '<mark class="search-highlight">$1</mark>');
    }

    private escapeRegex(str: string): string {
        return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    private handleResultClick(match: SearchMatch): void {
        import('./logger.ts').then(({ log, SEG }) => {
            log.debug(SEG.QUERY, 'Focusing on node:', match.node_id);
        });

        this.hide();

        const event = new CustomEvent('search-select', {
            detail: {
                nodeId: match.node_id,
                match: match
            }
        });
        document.dispatchEvent(event);
    }
}
