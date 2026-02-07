/**
 * Markdown parser and serializer for note glyphs
 *
 * Lightweight version without frontmatter or custom code blocks
 */

import { MarkdownParser, MarkdownSerializer, defaultMarkdownParser, defaultMarkdownSerializer } from 'prosemirror-markdown';
import { noteSchema } from './note-schema.ts';

// Create markdown parser for notes
export const noteMarkdownParser = new MarkdownParser(
    noteSchema,
    // Use markdown-it tokenizer from defaultMarkdownParser
    (defaultMarkdownParser as any).tokenizer,
    // Use default tokens (supports headings, lists, bold, italic, etc.)
    (defaultMarkdownParser as any).tokens
);

// Create markdown serializer for notes
export const noteMarkdownSerializer = new MarkdownSerializer(
    defaultMarkdownSerializer.nodes,
    defaultMarkdownSerializer.marks
);
