/**
 * Tests for markdown parser and serializer
 */

import { test, expect } from 'bun:test';
import { proseMarkdownParser, proseMarkdownSerializer } from './markdown.ts';

test('transforms only ATS blocks in mixed documents', () => {
    const markdown = `# Heading

Regular paragraph text.

\`\`\`javascript
const x = 1;
\`\`\`

\`\`\`ats
is engineer
\`\`\`

\`\`\`
plain code
\`\`\``;

    const doc = proseMarkdownParser.parse(markdown);

    // Should have 5 blocks: heading, paragraph, javascript code, ats code, plain code
    expect(doc.childCount).toBe(5);

    // Check each block type
    expect(doc.child(0).type.name).toBe('heading');
    expect(doc.child(1).type.name).toBe('paragraph');
    expect(doc.child(2).type.name).toBe('code_block');
    expect(doc.child(2).attrs.params).toBe('javascript');
    expect(doc.child(3).type.name).toBe('ats_code_block');  // Only this one transformed
    expect(doc.child(4).type.name).toBe('code_block');
    expect(doc.child(4).attrs.params).toBe('');
});
