/**
 * Markdown parser and serializer for note glyphs
 *
 * Lightweight version without frontmatter or custom code blocks
 */

import { MarkdownParser, MarkdownSerializer, defaultMarkdownSerializer } from 'prosemirror-markdown';
import { noteSchema } from './note-schema.ts';
import MarkdownIt from 'markdown-it';

// Create markdown-it tokenizer instance for note parsing
const markdownIt = MarkdownIt('commonmark', { html: false });

// Token parsing rules for note schema (uses default markdown spec)
const tokens = {
    blockquote: { block: 'blockquote' },
    paragraph: { block: 'paragraph' },
    list_item: { block: 'list_item' },
    bullet_list: { block: 'bullet_list' },
    ordered_list: { block: 'ordered_list', getAttrs: (tok: any) => ({ order: +tok.attrGet('start') || 1 }) },
    heading: { block: 'heading', getAttrs: (tok: any) => ({ level: +tok.tag.slice(1) }) },
    code_block: { block: 'code_block', noCloseToken: true },
    fence: { block: 'code_block', getAttrs: (tok: any) => ({ params: tok.info || '' }), noCloseToken: true },
    hr: { node: 'horizontal_rule' },
    image: { node: 'image', getAttrs: (tok: any) => ({ src: tok.attrGet('src'), title: tok.attrGet('title') || null, alt: tok.children?.[0]?.content || null }) },
    hardbreak: { node: 'hard_break' },
    em: { mark: 'em' },
    strong: { mark: 'strong' },
    link: { mark: 'link', getAttrs: (tok: any) => ({ href: tok.attrGet('href'), title: tok.attrGet('title') || null }) },
    code_inline: { mark: 'code', noCloseToken: true }
};

// Create markdown parser for notes with explicit tokenizer
export const noteMarkdownParser = new MarkdownParser(noteSchema, markdownIt, tokens);

// Create markdown serializer for notes
export const noteMarkdownSerializer = new MarkdownSerializer(
    defaultMarkdownSerializer.nodes,
    defaultMarkdownSerializer.marks
);
