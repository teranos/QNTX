/**
 * Input rules for creating code blocks with ``` syntax and frontmatter with ---
 */

import { Plugin } from 'prosemirror-state';
import { proseSchema } from './schema.ts';

// Plugin to handle ``` input for code blocks and --- for frontmatter
export const proseInputRules = new Plugin({
    props: {
        handleKeyDown(view, event) {
            // Trigger on Enter key
            if (event.key !== 'Enter') return false;

            const { state } = view;
            const { $from } = state.selection;

            // Get text in current paragraph
            const text = $from.parent.textContent;

            // Check if line starts with --- for frontmatter
            if (text === '---') {
                // Only create frontmatter if:
                // 1. We're at the document start (first node)
                // 2. No frontmatter block already exists

                // Check if current node is the first block in the document
                const isAtDocStart = $from.index(0) === 0;
                if (!isAtDocStart) {
                    return false;
                }

                // Check if document already has a frontmatter block
                let hasFrontmatter = false;
                state.doc.forEach((node) => {
                    if (node.type.name === 'frontmatter_block') {
                        hasFrontmatter = true;
                    }
                });

                if (hasFrontmatter) {
                    return false;
                }

                // Create frontmatter block with a placeholder text
                const frontmatterNode = proseSchema.nodes.frontmatter_block.create(
                    { params: 'yaml' },
                    proseSchema.text('# Add your YAML frontmatter here')
                );

                const tr = state.tr.replaceRangeWith(
                    $from.before(),
                    $from.after(),
                    frontmatterNode
                );
                view.dispatch(tr);
                return true;
            }

            // Check if line starts with ``` followed by optional language
            if (text.startsWith('```')) {
                const lang = text.slice(3).trim();

                // Create appropriate node type
                let nodeType, attrs;
                if (lang === 'ats') {
                    // Create ATS block only for ```ats
                    nodeType = proseSchema.nodes.ats_code_block;
                    attrs = { params: 'ats' };
                } else {
                    // Regular code block for ``` or ```<other-lang>
                    nodeType = proseSchema.nodes.code_block;
                    attrs = {};
                }

                const tr = state.tr.replaceRangeWith(
                    $from.before(),
                    $from.after(),
                    nodeType.create(attrs)
                );
                view.dispatch(tr);
                return true;
            }

            return false;
        }
    }
});
