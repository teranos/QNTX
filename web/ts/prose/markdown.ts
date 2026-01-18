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

/**
 * Extract YAML frontmatter from markdown content
 * Returns { frontmatter: string | null, content: string }
 */
function extractFrontmatter(content: string): { frontmatter: string | null, content: string } {
    // Check if content starts with ---
    if (!content.trimStart().startsWith('---\n')) {
        return { frontmatter: null, content };
    }

    // Find the closing ---
    const lines = content.split('\n');
    let endIndex = -1;
    for (let i = 1; i < lines.length; i++) {
        if (lines[i].trim() === '---') {
            endIndex = i;
            break;
        }
    }

    // No closing ---, treat as regular content
    if (endIndex === -1) {
        return { frontmatter: null, content };
    }

    // Extract frontmatter (lines between the --- markers)
    const frontmatter = lines.slice(1, endIndex).join('\n');
    // Remaining content (after the closing ---)
    const remainingContent = lines.slice(endIndex + 1).join('\n');

    return { frontmatter, content: remainingContent };
}

// Wrap parser to extract frontmatter and transform specialized code blocks
export const proseMarkdownParser = {
    parse(content: string): PMNode {
        // Extract frontmatter if present
        const { frontmatter, content: bodyContent } = extractFrontmatter(content);

        // Parse the body content (without frontmatter)
        const doc = baseParser.parse(bodyContent);
        if (!doc) return doc;

        // Transform code_block nodes with params='ats' or 'go' into specialized blocks
        const transformedContent: PMNode[] = [];

        // Add frontmatter block as first node if frontmatter exists
        if (frontmatter) {
            transformedContent.push(
                proseSchema.nodes.frontmatter_block.create(
                    { params: 'yaml' },
                    proseSchema.text(frontmatter)
                )
            );
        }

        // Transform and add body nodes
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
        // Add serializer for frontmatter_block
        frontmatter_block(state, node) {
            state.write('---\n');
            state.text(node.textContent, false);
            state.ensureNewLine();
            state.write('---');
            state.closeBlock(node);
        },
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
