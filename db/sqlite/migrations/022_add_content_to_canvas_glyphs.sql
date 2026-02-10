-- Add content field to canvas_glyphs
-- Stores glyph content inline: source code, markdown, prompt template, or JSON result
-- Interpretation depends on the glyph's symbol type

ALTER TABLE canvas_glyphs ADD COLUMN content TEXT;
