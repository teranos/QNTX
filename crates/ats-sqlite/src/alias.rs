//! Alias storage for the SQLite backend.
//!
//! Aliases live in the `aliases` table (migration 009, recreated with
//! `COLLATE NOCASE` on both columns by migration 012). Both directions of every
//! pair are stored as rows, so resolution is a single indexed lookup rather than
//! a union of forward and reverse queries.
//!
//! Because the columns declare `COLLATE NOCASE`, comparisons are
//! case-insensitive through the index, and the `(alias, target)` primary key
//! treats `"Weave"/"weave"` as one pair. No query here needs an inline
//! `COLLATE NOCASE` — migration 012 exists precisely so they don't, since an
//! inline clause bypasses the index.
//!
//! This replaces `ats/storage/alias_store.go`.

use std::collections::HashMap;

use ats::storage::{AliasStore, StoreError};
use rusqlite::Connection;

use crate::error::SqliteError;
use crate::store::SqliteStore;

type StoreResult<T> = Result<T, StoreError>;

impl AliasStore for SqliteStore {
    fn resolve_alias(&self, identifier: &str) -> StoreResult<Vec<String>> {
        resolve(&self.conn, identifier)
    }

    fn create_alias(&mut self, alias: &str, target: &str, created_by: &str) -> StoreResult<()> {
        validate_pair(alias, target)?;

        // Both directions in one transaction: an alias that resolves one way and
        // not the other is not an alias.
        let tx = self.conn.transaction().map_err(SqliteError::from)?;

        for (from, to) in [(alias, target), (target, alias)] {
            tx.execute(
                "INSERT OR IGNORE INTO aliases (alias, target, created_by)
                 VALUES (?1, ?2, ?3)",
                rusqlite::params![from, to, created_by],
            )
            .map_err(SqliteError::from)?;
        }

        tx.commit().map_err(SqliteError::from)?;
        Ok(())
    }

    fn remove_alias(&mut self, alias: &str, target: &str) -> StoreResult<()> {
        validate_endpoints(alias, target)?;

        self.conn
            .execute(
                "DELETE FROM aliases
                 WHERE (alias = ?1 AND target = ?2)
                    OR (alias = ?2 AND target = ?1)",
                rusqlite::params![alias, target],
            )
            .map_err(SqliteError::from)?;

        Ok(())
    }

    fn all_aliases(&self) -> StoreResult<HashMap<String, Vec<String>>> {
        let mut stmt = self
            .conn
            .prepare("SELECT alias, target FROM aliases ORDER BY alias, target")
            .map_err(SqliteError::from)?;

        let rows = stmt
            .query_map([], |row| {
                Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?))
            })
            .map_err(SqliteError::from)?;

        let mut out: HashMap<String, Vec<String>> = HashMap::new();
        for row in rows {
            let (alias, target) = row.map_err(SqliteError::from)?;
            out.entry(alias).or_default().push(target);
        }

        Ok(out)
    }
}

/// Every identifier equivalent to `identifier`, including itself.
fn resolve(conn: &Connection, identifier: &str) -> StoreResult<Vec<String>> {
    let mut stmt = conn
        .prepare("SELECT target FROM aliases WHERE alias = ?1")
        .map_err(SqliteError::from)?;

    let rows = stmt
        .query_map([identifier], |row| row.get::<_, String>(0))
        .map_err(SqliteError::from)?;

    let mut out = Vec::new();
    for row in rows {
        out.push(row.map_err(SqliteError::from)?);
    }

    // The identifier itself always resolves, aliased or not.
    out.push(identifier.to_string());

    out.sort();
    out.dedup();
    Ok(out)
}

fn validate_endpoints(alias: &str, target: &str) -> StoreResult<()> {
    if alias.is_empty() {
        return Err(StoreError::InvalidData("alias cannot be empty".to_string()));
    }
    if target.is_empty() {
        return Err(StoreError::InvalidData(
            "target cannot be empty".to_string(),
        ));
    }
    Ok(())
}

