//! 「[Sonnet 5.5] What every `AttestationStore` has to do, as a check a backend runs on itself.」
//!
//! 「[Sonnet 5.5] The trait's docs are the contract (`traits.rs`); this is that contract run.」
//! 「[Sonnet 5.5] It exists because a backend's own tests tend to stay inside one session: put,」
//! 「[Sonnet 5.5] then act, in the same process, on the same buffer. DuckDB's `delete`, `update`,」
//! 「[Sonnet 5.5] `ids` and `clear` pass every such test and still do nothing to a row that a」
//! 「[Sonnet 5.5] flush has moved into a Parquet file (ADR-024). So each case here runs twice」
//! 「[Sonnet 5.5] where it can: acting in the session that wrote, and acting after the store」
//! 「[Sonnet 5.5] has been reopened, which is a new process's view of what the first one left.」
//!
//! 「[Sonnet 5.5] The answer is data, not a panic: [`check`] returns the [`Violation`]s, none」
//! 「[Sonnet 5.5] when the store does what the trait says. A backend's test asserts the list」
//! 「[Sonnet 5.5] is empty and prints it when it is not.」
//!
//! 「[Sonnet 5.5] ```no_run」
//! 「[Sonnet 5.5] # use ats::storage::{conformance, MemoryStore};」
//! 「[Sonnet 5.5] // Memory has no durability boundary: "reopen" hands the same store back.」
//! 「[Sonnet 5.5] let found = conformance::check(MemoryStore::new(), &mut |s| Ok(s), "memory", true);」
//! 「[Sonnet 5.5] assert!(found.is_empty(), "{found:#?}");」
//! 「[Sonnet 5.5] ```」
//!
//! 「[Sonnet 5.5] `reopen` takes the store by value so the caller can drop it first, which is」
//! 「[Sonnet 5.5] what makes a buffering backend write out (`DuckdbStore`'s `Drop` flushes),」
//! 「[Sonnet 5.5] and returns a store over the same data.」

use std::collections::HashMap;
use std::fmt;

use crate::attestation::Attestation;
use crate::storage::{AttestationStore, StoreError, StoreResult};

/// 「[Sonnet 5.5] One way a store did not do what the trait says.」
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Violation {
    /// 「[Sonnet 5.5] The case, and whether it acted in the writing session or after a reopen.」
    pub case: String,
    /// 「[Sonnet 5.5] The call, in the order the case makes them.」
    pub step: &'static str,
    pub expected: String,
    pub got: String,
}

impl fmt::Display for Violation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{} / {}: expected {}, got {}",
            self.case, self.step, self.expected, self.got
        )
    }
}

/// 「[Sonnet 5.5] The store was lost: a reopen failed, so nothing more can be asked of it.」
struct Lost;

