-- Drop device_grants.
--
-- Added by 056_create_device_grants.sql for ADR-032, where one device admitted
-- another by QR scan. The feature was withdrawn nine hours later in "connect
-- device leaves, and takes a widened gate with it", which deleted the migration
-- file — but a deployment had already applied it, so the table outlived every
-- branch that knew what it was for.
--
-- Nothing in the tree names device_grants in any language. On the deployment it
-- holds no rows. Its indexes go with it.

DROP TABLE IF EXISTS device_grants;
