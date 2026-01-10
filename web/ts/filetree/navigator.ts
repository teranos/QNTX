/**
 * File Tree Navigator - Generic file tree component
 *
 * Reusable file tree navigation with support for:
 * - Hierarchical file/directory trees
 * - Recent files tracking
 * - Search/filter functionality
 * - Customizable for different file types (markdown, code, etc.)
 */

import { apiFetch } from '../api.ts';
import { handleError, SEG } from '../error-handler.ts';

export interface FileEntry {
    name: string;
    path: string;
    isDir: boolean;
    children?: FileEntry[];
}

export interface FileTreeConfig {
    // API configuration
    apiEndpoint: string;                // e.g., '/api/prose', '/api/code'

    // Display configuration
    stripExtension?: string;            // e.g., '.md' to show 'README' instead of 'README.md'
    recentHeaderText?: string;          // e.g., 'Recent', 'Recent Files'

    // Storage configuration
    storageKey: string;                 // e.g., 'prose-recent-docs', 'go-editor-recent-files'
    maxRecentFiles?: number;            // Default: 5

    // DOM selectors
    treeSelector: string;               // e.g., '.prose-tree', '#go-editor-tree'
    recentSelector: string;             // e.g., '#prose-recent', '#go-editor-recent'
    searchSelector: string;             // e.g., '.prose-search', '.go-editor-search'

    // Callbacks
    onFileSelect?: (path: string) => void;
}

export class FileTreeNavigator {
    private config: Required<FileTreeConfig>;
    private fileTree: FileEntry[] = [];
    private recentFiles: string[] = [];

    // DOM elements
    private treeContainer: HTMLElement | null = null;
    private recentContainer: HTMLElement | null = null;
    private searchInput: HTMLInputElement | null = null;

    constructor(config: FileTreeConfig) {
        // Apply defaults
        this.config = {
            ...config,
            stripExtension: config.stripExtension ?? '',
            recentHeaderText: config.recentHeaderText ?? 'Recent',
            maxRecentFiles: config.maxRecentFiles ?? 5,
            onFileSelect: config.onFileSelect ?? (() => {})
        };

        this.loadRecentFiles();
    }

    bindElements(panel: HTMLElement): void {
        this.treeContainer = panel.querySelector(this.config.treeSelector);
        this.recentContainer = panel.querySelector(this.config.recentSelector);
        this.searchInput = panel.querySelector(this.config.searchSelector) as HTMLInputElement;

        // Set up search listener
        if (this.searchInput) {
            this.searchInput.addEventListener('input', (e) => {
                const query = (e.target as HTMLInputElement).value;
                this.filterTree(query);
            });
        }
    }

    async fetchTree(): Promise<void> {
        try {
            const response = await apiFetch(this.config.apiEndpoint);
            const data = await response.json();
            this.fileTree = data || [];
        } catch (error) {
            handleError(error, `Failed to fetch file tree from ${this.config.apiEndpoint}`, { context: SEG.ERROR, silent: true });
            this.fileTree = [];
        }
    }

    renderTree(entries: FileEntry[] = this.fileTree, container?: HTMLElement): void {
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
                        if (isExpanded) {
                            childList.classList.remove('expanded');
                            toggle.textContent = '▶';
                        } else {
                            childList.classList.add('expanded');
                            toggle.textContent = '▼';
                        }
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

                // Apply extension stripping if configured
                const displayName = this.config.stripExtension
                    ? entry.name.replace(this.config.stripExtension, '')
                    : entry.name;
                label.textContent = displayName;

                label.addEventListener('click', () => {
                    if (this.config.onFileSelect) {
                        this.config.onFileSelect(entry.path);
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
        header.className = 'prose-recent-header';
        header.textContent = this.config.recentHeaderText;

        const list = document.createElement('ul');
        list.className = 'prose-recent-list';

        this.recentFiles.forEach(path => {
            const li = document.createElement('li');
            li.className = 'prose-recent-item';

            const label = document.createElement('span');
            label.className = 'prose-recent-label';

            // Extract filename and apply extension stripping
            const filename = path.split('/').pop() || path;
            const displayName = this.config.stripExtension
                ? filename.replace(this.config.stripExtension, '')
                : filename;

            label.textContent = displayName;
            label.title = path; // Full path on hover
            label.addEventListener('click', () => {
                if (this.config.onFileSelect) {
                    this.config.onFileSelect(path);
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
                const element = item as HTMLElement;
                if (matches || !query) {
                    element.classList.remove('u-hidden');
                } else {
                    element.classList.add('u-hidden');
                }
            }
        });
    }

    loadRecentFiles(): void {
        try {
            const stored = localStorage.getItem(this.config.storageKey);
            if (stored) {
                this.recentFiles = JSON.parse(stored);
            }
        } catch (error) {
            handleError(error, 'Failed to load recent files', { context: SEG.ERROR, silent: true });
            this.recentFiles = [];
        }
    }

    saveRecentFiles(): void {
        try {
            localStorage.setItem(this.config.storageKey, JSON.stringify(this.recentFiles));
        } catch (error) {
            handleError(error, 'Failed to save recent files', { context: SEG.ERROR, silent: true });
        }
    }

    addToRecentFiles(path: string): void {
        // Remove if already exists (to move to front)
        this.recentFiles = this.recentFiles.filter(p => p !== path);

        // Add to front
        this.recentFiles.unshift(path);

        // Keep only max recent files
        this.recentFiles = this.recentFiles.slice(0, this.config.maxRecentFiles);

        // Persist to localStorage
        this.saveRecentFiles();

        // Update UI
        this.renderRecentFiles();
    }

    getLastViewedFile(): string | null {
        return this.recentFiles.length > 0 ? this.recentFiles[0] : null;
    }

    async refresh(): Promise<void> {
        await this.fetchTree();
        this.renderTree();
        this.renderRecentFiles();
    }
}
