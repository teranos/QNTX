/**
 * Go Editor Navigation - Thin wrapper around FileTreeNavigator for Go code files
 *
 * Configures FileTreeNavigator for Go source code with:
 * - /api/code endpoint
 * - Full filename display (no extension stripping)
 * - code-specific storage key
 */

import { FileTreeNavigator, type FileEntry } from '../filetree/navigator.ts';

// Re-export for backward compatibility
export type CodeEntry = FileEntry;

export interface GoEditorNavigationCallbacks {
    onFileSelect?: (path: string) => void;
}

export class GoEditorNavigation {
    private navigator: FileTreeNavigator;

    constructor(callbacks: GoEditorNavigationCallbacks = {}) {
        this.navigator = new FileTreeNavigator({
            apiEndpoint: '/api/code',
            stripExtension: undefined,  // Don't strip extension for code files
            recentHeaderText: 'Recent Files',
            storageKey: 'go-editor-recent-files',
            maxRecentFiles: 5,
            treeSelector: '#go-editor-tree',
            recentSelector: '#go-editor-recent',
            searchSelector: '.go-editor-search',
            onFileSelect: callbacks.onFileSelect
        });
    }

    // Delegate all methods to navigator
    bindElements(panel: HTMLElement): void {
        this.navigator.bindElements(panel);
    }

    async fetchCodeTree(): Promise<void> {
        await this.navigator.fetchTree();
    }

    renderTree(entries?: CodeEntry[], container?: HTMLElement): void {
        this.navigator.renderTree(entries, container);
    }

    renderRecentFiles(): void {
        this.navigator.renderRecentFiles();
    }

    filterTree(query: string): void {
        this.navigator.filterTree(query);
    }

    addToRecentFiles(path: string): void {
        this.navigator.addToRecentFiles(path);
    }

    getLastViewedFile(): string | null {
        return this.navigator.getLastViewedFile();
    }

    async refresh(): Promise<void> {
        await this.navigator.refresh();
    }
}
