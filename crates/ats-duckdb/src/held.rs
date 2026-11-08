//! A DuckDB a subsystem holds for itself (ADR-056): its own tables, in one
//! file of the node's, held in memory while the node runs and kept in that file
//! across restarts. What the tables are is the subsystem's, made by its own
//! migrations; this opens the file, applies them, runs SQL on it, and answers
//! rows as JSON.

use duckdb::types::Value;
use serde_json::{Map, Number, Value as Json};

use crate::error::{DuckdbError, Name, Object, Refusal, Result};

/// One DuckDB file, held open.
pub struct HeldDb {
    path: String,
    conn: duckdb::Connection,
}

impl HeldDb {
    /// Open the DuckDB at `path`, making it when there is none, and apply the
    /// subsystem's `migrations`, each a version and its SQL, those not yet
    /// applied, in order.
    pub fn open(path: impl Into<String>, migrations: &[(String, String)]) -> Result<Self> {
        let path = path.into();
        if path.contains('\'') {
            return Err(DuckdbError::BadName {
                which: Name::Location,
                value: path,
                why: Refusal::CarriesAQuote,
            });
        }
        if let Some(dir) = std::path::Path::new(&path).parent() {
            std::fs::create_dir_all(dir).map_err(|source| DuckdbError::WriteFile {
                what: Object::Held,
                path: dir.display().to_string(),
                source,
            })?;
        }
        let conn = duckdb::Connection::open(&path).map_err(|source| DuckdbError::Read {
            what: Object::Held,
            under: path.clone(),
            source,
        })?;
        crate::assert_library_version(&conn)?;
        crate::migrate::migrate_held(&conn, migrations)?;
        Ok(Self { path, conn })
    }

    /// The file this DuckDB is kept in.
    pub fn path(&self) -> &str {
        &self.path
    }

    /// Run statements that answer no rows, with `params` bound in order. With
    /// no params, `sql` may hold several statements.
    pub fn exec(&self, sql: &str, params: &[Json]) -> Result<usize> {
        let wrote = |source| DuckdbError::Write {
            what: Object::Held,
            path: self.path.clone(),
            source,
        };
        if params.is_empty() {
            return self.conn.execute_batch(sql).map(|()| 0).map_err(wrote);
        }
        let bound = bind(params)?;
        self.conn
            .execute(sql, duckdb::params_from_iter(bound.iter()))
            .map_err(wrote)
    }

    /// The rows `sql` answers, with `params` bound in order, each row an
    /// object by column name.
    pub fn query(&self, sql: &str, params: &[Json]) -> Result<Vec<Map<String, Json>>> {
        let read = |source| DuckdbError::Read {
            what: Object::Held,
            under: self.path.clone(),
            source,
        };
        let bound = bind(params)?;
        let mut stmt = self.conn.prepare(sql).map_err(read)?;
        let mut rows = stmt
            .query(duckdb::params_from_iter(bound.iter()))
            .map_err(read)?;
        let names = match rows.as_ref() {
            Some(stmt) => stmt.column_names(),
            None => Vec::new(),
        };
        let mut out = Vec::new();
        while let Some(row) = rows.next().map_err(read)? {
            let mut object = Map::with_capacity(names.len());
            for (i, name) in names.iter().enumerate() {
                let value: Value = row.get(i).map_err(read)?;
                object.insert(name.clone(), json_of(value)?);
            }
            out.push(object);
        }
        Ok(out)
    }
}

/// A JSON parameter as the value DuckDB binds: null, a bool, a number, or a
/// string. A list or an object is no parameter.
fn bind(params: &[Json]) -> Result<Vec<Value>> {
    params
        .iter()
        .map(|p| match p {
            Json::Null => Ok(Value::Null),
            Json::Bool(b) => Ok(Value::Boolean(*b)),
            Json::Number(n) => match (n.as_i64(), n.as_f64()) {
                (Some(i), _) => Ok(Value::BigInt(i)),
                (None, Some(f)) => Ok(Value::Double(f)),
                (None, None) => Err(not_bindable(p)),
            },
            Json::String(s) => Ok(Value::Text(s.clone())),
            Json::Array(_) | Json::Object(_) => Err(not_bindable(p)),
        })
        .collect()
}

fn not_bindable(p: &Json) -> DuckdbError {
    DuckdbError::ColumnShape {
        expected: "a parameter that is null, a bool, a number or a string",
        got: Value::Text(p.to_string()),
    }
}

