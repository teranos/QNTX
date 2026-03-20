# ADR-013: Rust as Sole SQLite Owner

Date: 2026-03-18
Status: Implemented

## Context

QNTX currently has two SQLite drivers in the same process: Rust's `rusqlite` (for attestations) and Go's `mattn/go-sqlite3` (for everything else). Both open the same database file independently. This dual-driver architecture is the likely cause of `SQLITE_CORRUPT` errors — two separate copies of `sqlite3.c` managing WAL state on the same file.

## Decision

Rust owns the database. All SQL — from Go or Rust — goes through a single `rusqlite` connection.

Go's 96 callsites keep their SQL unchanged. A custom `database/sql/driver` routes Go's `Query`, `Exec`, and `Begin` calls through Rust via FFI. Go stops linking `mattn/go-sqlite3`.

sqlite-vec is not a blocker: the Rust side already loads it (`crates/qntx-sqlite/src/vec.rs`), with passing tests for `vec0` virtual tables and `vec_distance_l2`.

## Consequences

- One copy of `sqlite3.c`, one WAL, one lock strategy
- Corruption from dual-driver contention is eliminated
- All pragmas, busy_timeout, and journal_mode are set once in Rust
