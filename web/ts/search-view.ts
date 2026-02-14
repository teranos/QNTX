/**
 * Search Results View Component
 * Displays search results in an fzf-style list format
 */

import { typeDefinitionWindow } from './type-definition-window.ts';

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
    private container: HTMLElement | null = null;
    private resultsElement: HTMLElement | null = null;
    private currentQuery: string = '';
    private isVisible: boolean = false;

    /**
     * Initialize the search view
     */
    constructor() {
        this.createElements();
    }

    /**
     * Create the DOM elements for the search view
     */
    private createElements(): void {
        this.container = document.createElement('div');
        this.container.id = 'search-view';
        this.container.className = 'search-view';

        // Create results container
        this.resultsElement = document.createElement('div');
        this.resultsElement.className = 'search-results';

        this.container.appendChild(this.resultsElement);
        document.body.appendChild(this.container);
    }

    /**
     * Show the search view
     */
    public show(): void {
        if (this.container) {
            this.container.style.display = 'block';
            this.isVisible = true;
        }
    }

    /**
     * Hide the search view
     */
    public hide(): void {
        if (this.container) {
            this.container.style.display = 'none';
            this.isVisible = false;
        }
    }

    /**
     * Check if the view is currently visible
     */
    public getIsVisible(): boolean {
        return this.isVisible;
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

    /**
     * Create a single result line
     */
    private createResultLine(match: SearchMatch): HTMLElement {
        const line = document.createElement('div');
        line.className = 'search-result-line';

        // Click handler to focus on node
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

        // Assemble the line
        line.appendChild(nodeLabel);
        line.appendChild(typeBadge);
        line.appendChild(fieldName);
        line.appendChild(excerpt);
        line.appendChild(score);
        line.appendChild(strategy);

        return line;
    }

    /**
     * Highlight the matching text in the excerpt
     */
    private highlightMatch(text: string, query: string, matchedWords?: string[]): string {
        if (!query && !matchedWords) return text;

        // If we have matched words from search, use those for highlighting
        if (matchedWords && matchedWords.length > 0) {
            let highlightedText = text;
            // Sort by length descending to avoid replacing parts of longer words
            const sortedWords = [...matchedWords].sort((a, b) => b.length - a.length);

            for (const word of sortedWords) {
                const regex = new RegExp(`\\b(${this.escapeRegex(word)})\\b`, 'gi');
                highlightedText = highlightedText.replace(regex, '<mark class="search-highlight">$1</mark>');
            }
            return highlightedText;
        }

        // Fallback to exact query matching
        const regex = new RegExp(`(${this.escapeRegex(query)})`, 'gi');
        return text.replace(regex, '<mark class="search-highlight">$1</mark>');
    }

    /**
     * Escape special regex characters
     */
    private escapeRegex(str: string): string {
        return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    /**
     * Handle clicking on a result
     */
    private handleResultClick(match: SearchMatch): void {
        // Import log dynamically to avoid circular dependency
        import('./logger.ts').then(({ log, SEG }) => {
            log.debug(SEG.QUERY, 'Focusing on node:', match.node_id);
        });

        // Hide the search view
        this.hide();

        // Send a message to focus on this node in the graph
        // This will be handled by the graph module
        const event = new CustomEvent('search-select', {
            detail: {
                nodeId: match.node_id,
                match: match
            }
        });
        document.dispatchEvent(event);
    }

    /**
     * Clear all results
     */
    public clear(): void {
        if (this.resultsElement) {
            this.resultsElement.innerHTML = '';
        }
        this.currentQuery = '';
    }
}
