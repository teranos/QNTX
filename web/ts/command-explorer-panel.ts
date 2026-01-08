/**
 * Command Explorer Panel - Query History and Statement Explorer
 *
 * Shows different content based on symbol clicked:
 * - ax (⋈): Shows ax statement types with descriptions
 * - as (+): Shows previously executed ATS query history
 *
 * For ix (⨳) operations, see job-list-panel.js
 */

import { BasePanel } from './base-panel.ts';
import { AX } from '@generated/sym.js';
import { setActive, DATA } from './css-classes.ts';

interface AxStatement {
    type: string;
    count: number;
    label: string;
    description: string;
    examples: string[];
}

interface QueryHistoryItem {
    query: string;
    timestamp: string;
    results: number;
}

// Mock data for ax statements
const mockAxStatements: AxStatement[] = [
    { type: 'is', count: 156, label: 'Subject/Identity', description: 'Entity is something', examples: ['is engineer', 'is company', 'is located'] },
    { type: 'of', count: 89, label: 'Object/Possession', description: 'Entity of another', examples: ['of Amsterdam', 'of Microsoft', 'of 2024'] },
    { type: 'by', count: 67, label: 'Actor/Authority', description: 'Created/verified by', examples: ['by user', 'by system', 'by api'] },
    { type: 'at', count: 45, label: 'Temporal/Location', description: 'When or where', examples: ['at 2024-01-15', 'at Amsterdam', 'at tech-conference'] }
];

// Mock data for query history (as mode)
const mockQueryHistory: QueryHistoryItem[] = [
    { query: 'is engineer AND speaks dutch', timestamp: '2 hours ago', results: 15 },
    { query: 'is developer of Amsterdam', timestamp: '1 day ago', results: 42 },
    { query: 'is manager by linkedin', timestamp: '3 days ago', results: 8 }
];

class CommandExplorerPanel extends BasePanel {
    private currentMode: 'ax' | 'as' | null = null;

    constructor() {
        super({
            id: 'command-explorer-panel',
            classes: ['command-explorer-panel'],
            useOverlay: false,  // Uses click-outside instead
            closeOnEscape: true,
            insertAfter: '#symbolPalette'
        });
    }

    protected getTemplate(): string {
        return `
            <div class="command-explorer-header">
                <h3 class="command-explorer-title"></h3>
                <button class="panel-close" aria-label="Close">✕</button>
            </div>
            <div class="command-explorer-search">
                <input type="text" placeholder="Filter..." class="command-search-input">
            </div>
            <div class="panel-content command-explorer-content"></div>
        `;
    }

