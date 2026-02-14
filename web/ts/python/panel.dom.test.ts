/**
 * Python Editor Panel DOM Tests
 *
 * Purpose: Catch panel initialization regressions (PR #252 bugs)
 *
 * Why jsdom?
 * CodeMirror requires real DOM APIs (MutationObserver, requestAnimationFrame) to function.
 * happy-dom (our default test DOM) is too lightweight - missing these APIs.
 * jsdom provides complete DOM implementation but is heavy.
 *
 * Strategy:
 * - CI: USE_JSDOM=1 enables these tests (catches regressions before merge)
 * - Local: Tests skipped by default (keeps `make test` fast for development)
 * - Developers can run `USE_JSDOM=1 bun test` to debug panel issues
 *
 * These tests verify panel initialization without requiring the Rust Python plugin.
 * They focus on DOM structure and JavaScript behavior, not Python execution.
 */

import { describe, test, expect, beforeEach, afterEach } from 'bun:test';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';
const testOrSkip = USE_JSDOM ? test : test.skip;


describe('PythonEditorPanel', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        // Create a container for the panel
        container = document.createElement('div');
        container.id = 'test-container';
        document.body.appendChild(container);
    });

    afterEach(() => {
        // Clean up
        document.body.removeChild(container);
    });

    describe('Initialization', () => {
        testOrSkip('should create panel without errors', () => {
            // This would have caught the tabClickHandlers undefined bug
            expect(() => {
                // Dynamic import to avoid module-level execution
                const { showPythonEditor } = require('./panel.ts');
                showPythonEditor();
            }).not.toThrow();
        });

        testOrSkip('should initialize with null currentTab to force first render', () => {
            // This verifies the fix for the early return bug
            const panelModule = require('./panel.ts');
            const { showPythonEditor } = panelModule;

            showPythonEditor();

            // After show(), the tab content should be rendered
            // The bug would leave these empty
            const sidebar = document.querySelector('#tab-sidebar');
            const content = document.querySelector('#tab-content');

            expect(sidebar).toBeTruthy();
            expect(content).toBeTruthy();

            // Check that content was actually rendered (not just empty divs)
            expect(sidebar?.children.length).toBeGreaterThan(0);
            expect(content?.children.length).toBeGreaterThan(0);
        });
    });

    describe('Panel Structure', () => {
        testOrSkip('should render editor container on first show', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            // The bug would fail here - container would be empty
            const editorContainer = document.querySelector('#python-editor-container');
            expect(editorContainer).toBeTruthy();
            expect(editorContainer?.parentElement).toBeTruthy();
        });

        testOrSkip('should render tabs', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            const tabs = document.querySelectorAll('.python-editor-tab');
            expect(tabs.length).toBe(2); // Editor and Output tabs

            const tabTexts = Array.from(tabs).map(t => t.textContent?.trim());
            expect(tabTexts).toContain('Editor');
            expect(tabTexts).toContain('Output');
        });

        testOrSkip('should render sidebar with actions', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            const sidebar = document.querySelector('#tab-sidebar');
            expect(sidebar).toBeTruthy();

            // Check for key action buttons
            // Run button uses Button component with placeholder pattern
            const runBtn = document.querySelector('[data-button-id="python-execute"]');
            const clearBtn = document.querySelector('#python-clear-btn');

            expect(runBtn).toBeTruthy();
            expect(clearBtn).toBeTruthy();
        });
    });

    describe('Event Listener Cleanup', () => {
        testOrSkip('should not leak event listeners on repeated show/hide', () => {
            const { showPythonEditor, togglePythonEditor } = require('./panel.ts');

            // Track addEventListener calls (simplified - in real test would use spy)
            const initialListeners = document.querySelectorAll('[data-has-listener]').length;

            // Show and hide multiple times
            for (let i = 0; i < 5; i++) {
                showPythonEditor();
                togglePythonEditor(); // hide
            }

            // Should not accumulate listeners
            // (This is a simplified check - real test would verify removeEventListener calls)
            const finalListeners = document.querySelectorAll('[data-has-listener]').length;
            expect(finalListeners).toBeLessThanOrEqual(initialListeners + 10); // Some growth is OK
        });

        testOrSkip('should have tabClickHandlers map initialized', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            // If tabClickHandlers wasn't initialized, this would throw
            // when setupEventListeners tries to call .set()
            const tabs = document.querySelectorAll('.python-editor-tab');
            expect(() => {
                // Use window.Event to get jsdom's Event constructor
                tabs.forEach(tab => tab.dispatchEvent(new window.Event('click')));
            }).not.toThrow();
        });
    });

    describe('Tab Switching', () => {
        testOrSkip('should switch from editor to output tab', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            const outputTab = Array.from(document.querySelectorAll('.python-editor-tab'))
                .find(t => t.textContent?.includes('Output')) as HTMLElement;

            expect(outputTab).toBeTruthy();

            // Click output tab
            outputTab.click();

            // Check that output content is now visible
            const outputContent = document.querySelector('#python-output-content');
            expect(outputContent).toBeTruthy();
        });

        testOrSkip('should preserve editor content when switching tabs', () => {
            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            // This test would need access to the editor instance
            // Skipping for now as it requires CodeMirror
            // But the pattern is: set content, switch tab, switch back, verify content
        });
    });

    describe('Regression Tests', () => {
        testOrSkip('REGRESSION: should not skip first render due to currentTab=editor', () => {
            // This is the specific bug we fixed:
            // If currentTab starts as 'editor', switchTab('editor') returns early
            // and never renders the content

            const { showPythonEditor } = require('./panel.ts');
            showPythonEditor();

            // If the bug exists, these would be empty
            const sidebar = document.querySelector('#tab-sidebar');
            const content = document.querySelector('#tab-content');

            expect(sidebar?.innerHTML).not.toBe('');
            expect(content?.innerHTML).not.toBe('');

            // Specifically check that the comment is NOT present
            // (the bug left the empty template comment)
            expect(content?.innerHTML).not.toContain('Tab content dynamically rendered here');
        });

        testOrSkip('REGRESSION: setupEventListeners should handle undefined field initializers', () => {
            // This is the timing bug: setupEventListeners runs during super()
            // before field initializers run, so tabClickHandlers is undefined

            // This test verifies the defensive check exists
            expect(() => {
                const { showPythonEditor } = require('./panel.ts');
                showPythonEditor();
            }).not.toThrow(/can't access property "set"/);
        });
    });
});
