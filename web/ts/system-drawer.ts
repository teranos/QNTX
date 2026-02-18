// Unified search drawer — search input + results in the system drawer shell

import { log, SEG } from './logger.ts';
import { getStorageItem, setStorageItem } from './indexeddb-storage.ts';
import { sendMessage } from './websocket.ts';
import { connectivityManager } from './connectivity.ts';
import { fuzzySearch } from './qntx-wasm.ts';
import { SearchView, STRATEGY_FUZZY } from './search-view.ts';
import type { SearchMatch, SearchResultsMessage } from './search-view.ts';
import { spawnGlyphByCommand } from './components/glyph/canvas/spawn-menu.ts';

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

// --- Search dispatch ---

function dispatchSearch(text: string): void {
    if (!text.trim()) {
        if (searchView) searchView.clear();
        return;
    }

    if (connectivityManager.state === 'online') {
        sendMessage({ type: 'rich_search', query: text });
    } else {
        searchOffline(text);
    }
}

function searchOffline(query: string): void {
    if (!searchView) return;

    const predicateMatches = fuzzySearch(query, 'predicates', 20, 0.3);
    const contextMatches = fuzzySearch(query, 'contexts', 20, 0.3);

    const matches: SearchMatch[] = [
        ...predicateMatches.map(m => ({
            node_id: '',
            type_name: 'predicate',
            type_label: 'P',
            field_name: 'predicate',
            field_value: m.value,
            excerpt: m.value,
            score: m.score,
            strategy: STRATEGY_FUZZY,
            display_label: m.value,
            attributes: {},
        })),
        ...contextMatches.map(m => ({
            node_id: '',
            type_name: 'context',
            type_label: 'C',
            field_name: 'context',
            field_value: m.value,
            excerpt: m.value,
            score: m.score,
            strategy: STRATEGY_FUZZY,
            display_label: m.value,
            attributes: {},
        })),
    ];

    matches.sort((a, b) => b.score - a.score);
    const top = matches.slice(0, 20);

    const message: SearchResultsMessage = {
        type: 'rich_search_results',
        query,
        matches: top,
        total: top.length,
    };

    searchView.updateResults(message);
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

/** Handle search results from WebSocket */
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
            if (e.key === 'Enter') {
                e.preventDefault();
                const text = searchInput!.value.trim();
                if (!text) return;

                // Try glyph spawn command first
                if (spawnGlyphByCommand(text)) {
                    searchInput!.value = '';
                    if (searchView) searchView.clear();
                    collapseDrawer();
                    searchInput!.blur();
                    return;
                }

                // Otherwise submit search immediately
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