    protected setupEventListeners(): void {
        // Search input
        const searchInput = this.$<HTMLInputElement>('.command-search-input');
        searchInput?.addEventListener('input', (e: Event) => {
            const target = e.target as HTMLInputElement;
            this.filterItems(target.value);
        });

        // Filter items - click to populate editor with command (event delegation)
        const content = this.$('.command-explorer-content');
        content?.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;
            const item = target.closest('.filter-item') as HTMLElement | null;
            if (item) {
                this.handleCommandItemClick(item);
            }
        });
    }

    /**
     * Show panel with specific mode
     */
    public showWithMode(mode: string): void {
        this.currentMode = mode as 'ax' | 'as';

        // Update content based on mode before showing
        if (mode === 'ax') {
            this.renderAxFilters();
        } else if (mode === 'as') {
            this.renderAsHistory();
        }

        this.show();
    }

    protected async onShow(): Promise<void> {
        // Focus search input
        const searchInput = this.$<HTMLInputElement>('.command-search-input');
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }
    }

    /**
     * Toggle panel with specific mode
     */
    public toggleWithMode(mode: string): void {
        if (this.isVisible && this.currentMode === mode) {
            this.hide();
        } else {
            this.showWithMode(mode);
        }
    }

    private renderAsHistory(): void {
        const title = this.$('.command-explorer-title');
        const content = this.$('.command-explorer-content');

        if (title) {
            title.textContent = '+ Query History';
        }

        if (!content) return;

        // Build history items using DOM API for security
        const filterItems = document.createElement('div');
        filterItems.className = 'filter-items';

        mockQueryHistory.forEach(query => {
            const item = document.createElement('div');
            item.className = 'panel-card filter-item';
            item.dataset.mode = 'as';

            // Query text with tooltip for click instruction
            const queryDiv = document.createElement('div');
            queryDiv.className = 'filter-item-query has-tooltip';
            queryDiv.textContent = query.query;
            queryDiv.dataset.tooltip = `Click to re-run query\n---\nQuery: ${query.query}\nResults: ${query.results}\nRan: ${query.timestamp}`;

            const metaDiv = document.createElement('div');
            metaDiv.className = 'filter-item-meta';
            metaDiv.textContent = `${query.timestamp} · ${query.results} results`;

            item.appendChild(queryDiv);
            item.appendChild(metaDiv);
            filterItems.appendChild(item);
        });

        content.innerHTML = '';
        content.appendChild(filterItems);
    }

    private renderAxFilters(): void {
        const title = this.$('.command-explorer-title');
        const content = this.$('.command-explorer-content');

        if (title) {
            title.textContent = `${AX} ax Statements`;
        }

        if (!content) return;

        // TODO: Sort by chronological order (most recent first) with frequency-based ranking boost

        // Build statement items using DOM API for security
        const filterItems = document.createElement('div');
        filterItems.className = 'filter-items';

        mockAxStatements.forEach(stmt => {
            const item = document.createElement('div');
            item.className = 'panel-card filter-item';
            item.dataset.type = stmt.type;
            item.dataset.mode = 'ax';

            // Header with type and count
            const header = document.createElement('div');
            header.className = 'filter-item-header';

            // Type badge with tooltip showing full description
            const typeSpan = document.createElement('span');
            typeSpan.className = 'filter-item-type has-tooltip';
            typeSpan.textContent = stmt.type;
            typeSpan.dataset.tooltip = `${stmt.label}\n---\n${stmt.description}\n\nExamples:\n${stmt.examples.join('\n')}`;

            // Count badge with tooltip showing what it represents
            const countSpan = document.createElement('span');
            countSpan.className = 'panel-badge filter-item-count has-tooltip';
            countSpan.textContent = String(stmt.count);
            countSpan.dataset.tooltip = `${stmt.count} attestations with "${stmt.type}" predicate`;

            header.appendChild(typeSpan);
            header.appendChild(countSpan);

            // Label
            const labelDiv = document.createElement('div');
            labelDiv.className = 'filter-item-label';
            labelDiv.textContent = stmt.label;

            // Description
            const descDiv = document.createElement('div');
            descDiv.className = 'filter-item-description';
            descDiv.textContent = stmt.description;

            // Examples with tooltips
            const examplesDiv = document.createElement('div');
            examplesDiv.className = 'filter-item-examples';

            stmt.examples.slice(0, 3).forEach(ex => {
                const code = document.createElement('code');
                code.className = 'has-tooltip';
                code.textContent = ex;
                code.dataset.tooltip = `Click to insert: ${ex}`;
                examplesDiv.appendChild(code);
            });

            item.appendChild(header);
            item.appendChild(labelDiv);
            item.appendChild(descDiv);
            item.appendChild(examplesDiv);

            filterItems.appendChild(item);
        });

        content.innerHTML = '';
        content.appendChild(filterItems);
    }

    private filterItems(searchText: string): void {
        const items = this.$$('.filter-item');
        const search = searchText.toLowerCase();

        items.forEach(item => {
            const htmlItem = item as HTMLElement;
            const type = htmlItem.dataset.type || '';
            const label = item.querySelector('.filter-item-label')?.textContent || '';
            const description = item.querySelector('.filter-item-description')?.textContent || '';

            const matches = type.includes(search) ||
                          label.toLowerCase().includes(search) ||
                          description.toLowerCase().includes(search);

            if (matches) {
                htmlItem.classList.remove('u-hidden');
                htmlItem.classList.add('u-block');
            } else {
                htmlItem.classList.remove('u-block');
                htmlItem.classList.add('u-hidden');
            }
        });
    }

    private handleCommandItemClick(item: HTMLElement): void {
        const mode = item.dataset.mode as 'ax' | 'as' | undefined;

        console.log(`[Command Explorer] Clicked ${mode} command`);

        if (mode === 'ax') {
            // For ax, populate the editor with a statement
            const type = item.dataset.type;
            if (type) {
                this.populateAxStatement(type);
            }
        } else if (mode === 'as') {
            // For as, populate the editor with the query
            this.populateQueryFromHistory(item);
        }

        // Highlight the selected item using data-active attribute
        this.$$('.filter-item').forEach(i => {
            setActive(i as HTMLElement, DATA.ACTIVE.INACTIVE);
        });
        setActive(item, DATA.ACTIVE.SELECTED);
    }

    private populateQueryFromHistory(item: HTMLElement): void {
        const editor = document.getElementById('ats-editor') as HTMLTextAreaElement | null;
        if (!editor) return;

        editor.focus();

        // Get query text from the item
        const queryDiv = item.querySelector('.filter-item-query');
        if (queryDiv && queryDiv.textContent) {
            editor.value = queryDiv.textContent;
            editor.selectionStart = editor.selectionEnd = editor.value.length;

            // Trigger input event to update UI
            editor.dispatchEvent(new Event('input', { bubbles: true }));
        }
    }

    private populateAxStatement(type: string): void {
        const editor = document.getElementById('ats-editor') as HTMLTextAreaElement | null;
        if (!editor) return;

        editor.focus();

        // Find example for this type
        const statement = mockAxStatements.find(stmt => stmt.type === type);
        if (statement && statement.examples.length > 0) {
            const example = statement.examples[0];
            editor.value = example;
            editor.selectionStart = editor.selectionEnd = editor.value.length;
        } else {
            editor.value = `${type} `;
            editor.selectionStart = editor.selectionEnd = editor.value.length;
        }

        // Trigger input event to update UI
        editor.dispatchEvent(new Event('input', { bubbles: true }));
    }
}

// Initialize and export
const commandExplorerPanel = new CommandExplorerPanel();

// Export for use by symbol palette - expose showWithMode and toggleWithMode
(window as any).commandExplorerPanel = {
    show: (mode: string) => commandExplorerPanel.showWithMode(mode),
    hide: () => commandExplorerPanel.hide(),
    toggle: (mode: string) => commandExplorerPanel.toggleWithMode(mode)
};

export {};
