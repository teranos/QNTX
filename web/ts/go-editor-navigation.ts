/**
 * Go Editor Navigation - File tree, recent files, and search functionality
 *
 * Handles the sidebar navigation for Go code files: file tree structure,
 * recent files tracking, and search/filter functionality.
 */

import { apiFetch } from './api.ts';

export interface CodeEntry {
    name: string;
    path: string;
    isDir: boolean;
    children?: CodeEntry[];
}

export interface GoEditorNavigationCallbacks {
    onFileSelect?: (path: string) => void;
}

export class GoEditorNavigation {
    private codeTree: CodeEntry[] = [];
    private recentFiles: string[] = [];
    private readonly MAX_RECENT_FILES = 5;
    private readonly RECENT_FILES_KEY = 'go-editor-recent-files';
    private callbacks: GoEditorNavigationCallbacks;

    // DOM elements
    private treeContainer: HTMLElement | null = null;
    private recentContainer: HTMLElement | null = null;
    private searchInput: HTMLInputElement | null = null;

    constructor(callbacks: GoEditorNavigationCallbacks = {}) {
        this.callbacks = callbacks;
        this.loadRecentFiles();
    }

    bindElements(panel: HTMLElement): void {
        this.treeContainer = panel.querySelector('.go-editor-tree');
        this.recentContainer = panel.querySelector('#go-editor-recent');
        this.searchInput = panel.querySelector('.go-editor-search') as HTMLInputElement;

        // Set up search listener
        if (this.searchInput) {
            this.searchInput.addEventListener('input', (e) => {
                const query = (e.target as HTMLInputElement).value;
                this.filterTree(query);
            });
        }
    }

    async fetchCodeTree(): Promise<void> {
        try {
            const response = await apiFetch('/api/code');
            const data = await response.json();
            this.codeTree = data || [];
        } catch (error) {
            console.error('Failed to fetch code tree:', error);
            this.codeTree = [];
        }
    }

    renderTree(entries: CodeEntry[] = this.codeTree, container?: HTMLElement): void {
        const targetContainer = container || this.treeContainer;
        if (!targetContainer) return;

        const ul = document.createElement('ul');
        ul.className = 'go-editor-tree-list';

        // For root container, always show the list
        if (!container) {
            ul.classList.add('expanded');
        }

        for (const entry of entries) {
            const li = document.createElement('li');
            li.className = 'go-editor-tree-item';

            if (entry.isDir) {
                // Directory entry
                const toggle = document.createElement('span');
                toggle.className = 'go-editor-tree-toggle';
                toggle.textContent = '▶';

                const label = document.createElement('span');
                label.className = 'go-editor-tree-label go-editor-tree-dir';
                label.textContent = entry.name;

                // Function to toggle expansion
                const toggleExpansion = () => {
                    const childList = li.querySelector('.go-editor-tree-list');
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
                label.className = 'go-editor-tree-label go-editor-tree-file';
                label.textContent = entry.name;
                label.addEventListener('click', () => {
                    if (this.callbacks.onFileSelect) {
                        this.callbacks.onFileSelect(entry.path);
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

    renderRecentFiles(): void {
        if (!this.recentContainer) return;

        if (this.recentFiles.length === 0) {
            this.recentContainer.innerHTML = '';
            return;
        }

        const header = document.createElement('div');
        header.className = 'go-editor-recent-header';
        header.textContent = 'Recent Files';

        const list = document.createElement('ul');
        list.className = 'go-editor-recent-list';

        this.recentFiles.forEach(path => {
            const li = document.createElement('li');
            li.className = 'go-editor-recent-item';

            const label = document.createElement('span');
            label.className = 'go-editor-recent-label';
            label.textContent = path.split('/').pop() || path;
            label.title = path; // Full path on hover
            label.addEventListener('click', () => {
                if (this.callbacks.onFileSelect) {
                    this.callbacks.onFileSelect(path);
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

        const items = this.treeContainer.querySelectorAll('.go-editor-tree-item');
        const lowerQuery = query.toLowerCase();

        items.forEach(item => {
            const label = item.querySelector('.go-editor-tree-label');
            if (label) {
                const text = label.textContent?.toLowerCase() || '';
                const matches = text.includes(lowerQuery);
                (item as HTMLElement).style.display = matches || !query ? '' : 'none';
            }
        });
    }

    loadRecentFiles(): void {
        try {
            const stored = localStorage.getItem(this.RECENT_FILES_KEY);
            if (stored) {
                this.recentFiles = JSON.parse(stored);
            }
        } catch (error) {
            console.warn('Failed to load recent files:', error);
            this.recentFiles = [];
        }
    }

    saveRecentFiles(): void {
        try {
            localStorage.setItem(this.RECENT_FILES_KEY, JSON.stringify(this.recentFiles));
        } catch (error) {
            console.warn('Failed to save recent files:', error);
        }
    }

    addToRecentFiles(path: string): void {
        // Remove if already exists (to move to front)
        this.recentFiles = this.recentFiles.filter(p => p !== path);

        // Add to front
        this.recentFiles.unshift(path);

        // Keep only MAX_RECENT_FILES
        this.recentFiles = this.recentFiles.slice(0, this.MAX_RECENT_FILES);

        // Persist to localStorage
        this.saveRecentFiles();

        // Update UI
        this.renderRecentFiles();
    }

    getLastViewedFile(): string | null {
        return this.recentFiles.length > 0 ? this.recentFiles[0] : null;
    }

    async refresh(): Promise<void> {
        await this.fetchCodeTree();
        this.renderTree();
        this.renderRecentFiles();
    }
}
