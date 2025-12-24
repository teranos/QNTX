/**
 * Input rules for creating code blocks with ``` syntax
 */

import { Plugin } from 'prosemirror-state';
import { proseSchema } from './schema.ts';

// Plugin to handle ``` input for code blocks
export const proseInputRules = new Plugin({
    props: {
        handleKeyDown(view, event) {
            // Trigger on Enter key
            if (event.key !== 'Enter') return false;

            const { state } = view;
            const { $from } = state.selection;

            // Get text in current paragraph
            const text = $from.parent.textContent;

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
