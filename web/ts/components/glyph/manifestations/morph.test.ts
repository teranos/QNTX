/**
 * Tests for Manifestation Morphing (canvas ↔ window)
 */

import { describe, test, expect, beforeEach, afterEach, mock } from 'bun:test';
import { morphCanvasGlyphToWindow } from './morph';
import type { Glyph } from '../glyph';

describe('Manifestation Morphing - Tim (Happy Path)', () => {
    let container: HTMLElement;
    let canvasGlyph: Glyph;
    let glyphElement: HTMLElement;

    beforeEach(() => {
        // Clean slate
        document.body.innerHTML = '';

        // Mock Web Animations API for happy-dom compatibility
        if (!HTMLElement.prototype.animate) {
            HTMLElement.prototype.animate = mock((keyframes: Keyframe[], options: any) => {
                const animation = {
                    cancel: mock(() => {}),
                    finish: mock(() => {}),
                    addEventListener: mock((event: string, handler: any) => {
                        // Immediately trigger finish event for synchronous testing
                        if (event === 'finish') {
                            setTimeout(() => handler(), 0);
                        }
                    }),
                    removeEventListener: mock(() => {})
                };
                return animation as any;
            });
        }

        if (!HTMLElement.prototype.getAnimations) {
            HTMLElement.prototype.getAnimations = mock(() => []);
        }

        // Mock DOMParser for stripHtml (used in title bar creation)
        if (typeof DOMParser === 'undefined') {
            (global as any).DOMParser = class {
                parseFromString(html: string, type: string) {
                    return {
                        body: {
                            textContent: html.replace(/<[^>]*>/g, '')
                        }
                    };
                }
            };
        }

        // Set up canvas container
        container = document.createElement('div');
        container.className = 'canvas-workspace';
        container.style.position = 'relative';
        container.style.width = '800px';
        container.style.height = '600px';
        document.body.appendChild(container);

        // Create a simple result-style glyph element
        glyphElement = document.createElement('div');
        glyphElement.className = 'canvas-result-glyph canvas-glyph';
        glyphElement.dataset.glyphId = 'test-result-123';
        glyphElement.dataset.glyphSymbol = 'result';
        glyphElement.style.position = 'absolute';
        glyphElement.style.left = '100px';
        glyphElement.style.top = '100px';
        glyphElement.style.width = '400px';
        glyphElement.style.height = '200px';

        // Add header and content to match result glyph structure
        const header = document.createElement('div');
        header.className = 'result-glyph-header';
        header.textContent = 'Result Header';
        glyphElement.appendChild(header);

        const content = document.createElement('div');
        content.className = 'result-glyph-output';
        content.textContent = 'Test output content';
        glyphElement.appendChild(content);

        container.appendChild(glyphElement);

        // Glyph data object
        canvasGlyph = {
            id: 'test-result-123',
            title: 'Test Result',
            symbol: 'result',
            x: 100,
            y: 100,
            width: 400,
            height: 200,
            renderContent: () => {
                const el = document.createElement('div');
                el.textContent = 'Result content';
                return el;
            }
        };
    });

    afterEach(() => {
        document.body.innerHTML = '';
    });

    test('Tim morphs canvas glyph to window', async () => {
        expect(glyphElement.parentElement).toBe(container);
        expect(glyphElement.dataset.manifestation).toBeUndefined();

        let closeCalled = false;
        let restoreCalled = false;

        morphCanvasGlyphToWindow(glyphElement, canvasGlyph, container, {
            title: 'Test Window',
            width: 600,
            height: 400,
            onClose: () => { closeCalled = true; },
            onRestore: () => { restoreCalled = true; }
        });

        // Wait for animation to complete (mocked, so very fast)
        await new Promise(resolve => setTimeout(resolve, 10));

        // Element should be reparented to body
        expect(glyphElement.parentElement).toBe(document.body);
        expect(glyphElement.dataset.manifestation).toBe('window');

        // Window chrome should be present
        expect(glyphElement.className).toBe('canvas-glyph-as-window');
        expect(glyphElement.querySelector('.morph-window-title-bar')).toBeTruthy();
        expect(glyphElement.querySelector('.morph-window-content')).toBeTruthy();

        // Original content should be preserved in content area
        const contentArea = glyphElement.querySelector('.morph-window-content');
        expect(contentArea?.querySelector('.result-glyph-header')).toBeTruthy();
        expect(contentArea?.querySelector('.result-glyph-output')).toBeTruthy();

        expect(closeCalled).toBe(false);
        expect(restoreCalled).toBe(false);
    });

    test('Tim morphs window back to canvas', async () => {
        let restoreCalled = false;
        let restoredElement: HTMLElement | null = null;

        // First morph to window
        morphCanvasGlyphToWindow(glyphElement, canvasGlyph, container, {
            title: 'Test Window',
            width: 600,
            height: 400,
            onRestore: (el) => {
                restoreCalled = true;
                restoredElement = el;
            }
        });

        // Wait for morph to window to complete (mocked, so very fast)
        await new Promise(resolve => setTimeout(resolve, 10));

        expect(glyphElement.dataset.manifestation).toBe('window');
        expect(glyphElement.parentElement).toBe(document.body);

        // Find minimize button and click it
        const minimizeBtn = glyphElement.querySelector('button[title="Collapse to canvas"]') as HTMLButtonElement;
        expect(minimizeBtn).toBeTruthy();
        minimizeBtn.click();

        // Wait for minimize animation to complete (mocked, so very fast)
        await new Promise(resolve => setTimeout(resolve, 10));

        // Element should be back in canvas container
        expect(glyphElement.parentElement).toBe(container);
        expect(glyphElement.dataset.manifestation).toBeUndefined();

        // Window chrome should be removed
        expect(glyphElement.querySelector('.morph-window-title-bar')).toBeNull();
        expect(glyphElement.querySelector('.morph-window-content')).toBeNull();

        // Original content should be restored directly to element
        expect(glyphElement.querySelector('.result-glyph-header')).toBeTruthy();
        expect(glyphElement.querySelector('.result-glyph-output')).toBeTruthy();

        // onRestore callback should have been called
        expect(restoreCalled).toBe(true);
        expect(restoredElement).toBe(glyphElement);
    });
});
