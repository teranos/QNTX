-- Record what a migration was, not only that its number ran.
--
-- The migrator keyed on the version string alone, so a number applied to a box
-- by one branch was skipped in silence by every other — no run, no log, no
-- error, and a fresh database would diverge from the deployment with both
-- looking correct.
--
-- A number cannot be checked against the tree: a migration can be applied and
-- then deleted from every branch, which is how 056 was burned. The content is
-- the only thing that can say whether the file claiming a version is the file
-- that ran. Existing rows are backfilled on the next boot.

ALTER TABLE schema_migrations ADD COLUMN checksum TEXT;