/// A DuckDB value as JSON. A value JSON has no word for — a timestamp, a
/// blob, a decimal — is refused, for the SQL to cast to one it has.
fn json_of(value: Value) -> Result<Json> {
    Ok(match value {
        Value::Null => Json::Null,
        Value::Boolean(b) => Json::Bool(b),
        Value::TinyInt(i) => Json::from(i),
        Value::SmallInt(i) => Json::from(i),
        Value::Int(i) => Json::from(i),
        Value::BigInt(i) => Json::from(i),
        Value::UTinyInt(i) => Json::from(i),
        Value::USmallInt(i) => Json::from(i),
        Value::UInt(i) => Json::from(i),
        Value::UBigInt(i) => Json::from(i),
        Value::HugeInt(i) => match i64::try_from(i) {
            Ok(i) => Json::from(i),
            Err(_) => return Err(not_json(Value::HugeInt(i))),
        },
        Value::Float(f) => float(f64::from(f), Value::Float(f))?,
        Value::Double(f) => float(f, Value::Double(f))?,
        Value::Text(s) | Value::Enum(s) => Json::String(s),
        Value::List(items) | Value::Array(items) => {
            Json::Array(items.into_iter().map(json_of).collect::<Result<Vec<_>>>()?)
        }
        other => return Err(not_json(other)),
    })
}

fn float(f: f64, value: Value) -> Result<Json> {
    Number::from_f64(f)
        .map(Json::Number)
        .ok_or_else(|| not_json(value))
}

fn not_json(got: Value) -> DuckdbError {
    DuckdbError::ColumnShape {
        expected: "a value JSON carries; cast it to one in the SQL",
        got,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn at(dir: &tempfile::TempDir) -> String {
        dir.path()
            .join("subsystems")
            .join("quotes.duckdb")
            .display()
            .to_string()
    }

    /// A subsystem's migrations, as it would embed them.
    fn reads() -> Vec<(String, String)> {
        vec![(
            "quotes-001".to_string(),
            "CREATE TABLE reads (id VARCHAR PRIMARY KEY, doc VARCHAR, read_at BIGINT);".to_string(),
        )]
    }

    fn open(dir: &tempfile::TempDir) -> HeldDb {
        HeldDb::open(at(dir), &reads()).unwrap()
    }

    #[test]
    fn what_is_written_is_there_after_reopening() {
        let dir = tempfile::tempdir().unwrap();
        {
            let held = open(&dir);
            held.exec(
                "INSERT INTO reads VALUES (?, ?, ?)",
                &[
                    json!("AS-1"),
                    json!("teranos/QNTX/AGENTS.md"),
                    json!(1_700_000_000_000_i64),
                ],
            )
            .unwrap();
        }
        let held = open(&dir);
        let rows = held
            .query(
                "SELECT id, doc, read_at FROM reads WHERE doc = ?",
                &[json!("teranos/QNTX/AGENTS.md")],
            )
            .unwrap();
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0]["id"], json!("AS-1"));
        assert_eq!(rows[0]["read_at"], json!(1_700_000_000_000_i64));
    }

    #[test]
    fn a_second_row_under_one_key_is_refused() {
        let dir = tempfile::tempdir().unwrap();
        let held = open(&dir);
        let one = [json!("AS-1"), json!("a"), json!(1)];
        held.exec("INSERT INTO reads VALUES (?, ?, ?)", &one)
            .unwrap();
        assert!(held
            .exec("INSERT INTO reads VALUES (?, ?, ?)", &one)
            .is_err());
        held.exec(
            "INSERT INTO reads VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
            &one,
        )
        .unwrap();
    }

    #[test]
    fn a_migration_is_applied_once() {
        let dir = tempfile::tempdir().unwrap();
        drop(open(&dir));
        let held = open(&dir);
        let rows = held
            .query(
                "SELECT count(*) AS n FROM schema_migrations WHERE version = ?",
                &[json!("quotes-001")],
            )
            .unwrap();
        assert_eq!(rows[0]["n"], json!(1));
    }

    #[test]
    fn a_value_json_has_no_word_for_is_refused() {
        let dir = tempfile::tempdir().unwrap();
        let held = open(&dir);
        assert!(held
            .query("SELECT TIMESTAMP '2026-11-08 10:00:00' AS t", &[])
            .is_err());
        let rows = held
            .query(
                "SELECT CAST(TIMESTAMP '2026-11-08 10:00:00' AS DATE)::VARCHAR AS day",
                &[],
            )
            .unwrap();
        assert_eq!(rows[0]["day"], json!("2026-11-08"));
    }

    #[test]
    fn a_list_is_no_parameter() {
        let dir = tempfile::tempdir().unwrap();
        let held = open(&dir);
        assert!(held.query("SELECT ? AS x", &[json!(["a"])]).is_err());
    }
}
