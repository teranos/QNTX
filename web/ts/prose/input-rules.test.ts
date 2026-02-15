/**
 * Tests for ProseMirror input rules (frontmatter and code block auto-conversion)
 */

import { test, expect } from 'bun:test';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { proseSchema } from './schema.ts';
import { proseInputRules } from './input-rules.ts';

test('--- at document start creates frontmatter block', () => {
    const container = document.createElement('div');

    const state = EditorState.create({
        doc: proseSchema.node('doc', null, [
            proseSchema.node('paragraph', null, [proseSchema.text('---')])
        ]),
        plugins: [proseInputRules]
    });

    const view = new EditorView(container, { state });

    // Simulate Enter key press by directly calling the handler
    const event = new window.KeyboardEvent('keydown', { key: 'Enter' }) as any;
    const handler = (proseInputRules.props as any).handleKeyDown;
    const handled = handler(view, event);

    expect(handled).toBe(true);
    // Check that paragraph was replaced with frontmatter block
    expect(view.state.doc.firstChild?.type.name).toBe('frontmatter_block');
});

test('--- in middle of document does not create frontmatter', () => {
    const container = document.createElement('div');

    const state = EditorState.create({
        doc: proseSchema.node('doc', null, [
            proseSchema.node('paragraph', null, [proseSchema.text('Some text')]),
            proseSchema.node('paragraph', null, [proseSchema.text('---')])
        ]),
        plugins: [proseInputRules]
    });

    const view = new EditorView(container, { state });

    // Move selection to second paragraph
    const tr = view.state.tr.setSelection(
        view.state.selection.constructor.near(view.state.doc.resolve(13))
    );
    view.dispatch(tr);

    // Simulate Enter key press
    const event = new window.KeyboardEvent('keydown', { key: 'Enter' }) as any;
    const handler = (proseInputRules.props as any).handleKeyDown;
    const handled = handler(view, event);

    expect(handled).toBe(false);
    // Check that second node is still a paragraph
    expect(view.state.doc.child(1).type.name).toBe('paragraph');
});

test('--- does not create frontmatter when one already exists', () => {
    const container = document.createElement('div');

    const state = EditorState.create({
        doc: proseSchema.node('doc', null, [
            proseSchema.nodes.frontmatter_block.create(
                { params: 'yaml' },
                proseSchema.text('name: test')
            ),
            proseSchema.node('paragraph', null, [proseSchema.text('---')])
        ]),
        plugins: [proseInputRules]
    });

    const view = new EditorView(container, { state });

    // Move selection to second node
    const tr = view.state.tr.setSelection(
        view.state.selection.constructor.near(view.state.doc.resolve(13))
    );
    view.dispatch(tr);

    // Simulate Enter key press
    const event = new window.KeyboardEvent('keydown', { key: 'Enter' }) as any;
    const handler = (proseInputRules.props as any).handleKeyDown;
    const handled = handler(view, event);

    expect(handled).toBe(false);
    // Check that second node is still a paragraph (not converted)
    expect(view.state.doc.child(1).type.name).toBe('paragraph');

    // Verify only one frontmatter block exists
    let frontmatterCount = 0;
    view.state.doc.forEach((node) => {
        if (node.type.name === 'frontmatter_block') {
            frontmatterCount++;
        }
    });
    expect(frontmatterCount).toBe(1);
});

test('```ats at any position creates ATS code block', () => {
    const container = document.createElement('div');

    const state = EditorState.create({
        doc: proseSchema.node('doc', null, [
            proseSchema.node('paragraph', null, [proseSchema.text('```ats')])
        ]),
        plugins: [proseInputRules]
    });

    const view = new EditorView(container, { state });

    // Simulate Enter key press
    const event = new window.KeyboardEvent('keydown', { key: 'Enter' }) as any;
    const handler = (proseInputRules.props as any).handleKeyDown;
    const handled = handler(view, event);

    expect(handled).toBe(true);
    // Check that paragraph was replaced with ats_code_block
    expect(view.state.doc.firstChild?.type.name).toBe('ats_code_block');
    expect(view.state.doc.firstChild?.attrs.params).toBe('ats');
});

test('``` without language creates regular code block', () => {
    const container = document.createElement('div');

    const state = EditorState.create({
        doc: proseSchema.node('doc', null, [
            proseSchema.node('paragraph', null, [proseSchema.text('```')])
        ]),
        plugins: [proseInputRules]
    });

    const view = new EditorView(container, { state });

    // Simulate Enter key press
    const event = new window.KeyboardEvent('keydown', { key: 'Enter' }) as any;
    const handler = (proseInputRules.props as any).handleKeyDown;
    const handled = handler(view, event);

    expect(handled).toBe(true);
    // Check that paragraph was replaced with code_block
    expect(view.state.doc.firstChild?.type.name).toBe('code_block');
});
