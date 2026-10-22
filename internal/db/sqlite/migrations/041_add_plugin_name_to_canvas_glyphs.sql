-- Add plugin_name to canvas_glyphs to identify plugin-provided glyphs
-- Empty string = built-in glyph (default)
ALTER TABLE canvas_glyphs ADD COLUMN plugin_name TEXT NOT NULL DEFAULT '';
