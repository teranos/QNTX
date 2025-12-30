/**
 * Prose Navigation - Thin wrapper around FileTreeNavigator for markdown files
 *
 * Configures FileTreeNavigator for prose/markdown documentation with:
 * - /api/prose endpoint
 * - .md extension stripping in display
 * - prose-specific storage key
 */

import { FileTreeNavigator, type FileEntry } from '../filetree/navigator.ts';

// Re-export for backward compatibility
export type ProseEntry = FileEntry;

export interface ProseNavigationCallbacks {
    onDocumentSelect?: (path: string) => void;
}

export class ProseNavigation {
    private navigator: FileTreeNavigator;

    constructor(callbacks: ProseNavigationCallbacks = {}) {
        this.navigator = new FileTreeNavigator({
            apiEndpoint: '/api/prose',
            stripExtension: '.md',
            recentHeaderText: 'Recent',
            storageKey: 'prose-recent-docs',
            maxRecentFiles: 5,
            treeSelector: '.prose-tree',
            recentSelector: '#prose-recent',
            searchSelector: '.prose-search',
            onFileSelect: callbacks.onDocumentSelect
        });
    }

    // Delegate all methods to navigator
    bindElements(panel: HTMLElement): void {
        this.navigator.bindElements(panel);
    }

    async fetchProseTree(): Promise<void> {
        await this.navigator.fetchTree();
    }

    renderTree(entries?: ProseEntry[], container?: HTMLElement): void {
        this.navigator.renderTree(entries, container);
    }

    renderRecentDocs(): void {
        this.navigator.renderRecentFiles();
    }

    filterTree(query: string): void {
        this.navigator.filterTree(query);
    }

    addToRecentDocs(path: string): void {
        this.navigator.addToRecentFiles(path);
    }

    getLastViewedDoc(): string | null {
        return this.navigator.getLastViewedFile();
    }

    async refresh(): Promise<void> {
        await this.navigator.refresh();
    }
}
