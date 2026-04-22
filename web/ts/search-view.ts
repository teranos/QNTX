/**
 * Search Results View Component
 *
 * Renders two kinds of results in a unified list:
 *  - Local (commands, subcanvas navigation) — synthesized by system-drawer.ts
 *  - Plugin (server-forwarded from the SearchService gRPC provider)
 *
 * Plugin result `document` payloads are opaque JSON from the provider; the view
 * renders common fields when present and falls back to the id otherwise.
 */

import { createNewType } from './type-definition-window.ts';
import { escapeHtml } from './html-utils.ts';

// Local result type constants
export const TYPE_COMMAND = 'command';
export const TYPE_SUBCANVAS = 'subcanvas';
export const TYPE_PLUGIN = 'plugin';

/** A single entry rendered in the search results list. */
export interface SearchMatch {
    /** Primary identifier (glyph id for subcanvases, command key, or plugin doc id). */
    node_id: string;
    /** Result kind: TYPE_COMMAND, TYPE_SUBCANVAS, or TYPE_PLUGIN. */
    type_name: string;
    /** Short badge label (e.g. "⌘", "⌗", or the plugin's type). */
    type_label: string;
    /** Main display text for the line. */
    excerpt: string;
    /** Score in [0, 1]; local matches default to 1. */
    score: number;
    /** Optional payload (plugin document or command field). */
    field_value?: string;
    /** Plugin document attributes used for highlighting / selection. */
    attributes?: Record<string, unknown>;
    /** Words to highlight within the excerpt. */
    matched_words?: string[];
}

/** Message shape delivered to updateResults (built from plugin gRPC hits). */
export interface SearchResultsMessage {
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
        if (container) container.style.display = 'block';
    }

    public hide(): void {
        if (this.parented) return;
        const container = this.resultsElement?.parentElement;
        if (container) container.style.display = 'none';
    }

    public getIsVisible(): boolean {
        if (this.parented) return true;
        const container = this.resultsElement?.parentElement;
        return container?.style.display !== 'none';
    }

    public setLocalResults(matches: SearchMatch[]): void {
        this.localResults = matches;
        this.render();
    }

    public updateResults(message: SearchResultsMessage): void {
        if (!this.resultsElement) return;
        this.currentQuery = message.query;
        this.serverResults = message.matches;
        this.render();
    }

    public selectNext(): void {
        if (this.allResults.length === 0) return;
        this.selectedIndex = this.selectedIndex < this.allResults.length - 1
            ? this.selectedIndex + 1
            : 0;
        this.applySelection();
    }

    public selectPrev(): void {
        if (this.allResults.length === 0) return;
        this.selectedIndex = this.selectedIndex > 0
            ? this.selectedIndex - 1
            : this.allResults.length - 1;
        this.applySelection();
    }

    public getSelectedMatch(): SearchMatch | null {
        if (this.selectedIndex < 0 || this.selectedIndex >= this.allResults.length) return null;
        return this.allResults[this.selectedIndex];
    }

    public clearSelection(): void {
        this.selectedIndex = -1;
        this.applySelection();
    }

    public clear(): void {
        if (this.resultsElement) this.resultsElement.innerHTML = '';
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

        if (this.localResults.length > 0) {
            for (let i = 0; i < this.localResults.length; i++) {
                this.resultsElement.appendChild(this.createResultLine(this.localResults[i], i));
            }
        }

        if (this.localResults.length > 0 && this.serverResults.length > 0) {
            const divider = document.createElement('div');
            divider.className = 'search-section-divider';
            this.resultsElement.appendChild(divider);
        }

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
                createNewType();
            };
            header.appendChild(newTypeBtn);

            this.resultsElement.appendChild(header);

            const offset = this.localResults.length;
            for (let i = 0; i < this.serverResults.length; i++) {
                this.resultsElement.appendChild(this.createResultLine(this.serverResults[i], offset + i));
            }
        }

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
        line.onclick = () => this.handleResultClick(match);

        const isLocal = match.type_name === TYPE_COMMAND || match.type_name === TYPE_SUBCANVAS;

        const typeBadge = document.createElement('span');
        typeBadge.className = 'search-type-badge';
        typeBadge.textContent = match.type_label || match.type_name;

        const excerpt = document.createElement('span');
        excerpt.className = 'search-excerpt';
        if (isLocal) {
            excerpt.textContent = match.excerpt;
            excerpt.style.fontWeight = '600';
            line.appendChild(typeBadge);
            line.appendChild(excerpt);
            return line;
        }

        excerpt.innerHTML = this.highlightMatch(match.excerpt, this.currentQuery, match.matched_words);

        const nodeLabel = document.createElement('span');
        nodeLabel.className = 'search-node-id';
        nodeLabel.textContent = (match.node_id || '').substring(0, 7);

        const score = document.createElement('span');
        score.className = 'search-score';
        score.textContent = `${Math.round(match.score * 100)}%`;

        line.appendChild(nodeLabel);
        line.appendChild(typeBadge);
        line.appendChild(excerpt);
        line.appendChild(score);

        return line;
    }

    private applySelection(): void {
        if (!this.resultsElement) return;
        const lines = this.resultsElement.querySelectorAll('.search-result-line');
        lines.forEach((el, i) => {
            el.classList.toggle('search-result-selected', i === this.selectedIndex);
        });
        if (this.selectedIndex >= 0 && lines[this.selectedIndex]) {
            lines[this.selectedIndex].scrollIntoView({ block: 'nearest' });
        }
    }

    /**
     * Highlight occurrences of query or matched words inside the excerpt.
     * Uses string methods (per CLAUDE.md: regex is banned).
     */
    private highlightMatch(text: string, query: string, matchedWords?: string[]): string {
        const safeText = escapeHtml(text);
        if (!query && (!matchedWords || matchedWords.length === 0)) return safeText;

        // Build a list of needles to wrap. Prefer matched words (provider-supplied);
        // fall back to the raw query. Longest-first so substrings don't double-wrap.
        const needles = (matchedWords && matchedWords.length > 0)
            ? [...matchedWords]
            : [query];
        needles.sort((a, b) => b.length - a.length);

        return wrapOccurrencesCaseInsensitive(safeText, needles);
    }

    private handleResultClick(match: SearchMatch): void {
        import('./logger.ts').then(({ log, SEG }) => {
            log.debug(SEG.QUERY, 'Focusing on node:', match.node_id);
        });
        this.hide();
        document.dispatchEvent(new CustomEvent('search-select', {
            detail: { nodeId: match.node_id, match }
        }));
    }
}

// --- Highlighting helpers (string-method-based, no regex) ---

/** Wrap every case-insensitive occurrence of any needle in `<mark>` tags. */
function wrapOccurrencesCaseInsensitive(haystack: string, rawNeedles: string[]): string {
    const needles = rawNeedles
        .map(n => escapeHtml(n))
        .filter(n => n.length > 0);
    if (needles.length === 0) return haystack;

    const lowerHay = haystack.toLowerCase();
    const lowerNeedles = needles.map(n => n.toLowerCase());

    let out = '';
    let i = 0;
    while (i < haystack.length) {
        let matchLen = 0;
        for (let n = 0; n < lowerNeedles.length; n++) {
            const needle = lowerNeedles[n];
            if (needle.length === 0) continue;
            if (lowerHay.startsWith(needle, i)) {
                matchLen = needle.length;
                break;
            }
        }
        if (matchLen > 0) {
            out += '<mark class="search-highlight">' + haystack.slice(i, i + matchLen) + '</mark>';
            i += matchLen;
        } else {
            out += haystack[i];
            i += 1;
        }
    }
    return out;
}
