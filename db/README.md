# QNTX Database Package

**⊔** Material retention substrate for QNTX.

Directory structure (`db/sqlite/`) allows future backends without breaking changes.

## Usage

```go
import "github.com/teranos/QNTX/db"

// Open database connection
database, err := db.Open("path/to/db.sqlite", logger)
if err != nil {
    return err
}

// Run migrations (required for schema setup)
if err := db.Migrate(database, logger); err != nil {
    return err
}
```

## Migrations

- Located in `db/sqlite/migrations/`
- Named `NNN_description.sql` (zero-padded sequential)
- Must be run explicitly via `db.Migrate()`
- Forward-only (no rollback)
- Bootstrap migration `000` creates `schema_migrations` tracking table