/// 「[Sonnet 5.5] A case, by name.」
type Case<S> = (String, Box<dyn Fn(&mut Run<'_, S>) -> Result<(), Lost>>);

struct Run<'a, S> {
    store: Option<S>,
    reopen: &'a mut dyn FnMut(S) -> StoreResult<S>,
    tag: &'a str,
    found: Vec<Violation>,
    case: String,
}

impl<S: AttestationStore> Run<'_, S> {
    fn miss(&mut self, step: &'static str, expected: impl fmt::Display, got: impl fmt::Debug) {
        self.found.push(Violation {
            case: self.case.clone(),
            step,
            expected: expected.to_string(),
            got: format!("{got:?}"),
        });
    }

    fn s(&mut self) -> Result<&mut S, Lost> {
        self.store.as_mut().ok_or(Lost)
    }

    /// 「[Sonnet 5.5] The store, as a new process would see it.」
    fn reopen(&mut self) -> Result<(), Lost> {
        let Some(old) = self.store.take() else {
            return Err(Lost);
        };
        match (self.reopen)(old) {
            Ok(new) => {
                self.store = Some(new);
                Ok(())
            }
            Err(e) => {
                self.miss("reopen", "a store over the same data", e);
                Err(Lost)
            }
        }
    }

    /// 「[Sonnet 5.5] The call's value, or the failure recorded and `None`.」
    fn ok<T>(&mut self, step: &'static str, res: StoreResult<T>) -> Option<T> {
        match res {
            Ok(v) => Some(v),
            Err(e) => {
                self.miss(step, "Ok", e);
                None
            }
        }
    }

    fn id(&self, n: usize) -> String {
        format!("AS-conformance-{}-{}-{n}", self.tag, self.case.replace('/', "-"))
    }

    fn make(&self, n: usize) -> Attestation {
        let mut attributes = HashMap::new();
        attributes.insert("k".to_string(), serde_json::json!({ "n": 1.5 }));
        Attestation {
            id: self.id(n),
            subjects: vec!["ALICE".to_string()],
            predicates: vec!["knows".to_string()],
            contexts: vec!["work".to_string()],
            actors: vec!["human:bob".to_string()],
            timestamp: 1_700_000_000_000,
            source: "conformance".to_string(),
            attributes,
            created_at: 1_700_000_000_000,
            signature: None,
            signer_did: None,
        }
    }

    /// 「[Sonnet 5.5] Cross the durability boundary, if this case is the one that does.」
    fn boundary(&mut self, reopen: bool) -> Result<(), Lost> {
        if reopen {
            self.reopen()
        } else {
            Ok(())
        }
    }

    fn put_get(&mut self) -> Result<(), Lost> {
        let a = self.make(0);
        let missing = format!("{}-missing", a.id);
        let put = self.s()?.put(a.clone());
        self.ok("put", put);
        let got = self.s()?.get(&a.id);
        if let Some(got) = self.ok("get", got) {
            if got.as_ref() != Some(&a) {
                self.miss("get returns what was put", format!("{a:?}"), got);
            }
        }
        let exists = self.s()?.exists(&a.id);
        if self.ok("exists", exists) == Some(false) {
            self.miss("exists after put", true, false);
        }
        let none = self.s()?.get(&missing);
        if let Some(none) = self.ok("get missing", none) {
            if none.is_some() {
                self.miss("get of an id never put", "None", none);
            }
        }
        let none = self.s()?.exists(&missing);
        if self.ok("exists missing", none) == Some(true) {
            self.miss("exists of an id never put", false, true);
        }
        match self.s()?.put(a.clone()) {
            Err(StoreError::AlreadyExists(_)) => {}
            other => self.miss("put of an id already stored", "Err(AlreadyExists)", other),
        }
        let got = self.s()?.get(&a.id);
        if let Some(got) = self.ok("get after refused duplicate", got) {
            if got.as_ref() != Some(&a) {
                self.miss("a refused duplicate leaves the row as it was", format!("{a:?}"), got);
            }
        }
        Ok(())
    }

    /// 「[Sonnet 5.5] What `Attestation::writable` refuses, no store takes: not on `put`, not」
    /// 「[Sonnet 5.5] on `update`, and a refused one leaves nothing and changes nothing.」
    fn refuses_unwritable(&mut self) -> Result<(), Lost> {
        let mut bad = self.make(0);
        bad.subjects.clear();
        match self.s()?.put(bad.clone()) {
            Err(StoreError::Unwritable(_)) => {}
            other => self.miss("put with no subject", "Err(Unwritable)", other),
        }
        let exists = self.s()?.exists(&bad.id);
        if self.ok("exists", exists) == Some(true) {
            self.miss("a refused put stores nothing", false, true);
        }
        let good = self.make(1);
        let put = self.s()?.put(good.clone());
        self.ok("put", put);
        let mut worse = good.clone();
        worse.predicates.clear();
        match self.s()?.update(worse) {
            Err(StoreError::Unwritable(_)) => {}
            other => self.miss("update to no predicate", "Err(Unwritable)", other),
        }
        let got = self.s()?.get(&good.id);
        if let Some(got) = self.ok("get", got) {
            if got.as_ref() != Some(&good) {
                self.miss("a refused update changes nothing", format!("{good:?}"), got);
            }
        }
        Ok(())
    }

    fn kept_across_reopen(&mut self) -> Result<(), Lost> {
        let a = self.make(0);
        let put = self.s()?.put(a.clone());
        self.ok("put", put);
        self.reopen()?;
        let got = self.s()?.get(&a.id);
        if let Some(got) = self.ok("get", got) {
            if got.as_ref() != Some(&a) {
                self.miss("get returns what an earlier session put", format!("{a:?}"), got);
            }
        }
        let exists = self.s()?.exists(&a.id);
        if self.ok("exists", exists) == Some(false) {
            self.miss("exists after reopen", true, false);
        }
        Ok(())
    }

    fn ids_and_count(&mut self, reopen: bool) -> Result<(), Lost> {
        let all: Vec<Attestation> = (0..3).map(|n| self.make(n)).collect();
        let before = self.s()?.count();
        let before = self.ok("count", before);
        for a in &all {
            let put = self.s()?.put(a.clone());
            self.ok("put", put);
        }
        self.boundary(reopen)?;
        let ids = self.s()?.ids();
        let count = self.s()?.count();
        let (Some(ids), Some(count)) = (self.ok("ids", ids), self.ok("count", count)) else {
            return Ok(());
        };
        for a in &all {
            if !ids.contains(&a.id) {
                self.miss("ids lists every stored id", &a.id, format!("{} ids, not this one", ids.len()));
            }
        }
        // 「[Sonnet 5.5] The rows already in the store are not this case's, so it counts what it added.」
        if let Some(before) = before {
            if count != before + all.len() {
                self.miss("count grows by the rows put", before + all.len(), count);
            }
        }
        Ok(())
    }

    fn update(&mut self, reopen: bool) -> Result<(), Lost> {
        let a = self.make(0);
        let put = self.s()?.put(a.clone());
        self.ok("put", put);
        self.boundary(reopen)?;
        // 「[Sonnet 5.5] Every field an update can change, so a store that drops one is seen.」
        // 「[Sonnet 5.5] `created_at` stays as it was: whether an update may rewrite it is not」
        // 「[Sonnet 5.5] the trait's to say yet, and both answers then agree.」
        let mut changed = a.clone();
        changed.subjects = vec!["ZED".to_string()];
        changed.predicates = vec!["knew".to_string()];
        changed.contexts = vec!["home".to_string()];
        changed.actors = vec!["human:carol".to_string()];
        changed.timestamp += 1_000;
        changed.source = "changed".to_string();
        changed.attributes = HashMap::from([
            ("k".to_string(), serde_json::json!({ "n": 2.5 })),
            ("z".to_string(), serde_json::json!("text")),
        ]);
        changed.signature = Some(vec![1, 2, 3]);
        changed.signer_did = Some("did:key:zconformance".to_string());
        let upd = self.s()?.update(changed.clone());
        let upd_ok = self.ok("update", upd).is_some();
        // 「[Sonnet 5.5] What is stored is what is asked about: now, and in a case that crosses」
        // 「[Sonnet 5.5] the boundary, after the next reopen as well. A same-session case never reopens.」
        for after in if reopen { vec![false, true] } else { vec![false] } {
            if after {
                self.reopen()?;
            }
            let got = self.s()?.get(&a.id);
            let Some(Some(got)) = self.ok("get", got) else {
                self.miss("get after update", "the row", "None");
                continue;
            };
            let at = if after { "after update and reopen" } else { "after update" };
            if upd_ok && got != changed {
                self.miss(at, format!("{changed:?}"), got);
            }
        }
        let mut gone = self.make(1);
        gone.source = "changed".to_string();
        match self.s()?.update(gone) {
            Err(StoreError::NotFound(_)) => {}
            other => self.miss("update of an id never put", "Err(NotFound)", other),
        }
        Ok(())
    }

    fn delete(&mut self, reopen: bool) -> Result<(), Lost> {
        let a = self.make(0);
        let put = self.s()?.put(a.clone());
        self.ok("put", put);
        self.boundary(reopen)?;
        let del = self.s()?.delete(&a.id);
        if self.ok("delete", del) == Some(false) {
            self.miss("delete of a stored id", true, false);
        }
        let exists = self.s()?.exists(&a.id);
        if self.ok("exists", exists) == Some(true) {
            self.miss("exists after delete", false, true);
        }
        let got = self.s()?.get(&a.id);
        if let Some(got) = self.ok("get", got) {
            if got.is_some() {
                self.miss("get after delete", "None", "the row");
            }
        }
        let ids = self.s()?.ids();
        if let Some(ids) = self.ok("ids", ids) {
            if ids.contains(&a.id) {
                self.miss("ids after delete", "without the id", "the id");
            }
        }
        if reopen {
            self.reopen()?;
            let exists = self.s()?.exists(&a.id);
            if self.ok("exists after reopen", exists) == Some(true) {
                self.miss("exists after delete and reopen", false, true);
            }
        }
        let again = self.s()?.delete(&a.id);
        if self.ok("delete again", again) == Some(true) {
            self.miss("delete of an id already deleted", false, true);
        }
        Ok(())
    }

    fn clear(&mut self, reopen: bool) -> Result<(), Lost> {
        let a = self.make(0);
        let put = self.s()?.put(a);
        self.ok("put", put);
        self.boundary(reopen)?;
        let clear = self.s()?.clear();
        self.ok("clear", clear);
        for after in if reopen { vec![false, true] } else { vec![false] } {
            if after {
                self.reopen()?;
            }
            let count = self.s()?.count();
            if let Some(n) = self.ok("count", count) {
                if n != 0 {
                    self.miss(if after { "count after clear and reopen" } else { "count after clear" }, 0, n);
                }
            }
            let ids = self.s()?.ids();
            if let Some(ids) = self.ok("ids", ids) {
                if !ids.is_empty() {
                    self.miss(if after { "ids after clear and reopen" } else { "ids after clear" }, "none", ids.len());
                }
            }
        }
        Ok(())
    }
}

