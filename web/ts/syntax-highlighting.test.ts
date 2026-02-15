/**
 * Syntax Highlighting Regression Tests
 *
 * Purpose: Prevent regressions in CodeMirror syntax highlighting (Issue #75)
 *
 * Why jsdom?
 * CodeMirror requires real DOM APIs (MutationObserver, requestAnimationFrame) to function.
 * happy-dom (our default test DOM) is too lightweight - missing these APIs.
 * jsdom provides complete DOM implementation but is heavy (52 packages, ~20MB).
 *
 * Strategy:
 * - CI: USE_JSDOM=1 enables these tests (catches regressions before merge)
 * - Local: Tests skipped by default (keeps `make test` fast for development)
 * - Developers can run `USE_JSDOM=1 bun test` to debug syntax highlighting issues
 *
 * Trade-off Analysis:
 * ✅ Prevents critical regressions (#75 broke syntax highlighting entirely)
 * ✅ No impact on local dev speed (skipped by default)
 * ❌ CI slower (~670ms vs ~150ms per test file)
 * ❌ 52 additional packages in devDependencies
 *
 * Related Issues:
 * - #75: Fix Go syntax highlighting crash (fixed via @lezer version pins)
 * - #80: Add syntax highlighting tests
 */

import { describe, test, expect, beforeEach, afterEach } from 'bun:test';
import { EditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';
const testOrSkip = USE_JSDOM ? test : test.skip;

// Setup jsdom if enabled
if (USE_JSDOM) {
    // Polyfill browser APIs that happy-dom doesn't provide
    globalThis.requestAnimationFrame = (cb: any) => setTimeout(cb, 0) as any;
    globalThis.cancelAnimationFrame = (id: any) => clearTimeout(id);
    (window as any).requestAnimationFrame = globalThis.requestAnimationFrame;
    (window as any).cancelAnimationFrame = globalThis.cancelAnimationFrame;
}

describe('Go Syntax Highlighting Regression Tests', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    afterEach(() => {
        // Clean up DOM to prevent memory leaks
        container.remove();
    });

    testOrSkip('loads Go language support without crashing (regression: #75)', async () => {
        // This test caught the @lezer version conflict that caused:
        // "TypeError: can't access property Symbol.iterator, K is undefined"
        //
        // NOTE: When adding TypeScript support, update this test to import both
        // @codemirror/lang-go and @codemirror/lang-typescript to verify no
        // version conflicts between multiple language packages.

        const code = 'func main() {\n\tprintln("Hello")\n}';
        const { go } = await import('@codemirror/lang-go');

        const state = EditorState.create({
            doc: code,
            extensions: [go(), syntaxHighlighting(defaultHighlightStyle)]
        });

        const view = new EditorView({ state, parent: container });

        // Verify initialization succeeded
        expect(view).toBeDefined();
        expect(container.textContent).toContain('func');
        expect(container.textContent).toContain('main');
    });

    testOrSkip('handles complex Go code structures', async () => {
        // Regression test: ensure complex Go syntax doesn't crash parser
        const code = `package main

import (
    "fmt"
    "time"
)

type Server struct {
    Port int
}

func (s *Server) Start() error {
    for i := 0; i < 10; i++ {
        select {
        case <-time.After(time.Second):
            fmt.Println(i)
        }
    }
    return nil
}`;

        const { go } = await import('@codemirror/lang-go');

        const state = EditorState.create({
            doc: code,
            extensions: [go(), syntaxHighlighting(defaultHighlightStyle)]
        });

        const view = new EditorView({ state, parent: container });

        // Verify initialization succeeded
        expect(view).toBeDefined();
        expect(container.textContent).toContain('package');
        expect(container.textContent).toContain('import');
        expect(container.textContent).toContain('struct');
        expect(container.textContent).toContain('func');
    });

    testOrSkip('handles empty Go code', async () => {
        // Edge case: empty document shouldn't crash
        const { go } = await import('@codemirror/lang-go');

        const state = EditorState.create({
            doc: '',
            extensions: [go(), syntaxHighlighting(defaultHighlightStyle)]
        });

        const view = new EditorView({ state, parent: container });

        // Verify initialization succeeded even with empty content
        expect(view).toBeDefined();
        expect(container.textContent).toBe('');
    });

    testOrSkip('handles Go code with syntax errors gracefully', async () => {
        // Regression: malformed code shouldn't crash the editor
        const code = 'func broken( {{{ incomplete';
        const { go } = await import('@codemirror/lang-go');

        const state = EditorState.create({
            doc: code,
            extensions: [go(), syntaxHighlighting(defaultHighlightStyle)]
        });

        const view = new EditorView({ state, parent: container });

        // Verify initialization succeeded despite syntax errors
        expect(view).toBeDefined();
        expect(container.textContent).toContain('func');
        expect(container.textContent).toContain('broken');
    });
});
