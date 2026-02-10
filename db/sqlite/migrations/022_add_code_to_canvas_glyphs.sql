-- Add code field to canvas_glyphs to store script content inline
-- This eliminates the need for separate localStorage script storage
-- and enables proper backend sync of script content

ALTER TABLE canvas_glyphs ADD COLUMN code TEXT;
