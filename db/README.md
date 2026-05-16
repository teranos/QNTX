# QNTX Database Package

**⊔** Material retention substrate for QNTX.

## Why SQLite?

**QNTX should work out of the box.** Run a binary on your laptop and off you go - no database servers, no configuration, no deployment complexity.

SQLite fits this philosophy: single file, no dependencies, runs anywhere. This aligns with the "almost no configuration required" ideal.

## Why `db/sqlite/` Structure?

Future vision: QNTX running on AWS for 2-3 hours daily, writing attestations to DynamoDB or S3, then local QNTX pulls them into local SQLite.

Hybrid cloud/local setups need multiple backend support. The directory structure keeps that door open.

## Usage

**Preferred: `rustsqlite` driver** — Rust owns the database. New connections go through `rustsqlite`:

```go
import "database/sql"

db, err := sql.Open("rustsqlite", dbPath)
db.SetMaxOpenConns(4)
```

See `server/watcher_handlers.go` (watcherDB) and `server/init.go` (pulseReadDB) for examples.

**Legacy: Go `db.Open()`** — Uses `mattn/go-sqlite3` with `_txlock=immediate`. Being phased out.

```go
import "github.com/teranos/QNTX/db"

database, err := db.Open("path/to/db.sqlite", logger)
db.Migrate(database, logger)
```

## Migrations

Located in `db/sqlite/migrations/`, named `NNN_description.sql`. Run via `db.Migrate()`.

---

TODO: INTERNAL: Move db/ package to internal.
