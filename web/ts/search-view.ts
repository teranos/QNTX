/**
 * Search Results View Component
 * Displays search results in an fzf-style list format
 */

import { typeDefinitionWindow } from './type-definition-window.ts';
import { escapeHtml } from './html-utils.ts';

// Search strategy constants — must match ats/storage/rich_search.go
export const STRATEGY_SUBSTRING = 'substring';
export const STRATEGY_FUZZY = 'fuzzy';
export const STRATEGY_SEMANTIC = 'semantic';

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

    /**
     * Update the search results
     */
    public updateResults(message: SearchResultsMessage): void {
        if (!this.resultsElement) return;

        this.currentQuery = message.query;

        // Clear existing results
        this.resultsElement.innerHTML = '';

        // Add header with match count and type definition shortcut
        const header = document.createElement('div');
        header.className = 'search-header';

        const headerText = document.createElement('span');
        headerText.textContent = `Found ${message.total} matches for "${message.query}"`;
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

        // Add each match as a result line
        message.matches.forEach((match) => {
            const resultLine = this.createResultLine(match);
            this.resultsElement!.appendChild(resultLine);
        });

        // If no matches
        if (message.matches.length === 0) {
            const noResults = document.createElement('div');
            noResults.className = 'search-no-results';
            noResults.textContent = 'No matches found';
            this.resultsElement.appendChild(noResults);
        }
    }

    private createResultLine(match: SearchMatch): HTMLElement {
        const line = document.createElement('div');
        line.className = 'search-result-line';

        line.onclick = () => {
            this.handleResultClick(match);
        };

        // Node ID/Label (shortened hash)
        const nodeLabel = document.createElement('span');
        nodeLabel.className = 'search-node-id';
        const shortId = (match.node_id || '').substring(0, 7);
        nodeLabel.textContent = shortId;

        // Type badge
        const typeBadge = document.createElement('span');
        typeBadge.className = 'search-type-badge';
        typeBadge.textContent = match.type_label || match.type_name;

        // Field name
        const fieldName = document.createElement('span');
        fieldName.className = 'search-field-name';
        fieldName.textContent = `[${match.field_name}]`;

        // Excerpt with highlighting
        const excerpt = document.createElement('span');
        excerpt.className = 'search-excerpt';
        excerpt.innerHTML = this.highlightMatch(match.excerpt, this.currentQuery, match.matched_words);

        // Score indicator
        const score = document.createElement('span');
        score.className = 'search-score';
        score.textContent = `${Math.round(match.score * 100)}%`;

        // Strategy badge (⊨ semantic, ≡ text)
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

        return line;
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

    public clear(): void {
        if (this.resultsElement) {
            this.resultsElement.innerHTML = '';
        }
        this.currentQuery = '';
    }
}
