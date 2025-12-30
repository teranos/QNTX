/**
 * Custom markdown parser and serializer with ATS code block support
 */

import { MarkdownParser, MarkdownSerializer, defaultMarkdownParser, defaultMarkdownSerializer } from 'prosemirror-markdown';
import { proseSchema } from './schema.ts';
import type { Node as PMNode } from 'prosemirror-model';

// Create base markdown parser that tags ATS blocks
const baseParser = new MarkdownParser(
    proseSchema,
    // Use markdown-it tokenizer from defaultMarkdownParser
    (defaultMarkdownParser as any).tokenizer,
    {
        // Copy all default tokens
        ...(defaultMarkdownParser as any).tokens,
        // Handler for all fenced code blocks - parse as code_block with params attribute
        fence: {
            block: 'code_block',
            getAttrs: (tok: any) => ({
                // Store language in params (e.g., 'ats', 'javascript', or empty string)
                params: tok.info || ''
            }),
            noCloseToken: true
        }
    }
);

// Wrap parser to transform code_block nodes with params='ats' or 'go' into specialized blocks
export const proseMarkdownParser = {
    parse(content: string): PMNode {
        const doc = baseParser.parse(content);
        if (!doc) return doc;

        // Transform code_block nodes with params='ats' or 'go' into specialized blocks
        const transformedContent: PMNode[] = [];
        doc.forEach((node: PMNode) => {
            if (node.type.name === 'code_block' && node.attrs.params === 'ats') {
                // Convert to ats_code_block for ATS language blocks
                transformedContent.push(
                    proseSchema.nodes.ats_code_block.create(
                        { params: 'ats' },
                        node.content
                    )
                );
            } else if (node.type.name === 'code_block' && node.attrs.params === 'go') {
                // Convert to go_code_block for Go language blocks
                transformedContent.push(
                    proseSchema.nodes.go_code_block.create(
                        { params: 'go' },
                        node.content
                    )
                );
            } else {
                // Keep all other nodes as-is
                transformedContent.push(node);
            }
        });

        // Create new document with transformed nodes
        return proseSchema.node('doc', null, transformedContent);
    }
};

// Create custom markdown serializer that converts specialized code blocks back to ```lang
export const proseMarkdownSerializer = new MarkdownSerializer(
    {
        // Copy all default node serializers
        ...defaultMarkdownSerializer.nodes,
        // Add serializer for ats_code_block
        ats_code_block(state, node) {
            state.write('```' + (node.attrs.params || 'ats') + '\n');
            state.text(node.textContent, false);
            state.ensureNewLine();
            state.write('```');
            state.closeBlock(node);
        },
        // Add serializer for go_code_block
        go_code_block(state, node) {
            state.write('```' + (node.attrs.params || 'go') + '\n');
            state.text(node.textContent, false);
            state.ensureNewLine();
            state.write('```');
            state.closeBlock(node);
        }
    },
    // Use default mark serializers
    defaultMarkdownSerializer.marks
);
