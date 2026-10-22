-- Add attribute_filters column to watchers for JSON attribute matching
ALTER TABLE watchers ADD COLUMN attribute_filters JSON;
