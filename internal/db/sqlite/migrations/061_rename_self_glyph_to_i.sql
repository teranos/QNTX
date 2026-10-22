-- ⍟'s word is `i`, and the id it was written down under was `self-glyph`.
-- Nothing is called self afterwards, so every place that recorded the id says
-- the word instead.

-- These are the three tables a glyph id is still kept in. Migration 021
-- replaced the junction table with edges, and 047 recreated those without the
-- glyph foreign keys, so nothing references canvas_glyphs(id) any more and the
-- rename is three writes and no cascade.
UPDATE canvas_glyphs     SET id            = 'i-glyph' WHERE id            = 'self-glyph';
UPDATE composition_edges SET from_glyph_id = 'i-glyph' WHERE from_glyph_id = 'self-glyph';
UPDATE composition_edges SET to_glyph_id   = 'i-glyph' WHERE to_glyph_id   = 'self-glyph';
UPDATE minimized_windows SET glyph_id      = 'i-glyph' WHERE glyph_id      = 'self-glyph';
