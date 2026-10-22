-- Add canvas_id to support nested canvas workspaces (subcanvas)
-- Empty string = root canvas (backward compatible)
ALTER TABLE canvas_glyphs ADD COLUMN canvas_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_canvas_glyphs_canvas_id ON canvas_glyphs(canvas_id);