/// 「[Sonnet 5.5] Run every case against `store`.」
///
/// 「[Sonnet 5.5] `tag` is in every id the cases write, so two runs against one persistent」
/// 「[Sonnet 5.5] store do not collide. `destructive` adds the `clear` cases, which empty」
/// 「[Sonnet 5.5] the store, rows that were not the check's own included: never against data」
/// 「[Sonnet 5.5] that matters.」
pub fn check<S: AttestationStore>(
    store: S,
    reopen: &mut dyn FnMut(S) -> StoreResult<S>,
    tag: &str,
    destructive: bool,
) -> Vec<Violation> {
    let mut run = Run { store: Some(store), reopen, tag, found: Vec::new(), case: String::new() };
    // 「[Sonnet 5.5] The cases that act in the writing session run first. Once a reopen has」
    // 「[Sonnet 5.5] moved rows into files, a later case's store holds rows it did not write,」
    // 「[Sonnet 5.5] and a gap the first reopen exposed would be blamed on every case after it.」
    let mut cases: Vec<Case<S>> =
        vec![
            ("put_get".to_string(), Box::new(|r| r.put_get())),
            ("refuses_unwritable".to_string(), Box::new(|r| r.refuses_unwritable())),
        ];
    for (name, reopen) in [("same_session", false), ("after_reopen", true)] {
        if reopen {
            cases.push(("kept_across_reopen".to_string(), Box::new(|r| r.kept_across_reopen())));
        }
        cases.push((format!("ids_and_count/{name}"), Box::new(move |r| r.ids_and_count(reopen))));
        cases.push((format!("update/{name}"), Box::new(move |r| r.update(reopen))));
        cases.push((format!("delete/{name}"), Box::new(move |r| r.delete(reopen))));
        if destructive {
            cases.push((format!("clear/{name}"), Box::new(move |r| r.clear(reopen))));
        }
    }
    for (name, case) in cases {
        run.case = name;
        if case(&mut run).is_err() {
            break;
        }
    }
    run.found
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic, clippy::indexing_slicing)]
mod tests {
    use super::*;
    use crate::storage::MemoryStore;

