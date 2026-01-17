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
        this.container.style.cssText = `
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.95);
            z-index: 100;
            display: none;
            overflow: hidden;
        `;

        // Create results container
        this.resultsElement = document.createElement('div');
        this.resultsElement.className = 'fuzzy-results';
        this.resultsElement.style.cssText = `
            height: 100%;
            overflow-y: auto;
            padding: 20px;
            font-family: 'Monaco', 'Courier New', monospace;
            font-size: 13px;
            line-height: 1.6;
        `;

        this.container.appendChild(this.resultsElement);

        // Add to graph container
        const graphContainer = document.getElementById('graph-container');
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
        header.style.cssText = `
            color: #888;
            margin-bottom: 8px;
            padding-bottom: 6px;
            border-bottom: 1px solid #333;
            font-size: 12px;
        `;
        header.textContent = `Found ${message.total} matches for "${message.query}"`;
        this.resultsElement.appendChild(header);

        // Add each match as a result line
        message.matches.forEach((match, index) => {
            const resultLine = this.createResultLine(match, index);
            this.resultsElement.appendChild(resultLine);
        });

        // If no matches
        if (message.matches.length === 0) {
            const noResults = document.createElement('div');
            noResults.style.cssText = `
                color: #666;
                text-align: center;
                margin-top: 50px;
            `;
            noResults.textContent = 'No matches found';
            this.resultsElement.appendChild(noResults);
        }
    }

    /**
     * Create a single result line
     */
    private createResultLine(match: FuzzySearchMatch, index: number): HTMLElement {
        const line = document.createElement('div');
        line.className = 'fuzzy-result-line';
        line.style.cssText = `
            padding: 4px 8px;
            margin-bottom: 1px;
            background: rgba(255, 255, 255, 0.02);
            border-left: 2px solid transparent;
            cursor: pointer;
            transition: all 0.1s ease;
            display: flex;
            align-items: baseline;
            gap: 8px;
            font-size: 12px;
            line-height: 1.4;
        `;

        // Add hover effects
        line.onmouseenter = () => {
            line.style.background = 'rgba(255, 255, 255, 0.05)';
            line.style.borderLeftColor = '#4a9eff';
        };
        line.onmouseleave = () => {
            line.style.background = 'rgba(255, 255, 255, 0.02)';
            line.style.borderLeftColor = 'transparent';
        };

        // Click handler to focus on node
        line.onclick = () => {
            this.handleResultClick(match);
        };

        // Node ID/Label (shortened hash)
        const nodeLabel = document.createElement('span');
        nodeLabel.style.cssText = `
            color: #4a9eff;
            min-width: 80px;
            flex-shrink: 0;
            font-family: monospace;
        `;
        const shortId = (match.node_id || '').substring(0, 7);
        nodeLabel.textContent = shortId;

        // Type badge
        const typeBadge = document.createElement('span');
        typeBadge.style.cssText = `
            background: rgba(74, 158, 255, 0.15);
            color: #4a9eff;
            padding: 1px 4px;
            border-radius: 2px;
            font-size: 10px;
            flex-shrink: 0;
        `;
        typeBadge.textContent = match.type_label || match.type_name;

        // Field name
        const fieldName = document.createElement('span');
        fieldName.style.cssText = `
            color: #666;
            font-size: 10px;
            flex-shrink: 0;
        `;
        fieldName.textContent = `[${match.field_name}]`;

        // Excerpt with highlighting
        const excerpt = document.createElement('span');
        excerpt.style.cssText = `
            color: #ccc;
            flex: 1;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        `;
        excerpt.innerHTML = this.highlightMatch(match.excerpt, this.currentQuery, match.matched_words);

        // Score indicator
        const score = document.createElement('span');
        score.style.cssText = `
            color: #555;
            font-size: 10px;
            flex-shrink: 0;
            min-width: 30px;
            text-align: right;
        `;
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
                highlightedText = highlightedText.replace(regex, '<mark style="background: rgba(255, 200, 0, 0.3); color: #ffc800;">$1</mark>');
            }
            return highlightedText;
        }

        // Fallback to exact query matching
        const regex = new RegExp(`(${this.escapeRegex(query)})`, 'gi');
        return text.replace(regex, '<mark style="background: rgba(255, 200, 0, 0.3); color: #ffc800;">$1</mark>');
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