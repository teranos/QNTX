-- A User lives in the operational db (ADR-037). Every gated request read the
-- User from S3, one LIST and one GET per User, under one lock for the node.
--
-- "we need to keep users in mem"
-- "why not just implement the pending operational db work"
--
-- The row is the record as the node writes it, so this table and the object
-- on S3 hold the same bytes. S3 is the record for host loss, not for reading.
CREATE TABLE users (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    record TEXT NOT NULL
);