    #[test]
    fn the_memory_store_does_what_the_trait_says() {
        let found = check(MemoryStore::new(), &mut |s| Ok(s), "memory", true);
        assert!(found.is_empty(), "{found:#?}");
    }

    /// 「[Sonnet 5.5] A store whose delete and update reach only what the session wrote: the」
    /// 「[Sonnet 5.5] shape of a buffer in front of immutable files. The check has to see it,」
    /// 「[Sonnet 5.5] and only after a reopen.」
    #[derive(Default)]
    struct Buffered {
        files: HashMap<String, Attestation>,
        buffer: HashMap<String, Attestation>,
        /// 「[Sonnet 5.5] Takes what `writable` refuses, as Postgres did.」
        lax: bool,
        /// 「[Sonnet 5.5] Keeps the old predicates on update, as a store that persists only some fields.」
        lossy: bool,
    }

    impl AttestationStore for Buffered {
        fn put(&mut self, a: Attestation) -> StoreResult<()> {
            if !self.lax {
                a.writable()?;
            }
            if self.exists(&a.id)? {
                return Err(StoreError::AlreadyExists(a.id));
            }
            self.buffer.insert(a.id.clone(), a);
            Ok(())
        }
        fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
            Ok(self.buffer.get(id).or_else(|| self.files.get(id)).cloned())
        }
        fn delete(&mut self, id: &str) -> StoreResult<bool> {
            Ok(self.buffer.remove(id).is_some())
        }
        fn update(&mut self, a: Attestation) -> StoreResult<()> {
            if !self.lax {
                a.writable()?;
            }
            if !self.exists(&a.id)? {
                return Err(StoreError::NotFound(a.id));
            }
            if let Some(slot) = self.buffer.get_mut(&a.id) {
                let kept = slot.predicates.clone();
                *slot = a;
                if self.lossy {
                    slot.predicates = kept;
                }
            }
            Ok(())
        }
        fn ids(&self) -> StoreResult<Vec<String>> {
            Ok(self.buffer.keys().cloned().collect())
        }
        fn count(&self) -> StoreResult<usize> {
            Ok(self.buffer.len() + self.files.len())
        }
        fn clear(&mut self) -> StoreResult<()> {
            self.buffer.clear();
            Ok(())
        }
    }

    #[test]
    fn a_buffer_in_front_of_immutable_files_passes_in_the_session_and_fails_after_a_reopen() {
        let mut flush = |mut s: Buffered| {
            let moved: Vec<_> = s.buffer.drain().collect();
            s.files.extend(moved);
            Ok(s)
        };
        let found = check(Buffered::default(), &mut flush, "buffered", true);
        assert!(!found.is_empty());
        assert!(
            found.iter().all(|v| v.case.ends_with("after_reopen")),
            "the same-session cases must pass: {found:#?}"
        );
        let cases: std::collections::BTreeSet<&str> = found.iter().map(|v| v.case.as_str()).collect();
        assert_eq!(
            cases.into_iter().collect::<Vec<_>>(),
            ["clear/after_reopen", "delete/after_reopen", "ids_and_count/after_reopen", "update/after_reopen"],
            "{found:#?}"
        );
    }

    #[test]
    fn a_store_that_takes_what_writable_refuses_is_reported_in_the_session() {
        let mut same = |s: Buffered| Ok(s);
        let found = check(Buffered { lax: true, ..Buffered::default() }, &mut same, "lax", false);
        let cases: std::collections::BTreeSet<&str> = found.iter().map(|v| v.case.as_str()).collect();
        assert_eq!(cases.into_iter().collect::<Vec<_>>(), ["refuses_unwritable"], "{found:#?}");
    }

    #[test]
    fn a_store_that_persists_only_some_fields_of_an_update_is_reported_in_the_session() {
        let mut same = |s: Buffered| Ok(s);
        let found = check(Buffered { lossy: true, ..Buffered::default() }, &mut same, "lossy", false);
        let cases: std::collections::BTreeSet<&str> = found.iter().map(|v| v.case.as_str()).collect();
        assert!(cases.contains("update/same_session"), "{found:#?}");
    }

    #[test]
    fn a_store_that_cannot_reopen_is_reported_and_the_run_stops() {
        let mut gone = |_s: MemoryStore| Err(StoreError::Backend("gone".into()));
        let found = check(MemoryStore::new(), &mut gone, "gone", true);
        assert_eq!(found.last().map(|v| v.step), Some("reopen"), "{found:#?}");
    }
}
