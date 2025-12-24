/**
 * Prose Navigation - File tree, recent docs, and search functionality
 *
 * Handles the sidebar navigation: file tree structure, recent documents tracking,
 * and search/filter functionality.
 */

import { apiFetch } from '../api.ts';

export interface ProseEntry {
    name: string;
    path: string;
    isDir: boolean;
    children?: ProseEntry[];
}

export interface ProseNavigationCallbacks {
    onDocumentSelect?: (path: string) => void;
}

export class ProseNavigation {
    private proseTree: ProseEntry[] = [];
    private recentDocs: string[] = [];
    private readonly MAX_RECENT_DOCS = 5;
    private readonly RECENT_DOCS_KEY = 'prose-recent-docs';
    private callbacks: ProseNavigationCallbacks;

    // DOM elements
    private treeContainer: HTMLElement | null = null;
    private recentContainer: HTMLElement | null = null;
    private searchInput: HTMLInputElement | null = null;

    constructor(callbacks: ProseNavigationCallbacks = {}) {
        this.callbacks = callbacks;
        this.loadRecentDocs();
    }

    bindElements(panel: HTMLElement): void {
        this.treeContainer = panel.querySelector('.prose-tree');
        this.recentContainer = panel.querySelector('#prose-recent');
        this.searchInput = panel.querySelector('.prose-search') as HTMLInputElement;

        // Set up search listener
        if (this.searchInput) {
            this.searchInput.addEventListener('input', (e) => {
                const query = (e.target as HTMLInputElement).value;
                this.filterTree(query);
            });
        }
    }

    async fetchProseTree(): Promise<void> {
        try {
            const response = await apiFetch('/api/prose');
            const data = await response.json();
            this.proseTree = data || [];
        } catch (error) {
            console.error('Failed to fetch prose tree:', error);
            this.proseTree = [];
        }
    }

    renderTree(entries: ProseEntry[] = this.proseTree, container?: HTMLElement): void {
        const targetContainer = container || this.treeContainer;
        if (!targetContainer) return;

        const ul = document.createElement('ul');
        ul.className = 'prose-tree-list';

        // For root container, always show the list
        if (!container) {
            ul.classList.add('expanded');
        }

        for (const entry of entries) {
            const li = document.createElement('li');
            li.className = 'prose-tree-item';

            if (entry.isDir) {
                // Directory entry
                const toggle = document.createElement('span');
                toggle.className = 'prose-tree-toggle';
                toggle.textContent = '▶';

                const label = document.createElement('span');
                label.className = 'prose-tree-label prose-tree-dir';
                label.textContent = entry.name;

                // Function to toggle expansion
                const toggleExpansion = () => {
                    const childList = li.querySelector('.prose-tree-list');
                    if (childList) {
                        const isExpanded = childList.classList.contains('expanded');
                        childList.classList.toggle('expanded', !isExpanded);
                        toggle.textContent = isExpanded ? '▶' : '▼';
                    }
                };

                // Both toggle and label trigger expansion
                toggle.addEventListener('click', toggleExpansion);
                label.addEventListener('click', toggleExpansion);

                li.appendChild(toggle);
                li.appendChild(label);

                // Recursively render children
                if (entry.children && entry.children.length > 0) {
                    this.renderTree(entry.children, li);
                }
            } else {
                // File entry
                const label = document.createElement('span');
                label.className = 'prose-tree-label prose-tree-file';
                label.textContent = entry.name.replace('.md', '');
                label.addEventListener('click', () => {
                    if (this.callbacks.onDocumentSelect) {
                        this.callbacks.onDocumentSelect(entry.path);
                    }
                });

                li.appendChild(label);
            }

            ul.appendChild(li);
        }

        if (container) {
            container.appendChild(ul);
        } else if (targetContainer) {
            targetContainer.innerHTML = '';
            targetContainer.appendChild(ul);
        }
    }

    renderRecentDocs(): void {
        if (!this.recentContainer) return;

        if (this.recentDocs.length === 0) {
            this.recentContainer.innerHTML = '';
            return;
        }

        const header = document.createElement('div');
        header.className = 'prose-recent-header';
        header.textContent = 'Recent';

        const list = document.createElement('ul');
        list.className = 'prose-recent-list';

        this.recentDocs.forEach(path => {
            const li = document.createElement('li');
            li.className = 'prose-recent-item';

            const label = document.createElement('span');
            label.className = 'prose-recent-label';
            label.textContent = path.replace('.md', '').split('/').pop() || path;
            label.title = path; // Full path on hover
            label.addEventListener('click', () => {
                if (this.callbacks.onDocumentSelect) {
                    this.callbacks.onDocumentSelect(path);
                }
            });

            li.appendChild(label);
            list.appendChild(li);
        });

        this.recentContainer.innerHTML = '';
        this.recentContainer.appendChild(header);
        this.recentContainer.appendChild(list);
    }

    filterTree(query: string): void {
        if (!this.treeContainer) return;

        const items = this.treeContainer.querySelectorAll('.prose-tree-item');
        const lowerQuery = query.toLowerCase();

        items.forEach(item => {
            const label = item.querySelector('.prose-tree-label');
            if (label) {
                const text = label.textContent?.toLowerCase() || '';
                const matches = text.includes(lowerQuery);
                (item as HTMLElement).style.display = matches || !query ? '' : 'none';
            }
        });
    }

    loadRecentDocs(): void {
        try {
            const stored = localStorage.getItem(this.RECENT_DOCS_KEY);
            if (stored) {
                this.recentDocs = JSON.parse(stored);
            }
        } catch (error) {
            console.warn('Failed to load recent docs:', error);
            this.recentDocs = [];
        }
    }

    saveRecentDocs(): void {
        try {
            localStorage.setItem(this.RECENT_DOCS_KEY, JSON.stringify(this.recentDocs));
        } catch (error) {
            console.warn('Failed to save recent docs:', error);
        }
    }

    addToRecentDocs(path: string): void {
        // Remove if already exists (to move to front)
        this.recentDocs = this.recentDocs.filter(p => p !== path);

        // Add to front
        this.recentDocs.unshift(path);

        // Keep only MAX_RECENT_DOCS
        this.recentDocs = this.recentDocs.slice(0, this.MAX_RECENT_DOCS);

        // Persist to localStorage
        this.saveRecentDocs();

        // Update UI
        this.renderRecentDocs();
    }

    getLastViewedDoc(): string | null {
        return this.recentDocs.length > 0 ? this.recentDocs[0] : null;
    }

    async refresh(): Promise<void> {
        await this.fetchProseTree();
        this.renderTree();
        this.renderRecentDocs();
    }
}
