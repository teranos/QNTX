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

// Wrap parser to transform code_block nodes with params='ats' into ats_code_block
export const proseMarkdownParser = {
    parse(content: string): PMNode {
        const doc = baseParser.parse(content);
        if (!doc) return doc;

        // Transform code_block nodes with params='ats' into ats_code_block
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
            } else {
                // Keep all other nodes as-is
                transformedContent.push(node);
            }
        });

        // Create new document with transformed nodes
        return proseSchema.node('doc', null, transformedContent);
    }
};

// Create custom markdown serializer that converts ats_code_block back to ```ats
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
        }
    },
    // Use default mark serializers
    defaultMarkdownSerializer.marks
);
