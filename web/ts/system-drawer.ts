// Unified search drawer — search input + results in the system drawer shell

import { log, SEG } from './logger.ts';
import { getStorageItem, setStorageItem } from './indexeddb-storage.ts';
import { sendMessage } from './websocket.ts';
import { connectivityManager } from './connectivity.ts';
import { getCompletions } from './qntx-wasm.ts';
import { SearchView, STRATEGY_FUZZY, TYPE_COMMAND, TYPE_SUBCANVAS } from './search-view.ts';
import type { SearchMatch, SearchResultsMessage } from './search-view.ts';
import { spawnGlyphByCommand, getMatchingCommands, COMMAND_LABELS } from './components/glyph/canvas/spawn-menu.ts';
import { uiState } from './state/ui.ts';
import { Subcanvas } from '@generated/sym.js';

const DRAWER_HEIGHT_KEY = 'system-drawer-height';
const DRAWER_MIN = 6;     // Hidden: just the grab bar
const DRAWER_HEADER = 36; // Header-only height (10px grab + ~26px header)
const DRAWER_MAX = 300;

let searchView: SearchView | null = null;
let drawerPanel: HTMLElement | null = null;
let searchInput: HTMLInputElement | null = null;
let queryTimeout: ReturnType<typeof setTimeout> | null = null;
let lastExpandedHeight = DRAWER_MAX;

function setDrawerHeight(panel: HTMLElement, height: number): void {
    const clamped = Math.max(DRAWER_MIN, Math.min(DRAWER_MAX, height));
    panel.style.height = `${clamped}px`;

    document.documentElement.style.setProperty('--drawer-height', `${clamped}px`);

    if (clamped <= DRAWER_MIN) {
        panel.classList.add('drawer-hidden');
    } else {
        panel.classList.remove('drawer-hidden');
    }
}

function expandDrawer(): void {
    if (!drawerPanel) return;
    const current = drawerPanel.offsetHeight;
    if (current <= DRAWER_HEADER) {
        const target = lastExpandedHeight > DRAWER_HEADER ? lastExpandedHeight : DRAWER_MAX;
        setDrawerHeight(drawerPanel, target);
        setStorageItem(DRAWER_HEIGHT_KEY, String(target));
    }
}

function collapseDrawer(): void {
    if (!drawerPanel) return;
    const current = drawerPanel.offsetHeight;
    if (current > DRAWER_HEADER) {
        lastExpandedHeight = current;
    }
    setDrawerHeight(drawerPanel, DRAWER_HEADER);
    setStorageItem(DRAWER_HEIGHT_KEY, String(DRAWER_HEADER));
}

// --- Local result computation ---

function computeLocalResults(query: string): SearchMatch[] {
    const results: SearchMatch[] = [];
    if (!query) return results;

    // Command matches (prefix match against COMMAND_MAP keys)
    const commands = getMatchingCommands(query);
    for (const cmd of commands) {
        results.push({
            node_id: '',
            type_name: TYPE_COMMAND,
            type_label: '⌘',
            field_name: 'spawn',
            field_value: cmd,
            excerpt: COMMAND_LABELS[cmd] || cmd,
            score: 1,
            strategy: 'local',
            display_label: cmd,
            attributes: {},
            matched_words: [],
        });
    }

    // Subcanvas matches (name contains query)
    const q = query.toLowerCase();
    const allGlyphs = uiState.getCanvasGlyphs();
    for (const glyph of allGlyphs) {
        if (glyph.symbol !== Subcanvas) continue;
        const name = glyph.content || '';
        if (!name.toLowerCase().includes(q)) continue;
        results.push({
            node_id: glyph.id,
            type_name: TYPE_SUBCANVAS,
            type_label: '⌗',
            field_name: 'navigate',
            field_value: glyph.id,
            excerpt: name || 'Untitled',
            score: 1,
            strategy: 'local',
            display_label: name,
            attributes: {},
            matched_words: [],
        });
    }

    return results;
}

// --- Search dispatch ---

function dispatchSearch(text: string): void {
    if (!text.trim()) {
        if (searchView) searchView.clear();
        return;
    }

    // Inject local results immediately (commands + subcanvases)
    if (searchView) {
        searchView.setLocalResults(computeLocalResults(text.trim()));
    }

    // Fire async search
    if (connectivityManager.state === 'online') {
        sendMessage({ type: 'rich_search', query: text });
    } else {
        searchOffline(text);
    }
}

