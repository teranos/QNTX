/**
 * Command Explorer Panel - Query History and Statement Explorer
 *
 * Shows different content based on symbol clicked:
 * - ax (⋈): Shows ax statement types with descriptions
 * - as (+): Shows previously executed ATS query history
 *
 * For ix (⨳) operations, see job-list-panel.js
 */

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

class CommandExplorerPanel {
    private currentMode: 'ax' | 'as' | null = null;
    private panel: HTMLElement | null = null;
    private isVisible: boolean = false;

    constructor() {
        this.initialize();
    }

    initialize(): void {
        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'command-explorer-panel';
        this.panel.className = 'command-explorer-panel hidden';
        this.panel.innerHTML = this.getEmptyTemplate();

        // Insert after symbol palette
        const symbolPalette = document.getElementById('symbolPalette');
        if (symbolPalette && symbolPalette.parentNode) {
            symbolPalette.parentNode.insertBefore(this.panel, symbolPalette.nextSibling);
        }

        // Click outside to close
        document.addEventListener('click', (e: Event) => {
            const target = e.target as HTMLElement;
            if (this.panel && this.isVisible && !this.panel.contains(target) && !target.closest('.palette-cell')) {
                this.hide();
            }
        });
    }

    getEmptyTemplate(): string {
        return `
            <div class="command-explorer-header">
                <h3 class="command-explorer-title"></h3>
                <button class="command-explorer-close" aria-label="Close">✕</button>
            </div>
            <div class="command-explorer-search">
                <input type="text" placeholder="Filter..." class="command-search-input">
            </div>
            <div class="command-explorer-content"></div>
        `;
    }

    show(mode: string): void {
        if (!this.panel) return;

        this.currentMode = mode as 'ax' | 'as';
        this.isVisible = true;

        // Update content based on mode
        if (mode === 'ax') {
            this.renderAxFilters();
        } else if (mode === 'as') {
            this.renderAsHistory();
        }

        this.panel.classList.remove('hidden');
        this.panel.classList.add('visible');

        // Focus search input
        const searchInput = this.panel.querySelector('.command-search-input') as HTMLInputElement | null;
        if (searchInput) {
            setTimeout(() => searchInput.focus(), 100);
        }

        // Setup event listeners
        this.setupEventListeners();
    }

    hide(): void {
        if (!this.panel) return;

        this.isVisible = false;
        this.panel.classList.remove('visible');
        this.panel.classList.add('hidden');
    }

    toggle(mode: string): void {
        if (this.isVisible && this.currentMode === mode) {
            this.hide();
        } else {
            this.show(mode);
        }
    }

    renderAsHistory(): void {
        if (!this.panel) return;

        const title = this.panel.querySelector('.command-explorer-title');
        const content = this.panel.querySelector('.command-explorer-content');

        if (title) {
            title.textContent = '+ Query History';
        }

        const html = `
            <div class="filter-items">
                ${mockQueryHistory.map(query => `
                    <div class="filter-item" data-mode="as">
                        <div class="filter-item-query">${query.query}</div>
                        <div class="filter-item-meta">
                            ${query.timestamp} · ${query.results} results
                        </div>
                    </div>
                `).join('')}
            </div>
        `;

        if (content) {
            content.innerHTML = html;
        }
    }

    renderAxFilters(): void {
        if (!this.panel) return;

        const title = this.panel.querySelector('.command-explorer-title');
        const content = this.panel.querySelector('.command-explorer-content');

        if (title) {
            title.textContent = '⋈ ax Statements';
        }

        // TODO: Sort by chronological order (most recent first) with frequency-based ranking boost
        const html = `
            <div class="filter-items">
                ${mockAxStatements.map(stmt => `
                    <div class="filter-item" data-type="${stmt.type}" data-mode="ax">
                        <div class="filter-item-header">
                            <span class="filter-item-type">${stmt.type}</span>
                            <span class="filter-item-count">${stmt.count}</span>
                        </div>
                        <div class="filter-item-label">${stmt.label}</div>
                        <div class="filter-item-description">${stmt.description}</div>
                        <div class="filter-item-examples">
                            ${stmt.examples.slice(0, 3).map(ex => `<code>${ex}</code>`).join('')}
                        </div>
                    </div>
                `).join('')}
            </div>
        `;

        if (content) {
            content.innerHTML = html;
        }
    }

    setupEventListeners(): void {
        if (!this.panel) return;

        // Close button
        const closeBtn = this.panel.querySelector('.command-explorer-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.hide());
        }

        // Search input
        const searchInput = this.panel.querySelector('.command-search-input') as HTMLInputElement | null;
        if (searchInput) {
            searchInput.addEventListener('input', (e: Event) => {
                const target = e.target as HTMLInputElement;
                this.filterItems(target.value);
            });
        }

        // Filter items - click to populate editor with command
        const items = this.panel.querySelectorAll('.filter-item');
        items.forEach(item => {
            item.addEventListener('click', () => this.handleCommandItemClick(item as HTMLElement));
        });

        // TODO: Add action buttons within each command item for operations like:
        // - Retry failed ix invocations
        // - Stop running ix invocations
        // - View detailed logs/results
        // These buttons should have tooltips explaining their function
    }

    filterItems(searchText: string): void {
        if (!this.panel) return;

        const items = this.panel.querySelectorAll('.filter-item');
        const search = searchText.toLowerCase();

        items.forEach(item => {
            const htmlItem = item as HTMLElement;
            const type = htmlItem.dataset.type || '';
            const label = item.querySelector('.filter-item-label')?.textContent || '';
            const description = item.querySelector('.filter-item-description')?.textContent || '';

            const matches = type.includes(search) ||
                          label.toLowerCase().includes(search) ||
                          description.toLowerCase().includes(search);

            htmlItem.style.display = matches ? 'block' : 'none';
        });
    }

    handleCommandItemClick(item: HTMLElement): void {
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

        // Highlight the selected item
        if (this.panel) {
            this.panel.querySelectorAll('.filter-item').forEach(i => i.classList.remove('selected'));
        }
        item.classList.add('selected');
    }

    populateQueryFromHistory(item: HTMLElement): void {
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

    populateAxStatement(type: string): void {
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

// Export for use by symbol palette
window.commandExplorerPanel = commandExplorerPanel;

export {};
