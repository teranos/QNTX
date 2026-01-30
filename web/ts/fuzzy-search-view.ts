/**
 * Fuzzy Search Results View Component
 * Displays search results in an fzf-style list format
 */

export interface FuzzySearchMatch {
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

export interface FuzzySearchResultsMessage {
    type: 'rich_search_results';
    query: string;
    matches: FuzzySearchMatch[];
    total: number;
}

export class FuzzySearchView {
    private container: HTMLElement | null = null;
    private resultsElement: HTMLElement | null = null;
    private currentQuery: string = '';
    private isVisible: boolean = false;

    /**
     * Initialize the fuzzy search view
     */
    constructor() {
        this.createElements();
    }

    /**
     * Create the DOM elements for the search view
     */
    private createElements(): void {
        // Create container that will overlay the graph
        this.container = document.createElement('div');
        this.container.id = 'fuzzy-search-view';
        this.container.className = 'fuzzy-search-view';

        // Create results container
        this.resultsElement = document.createElement('div');
        this.resultsElement.className = 'fuzzy-results';

        this.container.appendChild(this.resultsElement);

        // Add to graph container
        const graphContainer = document.getElementById('graph-viewer');
        if (graphContainer) {
            graphContainer.appendChild(this.container);
        }
    }

    /**
     * Show the fuzzy search view
     */
    public show(): void {
        if (this.container) {
            this.container.style.display = 'block';
            this.isVisible = true;
        }
    }

    /**
     * Hide the fuzzy search view
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
    public updateResults(message: FuzzySearchResultsMessage): void {
        if (!this.resultsElement) return;

        this.currentQuery = message.query;

        // Clear existing results
        this.resultsElement.innerHTML = '';

        // Add header with match count
        const header = document.createElement('div');
        header.className = 'fuzzy-header';
        header.textContent = `Found ${message.total} matches for "${message.query}"`;
        this.resultsElement.appendChild(header);

        // Add each match as a result line
        message.matches.forEach((match) => {
            const resultLine = this.createResultLine(match);
            this.resultsElement!.appendChild(resultLine);
        });

        // If no matches
        if (message.matches.length === 0) {
            const noResults = document.createElement('div');
            noResults.className = 'fuzzy-no-results';
            noResults.textContent = 'No matches found';
            this.resultsElement.appendChild(noResults);
        }
    }

    /**
     * Create a single result line
     */
    private createResultLine(match: FuzzySearchMatch): HTMLElement {
        const line = document.createElement('div');
        line.className = 'fuzzy-result-line';

        // Click handler to focus on node
        line.onclick = () => {
            this.handleResultClick(match);
        };

        // Node ID/Label (shortened hash)
        const nodeLabel = document.createElement('span');
        nodeLabel.className = 'fuzzy-node-id';
        const shortId = (match.node_id || '').substring(0, 7);
        nodeLabel.textContent = shortId;

        // Type badge
        const typeBadge = document.createElement('span');
        typeBadge.className = 'fuzzy-type-badge';
        typeBadge.textContent = match.type_label || match.type_name;

        // Field name
        const fieldName = document.createElement('span');
        fieldName.className = 'fuzzy-field-name';
        fieldName.textContent = `[${match.field_name}]`;

        // Excerpt with highlighting
        const excerpt = document.createElement('span');
        excerpt.className = 'fuzzy-excerpt';
        excerpt.innerHTML = this.highlightMatch(match.excerpt, this.currentQuery, match.matched_words);

        // Score indicator
        const score = document.createElement('span');
        score.className = 'fuzzy-score';
        score.textContent = `${Math.round(match.score * 100)}%`;

        // Assemble the line
        line.appendChild(nodeLabel);
        line.appendChild(typeBadge);
        line.appendChild(fieldName);
        line.appendChild(excerpt);
        line.appendChild(score);

        return line;
    }

    /**
     * Highlight the matching text in the excerpt
     */
    private highlightMatch(text: string, query: string, matchedWords?: string[]): string {
        if (!query && !matchedWords) return text;

        // If we have matched words from fuzzy search, use those for highlighting
        if (matchedWords && matchedWords.length > 0) {
            let highlightedText = text;
            // Sort by length descending to avoid replacing parts of longer words
            const sortedWords = [...matchedWords].sort((a, b) => b.length - a.length);

            for (const word of sortedWords) {
                const regex = new RegExp(`\\b(${this.escapeRegex(word)})\\b`, 'gi');
                highlightedText = highlightedText.replace(regex, '<mark class="fuzzy-highlight">$1</mark>');
            }
            return highlightedText;
        }

        // Fallback to exact query matching
        const regex = new RegExp(`(${this.escapeRegex(query)})`, 'gi');
        return text.replace(regex, '<mark class="fuzzy-highlight">$1</mark>');
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
    private handleResultClick(match: FuzzySearchMatch): void {
        console.log('Focusing on node:', match.node_id);

        // Hide the fuzzy search view
        this.hide();

        // Send a message to focus on this node in the graph
        // This will be handled by the graph module
        const event = new CustomEvent('fuzzy-search-select', {
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