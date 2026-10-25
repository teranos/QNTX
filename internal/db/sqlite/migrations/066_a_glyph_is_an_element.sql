-- "i want to let go of the old term and have element be the thing"
-- Every table and column that said glyph says element. The ids the code names
-- itself, like i-glyph, say it too; ids the code minted stay as they were.

ALTER TABLE canvas_glyphs RENAME TO canvas_elements;
DROP INDEX IF EXISTS idx_canvas_glyphs_canvas_id;
CREATE INDEX idx_canvas_elements_canvas_id ON canvas_elements(canvas_id);

ALTER TABLE composition_edges RENAME COLUMN from_glyph_id TO from_element_id;
ALTER TABLE composition_edges RENAME COLUMN to_glyph_id TO to_element_id;
ALTER TABLE composition_edge_cursors RENAME COLUMN from_glyph_id TO from_element_id;
ALTER TABLE composition_edge_cursors RENAME COLUMN to_glyph_id TO to_element_id;
ALTER TABLE minimized_windows RENAME COLUMN glyph_id TO element_id;

-- A fixed id ends in -glyph: i-glyph, plugin-glyph, tokens-glyph.
UPDATE canvas_elements SET id = substr(id, 1, length(id) - 5) || 'element' WHERE id LIKE '%-glyph';
UPDATE composition_edges SET from_element_id = substr(from_element_id, 1, length(from_element_id) - 5) || 'element' WHERE from_element_id LIKE '%-glyph';
UPDATE composition_edges SET to_element_id = substr(to_element_id, 1, length(to_element_id) - 5) || 'element' WHERE to_element_id LIKE '%-glyph';
UPDATE composition_edge_cursors SET from_element_id = substr(from_element_id, 1, length(from_element_id) - 5) || 'element' WHERE from_element_id LIKE '%-glyph';
UPDATE composition_edge_cursors SET to_element_id = substr(to_element_id, 1, length(to_element_id) - 5) || 'element' WHERE to_element_id LIKE '%-glyph';
UPDATE minimized_windows SET element_id = substr(element_id, 1, length(element_id) - 5) || 'element' WHERE element_id LIKE '%-glyph';

-- A watcher that runs an element on a meld edge.
UPDATE watchers SET action_type = 'element_execute' WHERE action_type = 'glyph_execute';
UPDATE watchers SET action_data = json_remove(json_set(action_data, '$.target_element_id', json_extract(action_data, '$.target_glyph_id')), '$.target_glyph_id')
    WHERE json_valid(action_data) AND json_extract(action_data, '$.target_glyph_id') IS NOT NULL;
UPDATE watchers SET action_data = json_remove(json_set(action_data, '$.target_element_type', json_extract(action_data, '$.target_glyph_type')), '$.target_glyph_type')
    WHERE json_valid(action_data) AND json_extract(action_data, '$.target_glyph_type') IS NOT NULL;
UPDATE watchers SET action_data = json_remove(json_set(action_data, '$.source_element_id', json_extract(action_data, '$.source_glyph_id')), '$.source_glyph_id')
    WHERE json_valid(action_data) AND json_extract(action_data, '$.source_glyph_id') IS NOT NULL;