/** Slot display labels */
const SLOT_LABELS: Record<string, string> = {
    subjects: 'S',
    predicates: 'P',
    contexts: 'C',
    actors: 'A',
};

function searchOffline(query: string): void {
    if (!searchView) return;

    const completion = getCompletions(query, 20);

    const matches: SearchMatch[] = completion.items.map(m => ({
        node_id: '',
        type_name: completion.slot,
        type_label: SLOT_LABELS[completion.slot] || completion.slot,
        field_name: completion.slot,
        field_value: m.value,
        excerpt: m.value,
        score: m.score,
        strategy: STRATEGY_FUZZY,
        display_label: m.value,
        attributes: {},
        matched_words: [],
    }));

    const message: SearchResultsMessage = {
        query,
        matches,
        total: matches.length,
    };

    searchView.updateResults(message);
}

// --- Subcanvas navigation ---

function navigateToSubcanvas(glyphId: string): void {
    const el = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!el) return;
    el.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
}

// --- Action dispatch for selected result ---

function actOnSelectedResult(match: SearchMatch): void {
    if (match.type_name === TYPE_COMMAND) {
        spawnGlyphByCommand(match.field_value);
    } else if (match.type_name === TYPE_SUBCANVAS) {
        navigateToSubcanvas(match.node_id);
    } else {
        // Regular search result — dispatch search-select event
        document.dispatchEvent(new CustomEvent('search-select', {
            detail: { nodeId: match.node_id, match }
        }));
    }

    // Clear and collapse
    if (searchInput) {
        searchInput.value = '';
        searchInput.blur();
    }
    if (searchView) searchView.clear();
    collapseDrawer();
}

// --- Public API ---

/** Focus the drawer search input and expand to full height */
export function focusDrawerSearch(): void {
    if (!searchInput || !drawerPanel) return;
    // Always expand to full height — the user is requesting search
    setDrawerHeight(drawerPanel, DRAWER_MAX);
    setStorageItem(DRAWER_HEIGHT_KEY, String(DRAWER_MAX));
    searchInput.focus();
}

/** Handle search results from WebSocket (proto: RichSearchResultsMessage). */
export function handleSearchResults(message: SearchResultsMessage): void {
    if (!searchView) return;
    searchView.updateResults(message);
}

// --- Init ---