fn validate_pair(alias: &str, target: &str) -> StoreResult<()> {
    validate_endpoints(alias, target)?;
    if alias.eq_ignore_ascii_case(target) {
        return Err(StoreError::InvalidData(format!(
            "alias and target cannot be identical: {alias} and {target}"
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn store() -> SqliteStore {
        SqliteStore::in_memory().expect("in-memory store")
    }

    #[test]
    fn an_unaliased_identifier_resolves_to_itself() {
        let s = store();
        assert_eq!(s.resolve_alias("ALICE").unwrap(), vec!["ALICE"]);
    }

    #[test]
    fn an_alias_resolves_in_both_directions() {
        let mut s = store();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();

        assert_eq!(
            s.resolve_alias("ALICE").unwrap(),
            vec!["ALICE", "alice@example.com"]
        );
        assert_eq!(
            s.resolve_alias("alice@example.com").unwrap(),
            vec!["ALICE", "alice@example.com"]
        );
    }

    #[test]
    fn lookup_ignores_case_and_echoes_the_spelling_asked_for() {
        let mut s = store();
        s.create_alias("Weave", "WV", "test").unwrap();

        // The alias column matches case-insensitively, so the row is found. The
        // identifier itself comes back as the caller spelled it, not as stored —
        // it is appended, not read from the table. Go does the same: its
        // `UNION SELECT ?` re-selects the caller's own parameter.
        //
        // Harmless downstream: the junction tables are `COLLATE NOCASE` too
        // (migration 050), so either spelling matches the same attestations.
        assert_eq!(s.resolve_alias("weave").unwrap(), vec!["WV", "weave"]);
        assert_eq!(s.resolve_alias("WEAVE").unwrap(), vec!["WEAVE", "WV"]);

        // Reached from the other side, the stored spelling is what surfaces.
        assert_eq!(s.resolve_alias("wv").unwrap(), vec!["Weave", "wv"]);
    }

    #[test]
    fn one_identifier_can_carry_several_aliases() {
        let mut s = store();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();
        s.create_alias("ALICE", "a.smith", "test").unwrap();

        assert_eq!(
            s.resolve_alias("ALICE").unwrap(),
            vec!["ALICE", "a.smith", "alice@example.com"]
        );
    }

    #[test]
    fn creating_the_same_alias_twice_is_not_an_error() {
        let mut s = store();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();

        assert_eq!(s.resolve_alias("ALICE").unwrap().len(), 2);
    }

    #[test]
    fn an_alias_to_itself_is_refused() {
        let mut s = store();

        assert!(matches!(
            s.create_alias("ALICE", "ALICE", "test"),
            Err(StoreError::InvalidData(_))
        ));
        assert!(
            matches!(
                s.create_alias("ALICE", "alice", "test"),
                Err(StoreError::InvalidData(_))
            ),
            "case does not make it a different identifier"
        );
    }

    #[test]
    fn an_empty_endpoint_is_refused() {
        let mut s = store();

        assert!(matches!(
            s.create_alias("", "ALICE", "test"),
            Err(StoreError::InvalidData(_))
        ));
        assert!(matches!(
            s.create_alias("ALICE", "", "test"),
            Err(StoreError::InvalidData(_))
        ));
    }

    #[test]
    fn removing_an_alias_removes_both_directions() {
        let mut s = store();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();
        s.remove_alias("alice@example.com", "ALICE").unwrap();

        assert_eq!(s.resolve_alias("ALICE").unwrap(), vec!["ALICE"]);
        assert_eq!(
            s.resolve_alias("alice@example.com").unwrap(),
            vec!["alice@example.com"]
        );
    }

    #[test]
    fn removing_an_alias_that_does_not_exist_is_not_an_error() {
        let mut s = store();
        s.remove_alias("ALICE", "nobody").unwrap();
    }

    #[test]
    fn all_aliases_lists_both_directions() {
        let mut s = store();
        s.create_alias("ALICE", "alice@example.com", "test")
            .unwrap();

        let all = s.all_aliases().unwrap();
        assert_eq!(
            all.get("ALICE"),
            Some(&vec!["alice@example.com".to_string()])
        );
        assert_eq!(
            all.get("alice@example.com"),
            Some(&vec!["ALICE".to_string()])
        );
    }
}
