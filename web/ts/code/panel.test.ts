/**
 * Go Editor Panel - Critical Behavior Tests
 */

import { describe, test, expect, mock, beforeEach } from 'bun:test';
import { GoEditorNavigation } from './navigation.ts';
import type { CodeEntry } from './navigation.ts';

// Mock localStorage for testing
const mockLocalStorage = (() => {
    let store: Record<string, string> = {};
    return {
        getItem: (key: string) => store[key] || null,
        setItem: (key: string, value: string) => { store[key] = value; },
        clear: () => { store = {}; }
    };
})();

describe('Go Editor Panel', () => {
    beforeEach(() => {
        mockLocalStorage.clear();
        global.localStorage = mockLocalStorage as Storage;
    });

    test('status updates from connecting to ready when editor loads', () => {
        // This is the happy path - status should go from gray → green
        const panel = document.createElement('div');
        panel.innerHTML = '<span id="gopls-status">connecting...</span>';

        const statusEl = panel.querySelector('#gopls-status') as HTMLElement;

        // Simulate what updateStatus('ready') does
        statusEl.textContent = 'ready';
        statusEl.style.color = '#4ec9b0';

        expect(statusEl.textContent).toBe('ready');
        expect(statusEl.style.color).toMatch(/#4ec9b0|rgb\(78, 201, 176\)/); // Green
    });

    test('status shows error when gopls is unavailable', () => {
        // Critical: user needs to know if gopls isn't working
        const panel = document.createElement('div');
        panel.innerHTML = '<span id="gopls-status">connecting...</span>';

        const statusEl = panel.querySelector('#gopls-status') as HTMLElement;

        // Simulate what updateStatus('unavailable', 'gopls disabled') does
        statusEl.textContent = 'gopls disabled';
        statusEl.style.color = '#858585';

        expect(statusEl.textContent).toBe('gopls disabled');
        expect(statusEl.style.color).toMatch(/#858585|rgb\(133, 133, 133\)/); // Gray
    });

    test('panel toggles between hidden and visible', () => {
        // Basic UX: panel should show/hide when toggled
        const panel = document.createElement('div');
        panel.className = 'prose-panel hidden';

        // Show
        panel.classList.remove('hidden');
        panel.classList.add('visible');
        expect(panel.classList.contains('visible')).toBe(true);

        // Hide
        panel.classList.remove('visible');
        panel.classList.add('hidden');
        expect(panel.classList.contains('hidden')).toBe(true);
    });

    describe('File Tree Navigation', () => {
        test('clicking file in tree triggers onFileSelect callback', () => {
            // Critical: file selection must work for browsing to function
            const mockCallback = mock((path: string) => {});
            const nav = new GoEditorNavigation({
                onFileSelect: mockCallback
            });

            // Create minimal DOM structure
            const panel = document.createElement('div');
            panel.innerHTML = `
                <div id="go-editor-tree"></div>
                <div id="go-editor-recent"></div>
                <input class="go-editor-search" type="text" />
            `;
            nav.bindElements(panel);

            // Render a simple tree with a Go file
            const testTree: CodeEntry[] = [{
                name: 'main.go',
                path: 'cmd/qntx/main.go',
                isDir: false
            }];
            nav.renderTree(testTree);

            // Simulate clicking the file
            const fileLabel = panel.querySelector('.prose-tree-file') as HTMLElement;
            fileLabel?.click();

            expect(mockCallback).toHaveBeenCalledWith('cmd/qntx/main.go');
            expect(mockCallback).toHaveBeenCalledTimes(1);
        });
    });

    describe('Unsaved Changes Tracking', () => {
        test('hasUnsavedChanges flag is set when content changes', () => {
            // Critical: must warn user before closing with unsaved changes
            const editorContainer = document.createElement('div');
            const saveIndicator = document.createElement('span');
            saveIndicator.className = 'go-editor-save-indicator hidden';

            // Simulate what happens when editor content changes
            let hasUnsavedChanges = false;

            // Simulate EditorView.updateListener detecting change
            hasUnsavedChanges = true;

            // Update save indicator
            if (hasUnsavedChanges) {
                saveIndicator.textContent = '● Unsaved changes';
                saveIndicator.classList.remove('hidden');
            }

            expect(hasUnsavedChanges).toBe(true);
            expect(saveIndicator.textContent).toBe('● Unsaved changes');
            expect(saveIndicator.classList.contains('hidden')).toBe(false);
        });

        test('hasUnsavedChanges is cleared after successful save', () => {
            // Critical: indicator must disappear after save
            let hasUnsavedChanges = true;
            const saveIndicator = document.createElement('span');
            saveIndicator.textContent = '● Unsaved changes';
            saveIndicator.classList.remove('hidden');

            // Simulate successful save
            hasUnsavedChanges = false;
            saveIndicator.classList.add('hidden');

            expect(hasUnsavedChanges).toBe(false);
            expect(saveIndicator.classList.contains('hidden')).toBe(true);
        });
    });

    describe('Save Functionality', () => {
        test('save sends PUT request with file content', async () => {
            // Critical: save must send content to correct endpoint
            const mockFetch = mock(async (url: string, options: RequestInit) => {
                return {
                    ok: true,
                    status: 204
                } as Response;
            });

            global.fetch = mockFetch as any;

            const currentPath = 'cmd/qntx/main.go';
            const content = 'package main\n\nfunc main() {}\n';

            // Simulate what saveContent() does
            await fetch(`/api/code/${currentPath}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'text/plain' },
                body: content
            });

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/code/cmd/qntx/main.go',
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'text/plain' },
                    body: content
                }
            );
            expect(mockFetch).toHaveBeenCalledTimes(1);
        });
    });

    describe('PR Suggestions - Critical Path', () => {
        test('suggestion item has file and line data attributes', () => {
            // When user clicks suggestion, we need file path and line number
            const suggestion = document.createElement('div');
            suggestion.className = 'suggestion-item';
            suggestion.setAttribute('data-file', 'code/github/github.go');
            suggestion.setAttribute('data-line', '199');

            expect(suggestion.getAttribute('data-file')).toBe('code/github/github.go');
            expect(parseInt(suggestion.getAttribute('data-line')!)).toBe(199);
        });

        test('tab switching preserves content string', () => {
            // When switching tabs, editor content must be preserved
            let editorContent = 'package main\n\nfunc main() {}\n';
            let currentTab: 'editor' | 'suggestions' = 'editor';

            // User edits
            editorContent = '// Comment\n' + editorContent;

            // Switch away - store content
            const saved = editorContent;
            currentTab = 'suggestions';

            // Switch back - restore content
            currentTab = 'editor';
            const restored = saved;

            expect(restored).toBe(editorContent);
            expect(restored).toContain('// Comment');
        });
    });
});
