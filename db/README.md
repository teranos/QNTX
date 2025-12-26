# QNTX Database Package

**⊔** Material retention substrate for QNTX.

Directory structure (`db/sqlite/`) allows future backends without breaking changes.

## Migrations

- Located in `db/sqlite/migrations/`
- Named `NNN_description.sql` (zero-padded sequential)
- Run automatically on `db.Open()`
- Forward-only (no rollback)
- Bootstrap migration `000` creates `schema_migrations` tracking table
