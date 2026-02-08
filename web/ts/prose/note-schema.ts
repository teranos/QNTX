/**
 * Lightweight ProseMirror schema for note glyphs
 *
 * Supports basic markdown formatting:
 * - Bold, italic, code (marks)
 * - Paragraphs, headings (nodes)
 * - Bullet and numbered lists (via addListNodes)
 */

import { Schema } from 'prosemirror-model';
import { schema as basicSchema } from 'prosemirror-schema-basic';
import { addListNodes } from 'prosemirror-schema-list';

// Create lightweight schema for notes
// Includes: doc, paragraph, text, heading, horizontal_rule, hard_break
// Plus: bullet_list, ordered_list, list_item (from addListNodes)
// Marks: strong (bold), em (italic), code, link
const schemaSpec = {
    nodes: addListNodes(basicSchema.spec.nodes, 'paragraph block*', 'block'),
    marks: basicSchema.spec.marks
};

export const noteSchema = new Schema(schemaSpec);