export function initSystemDrawer(): void {
    const panel = document.getElementById('system-drawer') as HTMLElement | null;
    if (!panel) return;
    drawerPanel = panel;

    // Insert grab bar as first child of drawer
    const grabBar = document.createElement('div');
    grabBar.className = 'drawer-grab-bar';
    panel.prepend(grabBar);

    // Always start collapsed; use stored height only for expand target
    const stored = getStorageItem(DRAWER_HEIGHT_KEY);
    const storedHeight = stored ? (parseInt(stored, 10) || DRAWER_HEADER) : DRAWER_HEADER;
    lastExpandedHeight = storedHeight > DRAWER_HEADER ? storedHeight : DRAWER_MAX;
    setDrawerHeight(panel, DRAWER_HEADER);

    // --- Search input in header ---
    const header = document.getElementById('system-drawer-header') as HTMLElement | null;
    if (header) {
        searchInput = document.createElement('input');
        searchInput.type = 'text';
        searchInput.id = 'drawer-search-input';
        searchInput.placeholder = 'Search or command...';
        searchInput.autocomplete = 'off';

        const controls = header.querySelector('.controls');
        if (controls) {
            header.insertBefore(searchInput, controls);
        } else {
            header.appendChild(searchInput);
        }

        // Wire search input events
        searchInput.addEventListener('input', () => {
            if (queryTimeout) clearTimeout(queryTimeout);
            queryTimeout = setTimeout(() => {
                dispatchSearch(searchInput!.value.trim());
            }, 300);
        });

        searchInput.addEventListener('keydown', (e: KeyboardEvent) => {
            // Arrow/Tab navigation through results
            if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
                e.preventDefault();
                if (searchView) searchView.selectNext();
                return;
            }
            if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
                e.preventDefault();
                if (searchView) searchView.selectPrev();
                return;
            }

            if (e.key === 'Enter') {
                e.preventDefault();
                const text = searchInput!.value.trim();
                if (!text) return;

                // If a result is selected, act on it
                const selected = searchView?.getSelectedMatch();
                if (selected) {
                    actOnSelectedResult(selected);
                    return;
                }

                // No selection — try exact glyph command, then submit search
                if (spawnGlyphByCommand(text)) {
                    searchInput!.value = '';
                    if (searchView) searchView.clear();
                    collapseDrawer();
                    searchInput!.blur();
                    return;
                }

                // Submit search immediately
                if (queryTimeout) clearTimeout(queryTimeout);
                dispatchSearch(text);
            }

            if (e.key === 'Escape') {
                e.preventDefault();
                searchInput!.value = '';
                if (searchView) searchView.clear();
                collapseDrawer();
                searchInput!.blur();
            }
        });

        // Auto-expand on focus
        searchInput.addEventListener('focus', () => {
            expandDrawer();
        });

        // Prevent header click-toggle when interacting with input
        searchInput.addEventListener('click', (e: Event) => {
            e.stopPropagation();
        });
    }

    // --- Search results view ---
    const resultsContainer = document.getElementById('search-results-container');
    if (resultsContainer) {
        searchView = new SearchView(resultsContainer);
    }

    // --- Drag to resize ---
    const DRAG_THRESHOLD = 4;
    let pointerDown = false;
    let didDrag = false;
    let startY = 0;

    const topAnchorQuery = window.matchMedia('(max-width: 768px)');
    function isTopAnchored(): boolean {
        return topAnchorQuery.matches;
    }

    grabBar.addEventListener('pointerdown', (e: PointerEvent) => {
        pointerDown = true;
        didDrag = false;
        startY = e.clientY;
        grabBar.setPointerCapture(e.pointerId);
        e.preventDefault();
    });

    grabBar.addEventListener('pointermove', (e: PointerEvent) => {
        if (!pointerDown) return;
        if (!didDrag && Math.abs(e.clientY - startY) < DRAG_THRESHOLD) return;
        didDrag = true;
        const height = isTopAnchored() ? e.clientY : window.innerHeight - e.clientY;
        setDrawerHeight(panel, height);
    });

    grabBar.addEventListener('pointerup', (e: PointerEvent) => {
        if (!pointerDown) return;
        pointerDown = false;
        grabBar.releasePointerCapture(e.pointerId);

        if (didDrag) {
            const finalHeight = panel.offsetHeight;
            if (finalHeight > DRAWER_HEADER) {
                lastExpandedHeight = finalHeight;
            }
            setStorageItem(DRAWER_HEIGHT_KEY, String(finalHeight));
        }

        // Reset after a tick — header click handler in the same event loop
        // tick still sees didDrag=true (prevents drag→click double-fire),
        // but future clicks aren't permanently blocked.
        setTimeout(() => { didDrag = false; }, 0);
    });

    grabBar.addEventListener('pointercancel', (e: PointerEvent) => {
        if (!pointerDown) return;
        pointerDown = false;
        grabBar.releasePointerCapture(e.pointerId);
    });

    // --- Click header to toggle ---
    if (header) {
        header.addEventListener('click', function(e: Event) {
            if (didDrag) return;
            const target = e.target as HTMLElement;
            if (target.tagName === 'BUTTON' || target.tagName === 'SELECT' || target.tagName === 'INPUT') return;

            const currentHeight = panel.offsetHeight;
            let newHeight: number;

            if (currentHeight > DRAWER_HEADER) {
                lastExpandedHeight = currentHeight;
                newHeight = DRAWER_HEADER;
            } else if (currentHeight <= DRAWER_MIN) {
                newHeight = DRAWER_HEADER;
            } else {
                newHeight = lastExpandedHeight > DRAWER_HEADER ? lastExpandedHeight : DRAWER_MAX;
            }

            setDrawerHeight(panel, newHeight);
            setStorageItem(DRAWER_HEIGHT_KEY, String(newHeight));
        });
    }

    log.debug(SEG.UI, 'System drawer initialized (unified search mode)');
}
