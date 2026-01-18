/**
 * Custom ProseMirror schema with ATS code block support
 */

import { Schema } from 'prosemirror-model';
import { schema as basicSchema } from 'prosemirror-schema-basic';
import { addListNodes } from 'prosemirror-schema-list';

// Extend basic schema with frontmatter_block, ats_code_block and go_code_block nodes
const schemaSpec = {
    nodes: addListNodes(basicSchema.spec.nodes, 'paragraph block*', 'block')
        .addBefore('code_block', 'frontmatter_block', {
            content: 'text*',
            marks: '',
            group: 'block',
            code: true,
            defining: true,
            attrs: {
                params: { default: 'yaml' }
            },
            parseDOM: [{
                tag: 'div[data-type="frontmatter"]',
                preserveWhitespace: 'full',
                getAttrs: (node) => ({
                    params: (node as HTMLElement).getAttribute('data-params') || 'yaml'
                })
            }],
            toDOM: (node) => ['div', {
                'data-type': 'frontmatter',
                'data-params': node.attrs.params
            }, ['pre', 0]]
        })
        .addBefore('code_block', 'ats_code_block', {
            content: 'text*',
            marks: '',
            group: 'block',
            code: true,
            defining: true,
            attrs: {
                params: { default: 'ats' },
                scheduledJobId: { default: null }
            },
            parseDOM: [{
                tag: 'pre[data-language="ats"]',
                preserveWhitespace: 'full',
                getAttrs: (node) => ({
                    params: (node as HTMLElement).getAttribute('data-language') || 'ats',
                    scheduledJobId: (node as HTMLElement).getAttribute('data-scheduled-job-id') || null
                })
            }],
            toDOM: (node) => ['pre', {
                'data-language': node.attrs.params,
                'data-scheduled-job-id': node.attrs.scheduledJobId
            }, ['code', 0]]
        })
        .addBefore('code_block', 'go_code_block', {
            content: 'text*',
            marks: '',
            group: 'block',
            code: true,
            defining: true,
            attrs: {
                params: { default: 'go' }
            },
            parseDOM: [{
                tag: 'pre[data-language="go"]',
                preserveWhitespace: 'full',
                getAttrs: (node) => ({
                    params: (node as HTMLElement).getAttribute('data-language') || 'go'
                })
            }],
            toDOM: (node) => ['pre', {
                'data-language': node.attrs.params
            }, ['code', 0]]
        })
        .update('code_block', {
            ...basicSchema.spec.nodes.get('code_block'),
            attrs: {
                params: { default: '' }
            }
        }),
    marks: basicSchema.spec.marks
};

export const proseSchema = new Schema(schemaSpec);
