// Client-side documentation search
// Loads search-index.json at build time, performs fuzzy matching

(function() {
    'use strict';

    let searchIndex = null;
    let searchInput = null;
    let resultsContainer = null;
    let debounceTimer = null;

    // Initialize search when DOM is ready
    document.addEventListener('DOMContentLoaded', init);

    async function init() {
        searchInput = document.getElementById('search-input');
        resultsContainer = document.getElementById('search-results');

        if (!searchInput || !resultsContainer) return;

        // Load search index
        try {
            const response = await fetch('./search-index.json');
            if (response.ok) {
                searchIndex = await response.json();
            }
        } catch (e) {
            console.warn('Search index not available');
            return;
        }

        // Setup event listeners
        searchInput.addEventListener('input', handleInput);
        searchInput.addEventListener('keydown', handleKeydown);
    }

    function handleInput(e) {
        const query = e.target.value.trim();

        // Debounce search
        clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => {
            if (query.length < 2) {
                resultsContainer.innerHTML = '';
                resultsContainer.hidden = true;
                return;
            }
            performSearch(query);
        }, 150);
    }

    function handleKeydown(e) {
        // Allow Escape to clear search
        if (e.key === 'Escape') {
            searchInput.value = '';
            resultsContainer.innerHTML = '';
            resultsContainer.hidden = true;
        }
    }

    function performSearch(query) {
        if (!searchIndex) return;

        const terms = query.toLowerCase().split(/\s+/);
        const results = [];

        for (const doc of searchIndex) {
            const titleLower = doc.title.toLowerCase();
            const contentLower = doc.content.toLowerCase();
            const categoryLower = doc.category.toLowerCase();

            let score = 0;
            let matchedTerms = 0;

            for (const term of terms) {
                // Title matches are weighted higher
                if (titleLower.includes(term)) {
                    score += 10;
                    matchedTerms++;
                }
                // Category matches
                if (categoryLower.includes(term)) {
                    score += 5;
                    matchedTerms++;
                }
                // Content matches
                if (contentLower.includes(term)) {
                    score += 1;
                    matchedTerms++;
                }
            }

            // Only include if all terms matched somewhere
            if (matchedTerms >= terms.length) {
                results.push({ doc, score });
            }
        }

        // Sort by score descending
        results.sort((a, b) => b.score - a.score);

        renderResults(results.slice(0, 10), terms);
    }

    function renderResults(results, terms) {
        if (results.length === 0) {
            resultsContainer.innerHTML = '<p class="search-no-results">No results found</p>';
            resultsContainer.hidden = false;
            return;
        }

        const html = results.map(({ doc }) => {
            const snippet = getSnippet(doc.content, terms);
            return `
                <a href="${escapeHtml(doc.path)}" class="search-result">
                    <span class="search-result-title">${escapeHtml(doc.title)}</span>
                    <span class="search-result-category">${escapeHtml(doc.category)}</span>
                    ${snippet ? `<div class="search-result-snippet">${snippet}</div>` : ''}
                </a>
            `;
        }).join('');

        resultsContainer.innerHTML = html;
        resultsContainer.hidden = false;
    }

    function getSnippet(content, terms) {
        const contentLower = content.toLowerCase();
        let bestIndex = -1;
        let bestTerm = '';

        // Find first matching term in content
        for (const term of terms) {
            const index = contentLower.indexOf(term);
            if (index !== -1 && (bestIndex === -1 || index < bestIndex)) {
                bestIndex = index;
                bestTerm = term;
            }
        }

        if (bestIndex === -1) return '';

        // Extract snippet around match
        const start = Math.max(0, bestIndex - 40);
        const end = Math.min(content.length, bestIndex + bestTerm.length + 80);
        let snippet = content.slice(start, end);

        if (start > 0) snippet = '...' + snippet;
        if (end < content.length) snippet = snippet + '...';

        // Highlight matching terms
        for (const term of terms) {
            const regex = new RegExp(`(${escapeRegex(term)})`, 'gi');
            snippet = snippet.replace(regex, '<mark>$1</mark>');
        }

        return snippet;
    }

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    function escapeRegex(str) {
        return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }
})();
