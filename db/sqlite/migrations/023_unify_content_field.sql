-- Unify code + result_data into single content field
-- Both held "the stuff this glyph contains" — interpretation depends on symbol

ALTER TABLE canvas_glyphs ADD COLUMN content TEXT;
UPDATE canvas_glyphs SET content = COALESCE(code, result_data);
