/**
 * Tests for ProseNavigation module
 *
 * Demonstrates testing:
 * - localStorage persistence
 * - Callback invocation
 * - Recent docs management
 * - API integration
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { ProseNavigation } from './navigation.ts';
import type { ProseEntry } from './navigation.ts';

// Mock localStorage for testing
const mockLocalStorage = (() => {
    let store: Record<string, string> = {};
    return {
        getItem: (key: string) => store[key] || null,
        setItem: (key: string, value: string) => { store[key] = value; },
        clear: () => { store = {}; }
    };
})();

// Mock fetch for API calls
const mockFetch = (data: unknown) => {
    return Promise.resolve({
        ok: true,
        json: () => Promise.resolve(data),
        text: () => Promise.resolve(JSON.stringify(data))
    } as Response);
};

describe('ProseNavigation', () => {
    beforeEach(() => {
        // Clear localStorage before each test
        mockLocalStorage.clear();

        // Install mocks
        global.localStorage = mockLocalStorage as Storage;
    });

    describe('Recent Docs Management', () => {
        test('adds document to recent docs', () => {
            const nav = new ProseNavigation();

            nav.addToRecentDocs('docs/getting-started.md');

            expect(nav.getLastViewedDoc()).toBe('docs/getting-started.md');
        });

        test('maintains max 5 recent docs', () => {
            const nav = new ProseNavigation();

            // Add 6 documents
            for (let i = 1; i <= 6; i++) {
                nav.addToRecentDocs(`doc${i}.md`);
            }

            // Should only keep last 5
            const lastViewed = nav.getLastViewedDoc();
            expect(lastViewed).toBe('doc6.md');

            // Verify oldest was dropped by checking localStorage
            const stored = JSON.parse(mockLocalStorage.getItem('prose-recent-docs') || '[]');
            expect(stored.length).toBe(5);
            expect(stored).not.toContain('doc1.md');
        });

        test('moves existing doc to front when re-added', () => {
            const nav = new ProseNavigation();

            nav.addToRecentDocs('doc1.md');
            nav.addToRecentDocs('doc2.md');
            nav.addToRecentDocs('doc3.md');
            nav.addToRecentDocs('doc1.md'); // Re-add first doc

            expect(nav.getLastViewedDoc()).toBe('doc1.md');

            const stored = JSON.parse(mockLocalStorage.getItem('prose-recent-docs') || '[]');
            expect(stored).toEqual(['doc1.md', 'doc3.md', 'doc2.md']);
        });

        test('persists recent docs to localStorage', () => {
            const nav = new ProseNavigation();

            nav.addToRecentDocs('test.md');

            const stored = mockLocalStorage.getItem('prose-recent-docs');
            expect(stored).toBe('["test.md"]');
        });

        test('loads recent docs from localStorage on construction', () => {
            // Pre-populate localStorage
            mockLocalStorage.setItem('prose-recent-docs', '["cached1.md","cached2.md"]');

            const nav = new ProseNavigation();

            expect(nav.getLastViewedDoc()).toBe('cached1.md');
        });

        test('handles corrupted localStorage data gracefully', () => {
            // Set invalid JSON
            mockLocalStorage.setItem('prose-recent-docs', 'invalid json{');

            const nav = new ProseNavigation();

            // Should initialize with empty array
            expect(nav.getLastViewedDoc()).toBeNull();
        });
    });

    describe('Callback Integration', () => {
        test('invokes onDocumentSelect callback when file clicked', () => {
            const mockCallback = mock((path: string) => {});
            const nav = new ProseNavigation({
                onDocumentSelect: mockCallback
            });

            // Create minimal DOM structure
            const panel = document.createElement('div');
            panel.innerHTML = `
                <div class="prose-tree"></div>
                <div id="prose-recent"></div>
                <input class="prose-search" type="text" />
            `;
            nav.bindElements(panel);

            // Render a simple tree
            const testTree: ProseEntry[] = [{
                name: 'test.md',
                path: 'test.md',
                isDir: false
            }];
            nav.renderTree(testTree);

            // Simulate click
            const fileLabel = panel.querySelector('.prose-tree-file') as HTMLElement;
            fileLabel?.click();

            expect(mockCallback).toHaveBeenCalledWith('test.md');
            expect(mockCallback).toHaveBeenCalledTimes(1);
        });
    });

    describe('Tree Rendering', () => {
        test('renders file entries without .md extension', () => {
            const nav = new ProseNavigation();

            const panel = document.createElement('div');
            panel.innerHTML = '<div class="prose-tree"></div>';
            nav.bindElements(panel);

            const testTree: ProseEntry[] = [{
                name: 'documentation.md',
                path: 'docs/documentation.md',
                isDir: false
            }];
            nav.renderTree(testTree);

            const fileLabel = panel.querySelector('.prose-tree-file');
            expect(fileLabel?.textContent).toBe('documentation');
        });

        test('renders directory entries with toggle', () => {
            const nav = new ProseNavigation();

            const panel = document.createElement('div');
            panel.innerHTML = '<div class="prose-tree"></div>';
            nav.bindElements(panel);

            const testTree: ProseEntry[] = [{
                name: 'guides',
                path: 'docs/guides',
                isDir: true,
                children: []
            }];
            nav.renderTree(testTree);

            const toggle = panel.querySelector('.prose-tree-toggle');
            const dirLabel = panel.querySelector('.prose-tree-dir');

            expect(toggle?.textContent).toBe('▶');
            expect(dirLabel?.textContent).toBe('guides');
        });
    });

    describe('Search/Filter', () => {
        test('filters tree items by search query', () => {
            const nav = new ProseNavigation();

            const panel = document.createElement('div');
            panel.innerHTML = `
                <div class="prose-tree"></div>
                <input class="prose-search" type="text" />
            `;
            nav.bindElements(panel);

            const testTree: ProseEntry[] = [
                { name: 'getting-started.md', path: 'getting-started.md', isDir: false },
                { name: 'advanced.md', path: 'advanced.md', isDir: false }
            ];
            nav.renderTree(testTree);

            // Filter for "getting"
            nav.filterTree('getting');

            const items = panel.querySelectorAll('.prose-tree-item');
            const visibleItems = Array.from(items).filter(
                (item) => !(item as HTMLElement).classList.contains('u-hidden')
            );

            expect(visibleItems.length).toBe(1);
        });

        test('shows all items when search query is empty', () => {
            const nav = new ProseNavigation();

            const panel = document.createElement('div');
            panel.innerHTML = `<div class="prose-tree"></div>`;
            nav.bindElements(panel);

            const testTree: ProseEntry[] = [
                { name: 'doc1.md', path: 'doc1.md', isDir: false },
                { name: 'doc2.md', path: 'doc2.md', isDir: false }
            ];
            nav.renderTree(testTree);

            nav.filterTree(''); // Empty query

            const items = panel.querySelectorAll('.prose-tree-item');
            const visibleItems = Array.from(items).filter(
                (item) => !(item as HTMLElement).classList.contains('u-hidden')
            );

            expect(visibleItems.length).toBe(2);
        });
    });
});
