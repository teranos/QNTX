/**
 * Regression test: ensure save functionality continues to work
 */

import { test, expect, mock } from 'bun:test';
import { ProseEditor } from './editor.ts';

test('can save document', async () => {
    const panel = document.createElement('div');
    panel.innerHTML = '<div id="prose-editor"></div>';

    global.fetch = mock(() =>
        Promise.resolve({
            ok: true,
            text: () => Promise.resolve('# Test'),
        } as Response)
    );

    const editor = new ProseEditor();
    editor.bindElements(panel);
    editor.setDevMode(true);

    await editor.loadDocument('test.md');
    await editor.saveContent();

    expect(editor.getCurrentPath()).toBe('test.md');
});

test('parses ATS blocks from markdown', () => {
    // Test parser directly without instantiating NodeViews
    const { proseMarkdownParser } = require('./markdown.ts');

    const doc = proseMarkdownParser.parse('```ats\nis engineer\n```');

    expect(doc.firstChild?.type.name).toBe('ats_code_block');
    expect(doc.firstChild?.textContent).toBe('is engineer');
});

test('empty backticks create regular code blocks', () => {
    // Test parser directly without instantiating NodeViews
    const { proseMarkdownParser } = require('./markdown.ts');

    const doc = proseMarkdownParser.parse('```\nplain code\n```');

    expect(doc.firstChild?.type.name).toBe('code_block');
    expect(doc.firstChild?.textContent).toBe('plain code');
});

test('code blocks with language tags preserve params attribute', () => {
    // Test that extended code_block schema supports params attribute
    const { proseMarkdownParser } = require('./markdown.ts');

    const doc = proseMarkdownParser.parse('```javascript\nconst x = 1;\n```');

    expect(doc.firstChild?.type.name).toBe('code_block');
    expect(doc.firstChild?.attrs.params).toBe('javascript');
    expect(doc.firstChild?.textContent).toBe('const x = 1;');
});

test('ATS blocks round-trip through serialization', () => {
    // Test parse → serialize preserves ATS markdown format
    const { proseMarkdownParser, proseMarkdownSerializer } = require('./markdown.ts');

    const originalMarkdown = '```ats\nis engineer\n```';
    const doc = proseMarkdownParser.parse(originalMarkdown);
    const serialized = proseMarkdownSerializer.serialize(doc);

    expect(serialized.trim()).toBe(originalMarkdown.trim());
});
